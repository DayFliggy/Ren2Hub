package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestViolationFeeUsesIndependentFrozenEventAfterPrimaryRelease(t *testing.T) {
	c, session := durableSession(t, 40)
	info := session.relayInfo
	info.StartTime = time.Now()
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	require.NoError(t, model.DB.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}))
	version, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Model: "fee-test", Mode: billing.ModeUnitPrice, FlatFee: "0.002"}, 0, 1)
	require.NoError(t, err)
	info.PriceData.Quote, err = billing.NewQuote(*version, "1", nil, time.Now())
	require.NoError(t, err)
	info.PriceData.Quote.ViolationFeeEnabled = true
	info.PriceData.Quote.ViolationFeeUSD = "0.000008"
	info.PriceData.Quote.Hash = ""
	info.PriceData.Quote.Hash, err = billing.Hash(info.PriceData.Quote)
	require.NoError(t, err)
	session.Refund(c)
	require.NoError(t, info.BillingRefundError)
	apiErr := types.NewErrorWithStatusCode(assert.AnError, types.ErrorCodeViolationFeeGrokCSAM, 400)
	for range 2 {
		require.True(t, ChargeViolationFeeIfNeeded(c, info, apiErr))
	}
	assert.Equal(t, 96, getUserQuota(t, 1))
	assert.Equal(t, 4, getTokenUsedQuota(t, 1))
	var primary, fee model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", "session").First(&primary).Error)
	require.NoError(t, model.DB.Where("request_id = ?", "session:violation").First(&fee).Error)
	assert.Equal(t, model.BillingRecoveryStatusRefunded, primary.Status)
	assert.Equal(t, model.BillingRecoveryStatusSettled, fee.Status)
	assert.NotEmpty(t, fee.QuoteSnapshot)
	assert.NotEmpty(t, fee.UsageSnapshot)
	assert.NotEmpty(t, fee.ChargeSnapshot)
	var attempts []model.BillingAttempt
	var events []model.BillingEvent
	require.NoError(t, model.DB.Where("request_id = ?", fee.RequestID).Find(&attempts).Error)
	require.NoError(t, model.DB.Where("request_id = ?", fee.RequestID).Find(&events).Error)
	assert.Equal(t, model.BillingAuditVerified, model.AuditBillingEvidence(fee, attempts, events).Status)
	info.PriceData.Quote.ViolationFeeUSD = "0.000010"
	assert.False(t, ChargeViolationFeeIfNeeded(c, info, apiErr), "same event with changed frozen fee must conflict")
	assert.Equal(t, 96, getUserQuota(t, 1))
}

func TestViolationFeePersistsCompleteIntentAcrossFailures(t *testing.T) {
	for _, boundary := range []string{"attempt", "intent", "settlement", "canceled"} {
		t.Run(boundary, func(t *testing.T) {
			c, session := durableSession(t, 40)
			info := session.relayInfo
			info.StartTime = time.Now()
			info.ChannelMeta = &relaycommon.ChannelMeta{}
			info.PriceData.Quote = billingTestCallQuote(t)
			info.PriceData.Quote.ViolationFeeEnabled = true
			info.PriceData.Quote.ViolationFeeUSD = "0.000008"
			info.PriceData.Quote.Hash = ""
			var err error
			info.PriceData.Quote.Hash, err = billing.Hash(info.PriceData.Quote)
			require.NoError(t, err)
			session.Refund(c)
			require.NoError(t, info.BillingRefundError)
			apiErr := types.NewErrorWithStatusCode(assert.AnError, types.ErrorCodeViolationFeeGrokCSAM, 400)
			callback := "test:violation-failure-" + boundary
			if boundary == "canceled" {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
				require.True(t, ChargeViolationFeeIfNeeded(c, info, apiErr))
			} else {
				if boundary == "intent" {
					require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
						if values, ok := tx.Statement.Dest.(map[string]any); ok && values["consumption_snapshot"] != nil {
							tx.AddError(assert.AnError)
						}
					}))
				} else {
					require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
						if boundary == "attempt" && tx.Statement.Table == "billing_attempts" {
							tx.AddError(assert.AnError)
						}
						if event, ok := tx.Statement.Dest.(*model.BillingEvent); boundary == "settlement" && ok && event.Operation == "settle" {
							tx.AddError(assert.AnError)
						}
					}))
				}
				assert.False(t, ChargeViolationFeeIfNeeded(c, info, apiErr))
				if boundary == "intent" {
					require.NoError(t, model.DB.Callback().Update().Remove(callback))
				} else {
					require.NoError(t, model.DB.Callback().Create().Remove(callback))
				}
				assert.Equal(t, 100, getUserQuota(t, 1))
				if boundary == "settlement" {
					var recovery model.BillingRecovery
					require.NoError(t, model.DB.Where("request_id = ?", "session:violation").First(&recovery).Error)
					assert.Equal(t, model.BillingRecoveryStatusSettlementPending, recovery.Status)
					assert.NotEmpty(t, recovery.QuoteSnapshot)
					assert.NotEmpty(t, recovery.UsageSnapshot)
					assert.NotEmpty(t, recovery.ChargeSnapshot)
					status, err := recoverOneBillingRecovery(context.Background(), "session:violation")
					require.NoError(t, err)
					assert.Equal(t, model.BillingRecoveryStatusSettled, status)
				} else {
					var count int64
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", "session:violation").Count(&count).Error)
					assert.Zero(t, count, "partial surcharge evidence must roll back")
				}
			}
			require.True(t, ChargeViolationFeeIfNeeded(c, info, apiErr))
			assert.Equal(t, 96, getUserQuota(t, 1))
			assert.Equal(t, 4, getTokenUsedQuota(t, 1))
		})
	}
}
