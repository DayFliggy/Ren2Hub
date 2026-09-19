package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViolationFeeUsesIndependentFrozenEventAfterPrimaryRelease(t *testing.T) {
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
	info.PriceData.Quote.ViolationFeeUSD = "0.000010"
	assert.False(t, ChargeViolationFeeIfNeeded(c, info, apiErr), "same event with changed frozen fee must conflict")
	assert.Equal(t, 96, getUserQuota(t, 1))
}
