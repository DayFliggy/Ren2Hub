package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingAccountConcurrencyAcrossDatabases(t *testing.T) {
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " must target an isolated database")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLog := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(backend.kind, backend.kind)
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			if backend.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousKind, previousLogKind)
				initCol()
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}, &Log{}, &QuotaData{}, &Channel{}, &BillingPriceVersion{}, &BillingActivePrice{}))
			require.NoError(t, db.Create(&User{Id: 1, Username: "concurrent-wallet", Quota: 100}).Error)
			for id := 1; id <= 2; id++ {
				require.NoError(t, db.Create(&Token{Id: id, UserId: 1, Key: fmt.Sprintf("concurrent-%d", id), RemainQuota: 100}).Error)
				_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: fmt.Sprintf("reserve-%d", id), UserID: 1, TokenID: id, TokenRequired: true, Source: "wallet"})
				require.NoError(t, err)
			}
			var outcomes [2]error
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				go func(i int) { defer wg.Done(); <-start; outcomes[i] = Reserve(fmt.Sprintf("reserve-%d", i+1), 60) }(i)
			}
			close(start)
			wg.Wait()
			winner := 0
			for i, err := range outcomes {
				if err == nil {
					winner = i + 1
				} else {
					require.ErrorIs(t, err, ErrBillingRecoveryFundingQuota)
				}
			}
			require.NotZero(t, winner)
			assert.NotEqual(t, outcomes[0] == nil, outcomes[1] == nil, "two tokens share one wallet admission")
			var user User
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 40, user.Quota)
			assert.EqualValues(t, 60, user.ReservedQuota)
			requestID := fmt.Sprintf("reserve-%d", winner)
			consumption, err := bindConsumptionEvidence(requestID, BillingConsumption{Log: Log{UserId: 1, TokenId: winner, Quota: 30, CreatedAt: 7200}, CountUsage: true})
			require.NoError(t, err)
			require.NoError(t, PrepareBillingRecoveryConsumption(requestID, consumption))
			var recovery BillingRecovery
			require.NoError(t, db.Where("request_id = ?", requestID).First(&recovery).Error)
			command := BillingResolution{OperationID: "race-resolve", ExpectedVersion: recovery.Version, Action: "settle", Quota: 30, Reason: "provider amount verified"}
			start = make(chan struct{})
			wg.Add(2)
			go func() { defer wg.Done(); <-start; outcomes[0] = Settle(requestID, 30) }()
			go func() {
				defer wg.Done()
				<-start
				_, outcomes[1] = ResolveBillingReconciliation(context.Background(), recovery.ID, 1, command)
			}()
			close(start)
			wg.Wait()
			for _, err := range outcomes {
				if err != nil {
					assert.True(t, errors.Is(err, ErrBillingEventConflict) || errors.Is(err, ErrBillingVersionConflict) || errors.Is(err, ErrBillingRecoveryStateConflict), err)
				}
			}
			assert.NotEqual(t, outcomes[0] == nil, outcomes[1] == nil, "one finalization wins")
			require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
			require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, []common.Quota{70, 0, 30}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
			var finalCount int64
			require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ? AND operation = ?", requestID, "settle").Count(&finalCount).Error)
			assert.EqualValues(t, 1, finalCount)
			assert.Equal(t, 1, user.RequestCount)
			// Concurrent callback replay uses the same stable business event.
			start = make(chan struct{})
			wg.Add(2)
			for i := range 2 {
				go func(i int) {
					defer wg.Done()
					<-start
					_, outcomes[i] = ApplyWalletAdjustment(WalletAdjustment{EventID: "payment-once", UserID: 1, Delta: 50, SourceType: "topup", SourceID: "order-1"})
				}(i)
			}
			close(start)
			wg.Wait()
			for _, err := range outcomes {
				require.NoError(t, err)
			}
			_, err = ApplyWalletAdjustment(WalletAdjustment{EventID: "payment-once", UserID: 1, Delta: 51, SourceType: "topup", SourceID: "order-1"})
			require.ErrorIs(t, err, ErrBillingEventConflict)
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 120, user.Quota)
			var events []BillingEvent
			require.NoError(t, db.Find(&events).Error)
			available, reserved, used := common.Quota(100), common.Quota(0), common.Quota(0)
			for _, event := range events {
				available += event.AvailableDelta
				reserved += event.ReservedDelta
				used += event.UsedDelta
			}
			assert.Equal(t, []common.Quota{available, reserved, used}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
		})
	}
}
