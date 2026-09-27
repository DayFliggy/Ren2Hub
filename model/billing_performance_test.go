package model

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type billingPerformanceSQL struct {
	logger.Interface
	count atomic.Int64
}

func (l *billingPerformanceSQL) Trace(_ context.Context, _ time.Time, _ func() (string, int64), _ error) {
	l.count.Add(1)
}

// Run explicitly to compare this complete durable accounting workload against
// the baseline harness documented in docs/billing/VALIDATION.md.
func TestBillingPerformance(t *testing.T) {
	if os.Getenv("BILLING_PERFORMANCE") != "1" {
		t.Skip("explicit performance run")
	}
	for _, workers := range []int{1, 8} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			trace := &billingPerformanceSQL{Interface: logger.Default.LogMode(logger.Silent)}
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: trace})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			defer sqlDB.Close()
			oldDB, oldLogs, oldRedis := DB, LOG_DB, common.RedisEnabled
			oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB, common.RedisEnabled = db, db, false
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			defer func() {
				DB, LOG_DB, common.RedisEnabled = oldDB, oldLogs, oldRedis
				common.SetDatabaseTypes(oldMain, oldLog)
			}()
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}, &Log{}, &Channel{}, &QuotaData{}, &BillingPriceVersion{}, &BillingActivePrice{}))
			for id := 1; id <= 8; id++ {
				require.NoError(t, db.Create(&User{Id: id, Username: fmt.Sprintf("bench-%d", id), AffCode: fmt.Sprintf("b%d", id), Quota: 1000000000}).Error)
				require.NoError(t, db.Create(&Token{Id: id, UserId: id, Key: fmt.Sprintf("bench-key-%d", id), RemainQuota: 1000000000}).Error)
			}
			require.NoError(t, db.Create(&Channel{Id: 1, Name: "bench"}).Error)
			const requests = 160
			elapsed := make([]time.Duration, requests)
			failures := make(chan error, requests)
			startStats := sqlDB.Stats()
			trace.count.Store(0)
			var next atomic.Int64
			var wg sync.WaitGroup
			started := time.Now()
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						index := int(next.Add(1)) - 1
						if index >= requests {
							return
						}
						start := time.Now()
						id := index%8 + 1
						requestID := fmt.Sprintf("bench-%d", index)
						run := func() error {
							_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: requestID, UserID: id, TokenID: id, Source: "wallet", TokenRequired: true})
							if err != nil {
								return err
							}
							if err := Reserve(requestID, 500); err != nil {
								return err
							}
							usage := BillingConsumption{Log: Log{UserId: id, TokenId: id, ChannelId: 1, Username: fmt.Sprintf("bench-%d", id), CreatedAt: 7200, Type: LogTypeConsume, ModelName: "benchmark", Quota: 300, PromptTokens: 10, CompletionTokens: 20}, CountUsage: true, LogEnabled: true, ExportEnabled: true, NodeName: "bench"}
							usage, err = bindConsumptionEvidence(requestID, usage)
							if err != nil {
								return err
							}
							if err := PrepareBillingRecoveryConsumption(requestID, usage); err != nil {
								return err
							}
							if err := Settle(requestID, 300); err != nil {
								return err
							}
							return ApplyBillingRecoveryConsumption(requestID)
						}
						if err := run(); err != nil {
							failures <- err
						}
						elapsed[index] = time.Since(start)
					}
				}()
			}
			wg.Wait()
			total := time.Since(started)
			close(failures)
			for err := range failures {
				require.NoError(t, err)
			}
			statements := trace.count.Load()
			stats := sqlDB.Stats()
			var logCount int64
			require.NoError(t, db.Model(&Log{}).Count(&logCount).Error)
			require.EqualValues(t, requests, logCount)
			for id := 1; id <= 8; id++ {
				var user User
				require.NoError(t, db.First(&user, id).Error)
				require.EqualValues(t, requests/8*300, user.UsedQuota)
				require.EqualValues(t, 1000000000-requests/8*300, user.Quota)
			}
			sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
			t.Logf("PERF workers=%d requests=%d total_ms=%.3f throughput=%.2f p50_ms=%.3f p95_ms=%.3f sql_per_request=%.2f redis_per_request=0 db_wait_count=%d db_wait_ms=%.3f", workers, requests, float64(total.Microseconds())/1000, float64(requests)/total.Seconds(), float64(elapsed[requests/2].Microseconds())/1000, float64(elapsed[requests*95/100].Microseconds())/1000, float64(statements)/requests, stats.WaitCount-startStats.WaitCount, float64((stats.WaitDuration-startStats.WaitDuration).Microseconds())/1000)
		})
	}
}
