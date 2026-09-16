package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedRefundBillingSession(t *testing.T, trusted bool, precharge int) (*gin.Context, *BillingSession, int) {
	t.Helper()
	truncate(t)
	quota := common.GetTrustQuota() + 100
	seedUser(t, 810, quota)
	seedToken(t, 811, 810, "billing-refund-session", quota)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RequestId: "billing-refund-session", UserId: 810, TokenId: 811, TokenKey: "billing-refund-session",
		TokenUnlimited: true, ForcePreConsume: !trusted,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(c, info, precharge)
	require.Nil(t, apiErr, "%v", apiErr)
	t.Cleanup(session.stopRecoveryHeartbeat)
	assert.Equal(t, trusted, session.trusted)
	return c, session, quota
}

func assertRefundSessionBalances(t *testing.T, quota, spent int) {
	t.Helper()
	var user model.User
	var token model.Token
	require.NoError(t, model.DB.First(&user, 810).Error)
	require.NoError(t, model.DB.First(&token, 811).Error)
	assert.Equal(t, quota-spent, user.Quota)
	assert.Equal(t, quota-spent, token.RemainQuota)
	assert.Equal(t, spent, token.UsedQuota)
}

func TestBillingSessionZeroPrechargeRefundClosesRecovery(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		name := "explicit zero"
		precharge := 0
		if trusted {
			name, precharge = "trusted bypass", 10
		}
		t.Run(name, func(t *testing.T) {
			c, session, quota := seedRefundBillingSession(t, trusted, precharge)
			assert.Zero(t, session.GetPreConsumedQuota())
			require.True(t, session.NeedsRefund(), "the recovery record still needs a terminal state")
			session.Refund(c)
			session.Refund(c)
			require.NoError(t, session.relayInfo.BillingRefundError)
			assert.False(t, session.NeedsRefund())
			select {
			case <-session.recoveryDone:
			default:
				t.Error("failed zero-precharge request kept its heartbeat running")
			}
			recovery, err := loadBillingRecovery(session.recoveryID)
			require.NoError(t, err)
			assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
			assert.True(t, model.BillingRecoveryRefundComplete(recovery))
			assert.Zero(t, recovery.FundingAmount)
			assert.Zero(t, recovery.TokenAmount)
			status, err := recoverOneBillingRecovery(context.Background(), session.recoveryID)
			require.NoError(t, err)
			assert.Equal(t, model.BillingRecoveryStatusRefunded, status)
			assertRefundSessionBalances(t, quota, 0)
		})
	}
}

func TestBillingSessionZeroPrechargeRefundFailuresRemainRetryable(t *testing.T) {
	for _, phase := range []string{"pending", "funding", "complete"} {
		for _, worker := range []bool{false, true} {
			mode := "session"
			if worker {
				mode = "recovery"
			}
			t.Run(phase+"/"+mode, func(t *testing.T) {
				c, session, quota := seedRefundBillingSession(t, true, 10)
				var removeFailure func()
				if phase == "funding" {
					removeFailure = injectBillingIntegrationFailure(t, model.DB, model.LOG_DB, "funding_marker_failure")
				} else {
					const callback = "test:zero-refund-status-failure"
					status := model.BillingRecoveryStatusRefundPending
					if phase == "complete" {
						status = model.BillingRecoveryStatusRefunded
					}
					require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
						values, _ := tx.Statement.Dest.(map[string]any)
						if tx.Statement.Table == "billing_recoveries" && values["status"] == status {
							tx.AddError(assert.AnError)
						}
					}))
					removeFailure = func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) }
				}
				t.Cleanup(func() {
					if removeFailure != nil {
						removeFailure()
					}
				})
				for range 2 {
					session.Refund(c)
					require.ErrorIs(t, session.relayInfo.BillingRefundError, assert.AnError)
					assert.True(t, session.NeedsRefund())
				}
				select {
				case <-session.recoveryDone:
				default:
					t.Error("refund failure kept the request heartbeat running")
				}
				recovery, err := loadBillingRecovery(session.recoveryID)
				require.NoError(t, err)
				assert.NotEqual(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assertRefundSessionBalances(t, quota, 0)
				removeFailure()
				removeFailure = nil
				if worker {
					status, err := recoverOneBillingRecovery(context.Background(), session.recoveryID)
					require.NoError(t, err)
					assert.Equal(t, model.BillingRecoveryStatusRefunded, status)
				}
				session.Refund(c)
				session.Refund(c)
				require.NoError(t, session.relayInfo.BillingRefundError)
				assert.False(t, session.NeedsRefund())
				recovery, err = loadBillingRecovery(session.recoveryID)
				require.NoError(t, err)
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assert.True(t, model.BillingRecoveryRefundComplete(recovery))
				var adjustments []model.BillingRecoveryAdjustment
				require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", session.recoveryID, model.BillingRecoveryOperationRefund).Find(&adjustments).Error)
				require.Len(t, adjustments, 1, "retries must reuse the completed funding adjustment")
				assert.Zero(t, adjustments[0].Amount)
				assertRefundSessionBalances(t, quota, 0)
			})
		}
	}
}

func TestBillingSessionZeroPrechargeRefundCannotUndoSettlement(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "settled"
		if pending {
			name = "settlement pending"
		}
		t.Run(name, func(t *testing.T) {
			c, session, quota := seedRefundBillingSession(t, true, 10)
			var removeFailure func()
			if pending {
				removeFailure = injectBillingIntegrationFailure(t, model.DB, model.LOG_DB, "funding_marker_failure")
				t.Cleanup(func() {
					if removeFailure != nil {
						removeFailure()
					}
				})
				require.Error(t, session.Settle(10))
			} else {
				require.NoError(t, session.Settle(10))
			}
			before, err := loadBillingRecovery(session.recoveryID)
			require.NoError(t, err)
			session.Refund(c)
			assert.False(t, session.NeedsRefund())
			after, err := loadBillingRecovery(session.recoveryID)
			require.NoError(t, err)
			assert.Equal(t, before.Status, after.Status)
			assert.False(t, session.refunded)
			if removeFailure != nil {
				removeFailure()
				removeFailure = nil
			}
			require.NoError(t, session.Settle(10))
			session.Refund(c)
			assertRefundSessionBalances(t, quota, 10)
		})
	}
}
