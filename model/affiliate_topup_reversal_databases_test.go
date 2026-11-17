package model

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Order reversals serialize with transfers and with new top-up settlements of
// the same invitee on every dialect: one order is reversed once, balances and
// debt stay consistent with the records, and no combination deadlocks.
func TestAffiliateTopUpReversalConcurrencyDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openAffiliateLedgerDatabase(t, dialect)
			pair := func(t *testing.T, name string) (User, User) {
				inviter := User{Username: name + "-inviter", AffCode: "REV-" + name + "-A", Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&inviter).Error)
				invitee := User{Username: name + "-invitee", AffCode: "REV-" + name + "-B", InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&invitee).Error)
				return inviter, invitee
			}
			pending := func(t *testing.T, userID int, trade string) {
				order := &TopUp{UserId: userID, TradeNo: trade, PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, ExpectedQuota: 10000, Money: 2, Currency: "CNY", PayAmount: "2.00", CreateTime: common.GetTimestamp()}
				require.NoError(t, order.Insert())
			}
			settle := func(trade string) error {
				_, err := RechargeEpay(trade, "", "", "2.00")
				return err
			}

			t.Run("one order is reversed once", func(t *testing.T) {
				inviter, invitee := pair(t, "once")
				pending(t, invitee.Id, "rev-once")
				require.NoError(t, settle("rev-once"))
				var replays atomic.Int32
				errs := runAffiliateConcurrently(8, func(int) error {
					_, replayed, err := ReverseTopUpAffiliateReward("rev-once", common.RoleRootUser)
					if err == nil && replayed {
						replays.Add(1)
					}
					return err
				})
				for _, err := range errs {
					require.NoError(t, err)
				}
				assert.EqualValues(t, 7, replays.Load())
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Zero(t, inviter.AffOffsetDebtQuota)
				assert.EqualValues(t, 1, countAffiliateKind(t, db, inviter.Id, "reversal"))
			})

			t.Run("reversal and transfer", func(t *testing.T) {
				inviter, invitee := pair(t, "transfer")
				pending(t, invitee.Id, "rev-transfer")
				require.NoError(t, settle("rev-transfer"))
				var reversal *AffiliateRecord
				errs := runAffiliateConcurrently(2, func(i int) error {
					if i == 0 {
						record, _, err := ReverseTopUpAffiliateReward("rev-transfer", common.RoleRootUser)
						reversal = record
						return err
					}
					_, _, err := TransferAffiliateQuota(inviter.Id, "rev-transfer-out")
					return err
				})
				require.NoError(t, errs[0])
				if errs[1] != nil {
					require.ErrorIs(t, errs[1], ErrAffiliateUnavailable, "only a reversal that ran first may leave nothing to transfer")
				}
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Equal(t, 2000, inviter.Quota+reversal.CreditedQuota, "the reward was either transferred or revoked")
				assert.Equal(t, 2000, reversal.CreditedQuota+reversal.DebtAddedQuota)
				assert.Equal(t, reversal.DebtAddedQuota, inviter.AffOffsetDebtQuota)
			})

			t.Run("reversal and a new top-up of the same invitee", func(t *testing.T) {
				inviter, invitee := pair(t, "settle")
				for round := range 4 {
					reversed, next := fmt.Sprintf("rev-settle-%d", round), fmt.Sprintf("rev-settle-next-%d", round)
					pending(t, invitee.Id, reversed)
					require.NoError(t, settle(reversed))
					// Move the reward out so the reversal owes it as debt, which
					// the concurrent new reward repays in either order.
					_, _, err := TransferAffiliateQuota(inviter.Id, fmt.Sprintf("rev-settle-out-%d", round))
					require.NoError(t, err)
					pending(t, invitee.Id, next)
					errs := runAffiliateConcurrently(2, func(i int) error {
						if i == 0 {
							_, _, err := ReverseTopUpAffiliateReward(reversed, common.RoleRootUser)
							return err
						}
						return settle(next)
					})
					require.NoError(t, errs[0], "round %d reversal", round)
					require.NoError(t, errs[1], "round %d settlement", round)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Zero(t, inviter.AffQuota, "round %d", round)
					assert.Zero(t, inviter.AffOffsetDebtQuota, "round %d", round)
				}
				require.NoError(t, db.First(&invitee, invitee.Id).Error)
				assert.Equal(t, 80000, invitee.Quota, "the invitee's wallet keeps every top-up")
				assert.EqualValues(t, 8, countAffiliateKind(t, db, inviter.Id, "accrue"))
				assert.EqualValues(t, 4, countAffiliateKind(t, db, inviter.Id, "reversal"))
			})
		})
	}
}

func countAffiliateKind(t *testing.T, db *gorm.DB, userID int, kind string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ?", userID, kind).Count(&count).Error)
	return count
}
