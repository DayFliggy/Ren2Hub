package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestGmpayCreationOutcomeAndEarlyCallback(t *testing.T) {
	for _, outcome := range []string{"early callback", "unknown outcome", "gateway rejection", "early callback then failure"} {
		t.Run(outcome, func(t *testing.T) {
			oldDB, oldLog, oldRedis, oldTransport, oldAddress := model.DB, model.LOG_DB, common.RedisEnabled, http.DefaultTransport, system_setting.ServerAddress
			oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gmpay.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.TopUp{}, &model.GmpayMerchantVersion{}, &model.GmpayOrder{}, &model.Log{}))
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			model.DB, model.LOG_DB = db, db
			common.RedisEnabled = false
			system_setting.ServerAddress = "https://console.example.test"
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			common.SetLogDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLog
				common.RedisEnabled = oldRedis
				http.DefaultTransport = oldTransport
				system_setting.ServerAddress = oldAddress
				common.SetMainDatabaseType(oldType)
				common.SetLogDatabaseType(oldLogType)
				sqlDB.Close()
			})
			user := model.User{Username: "payer", AffCode: "payer"}
			require.NoError(t, db.Create(&user).Error)
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if outcome == "unknown outcome" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if outcome == "gateway rejection" {
					_, _ = w.Write([]byte(`{"status_code":10004,"data":null}`))
					return
				}
				if !assert.NoError(t, r.ParseForm()) {
					return
				}
				callback := map[string]string{"pid": "merchant", "trade_id": "remote-order", "order_id": r.Form.Get("order_id"), "amount": "7.30", "token": "usdt", "actual_amount": "1.00", "receive_address": "address", "block_transaction_id": "transaction", "status": "2"}
				signature, err := gmpay.Sign(callback, "old-secret")
				if !assert.NoError(t, err) {
					return
				}
				callback["signature"] = signature
				assert.NoError(t, SettleGmpayNotification(context.Background(), callback, ""), "callback must find the order before creation returns")
				if outcome == "early callback then failure" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				body, err := common.Marshal(map[string]any{"status_code": 200, "data": map[string]any{"trade_id": "remote-order", "order_id": r.Form.Get("order_id"), "amount": "7.30", "currency": "CNY", "token": "USDT", "actual_amount": "1.00", "receive_address": "address", "status": 2, "expiration_time": 1893456000, "payment_url": "https://cashier.example.test/pay"}})
				if !assert.NoError(t, err) {
					return
				}
				_, err = w.Write(body)
				assert.NoError(t, err)
			}))
			defer server.Close()
			http.DefaultTransport = server.Client().Transport
			config := setting.GmpayConfig{PayAddress: server.URL, MerchantID: "merchant", SecretKey: "old-secret", Enabled: true, Assets: []setting.GmpayAsset{{Name: "USDT", Token: "usdt", Network: "tron"}}}
			order := &model.TopUp{UserId: user.Id, Amount: 1, Money: 7.3, PaymentMethod: "gmpay:usdt.tron"}
			created, err := CreateGmpayTopUp(context.Background(), GmpayOrderRequest{Order: order, ExpectedQuota: 500000, PayAmount: "7.30", ReturnURL: "https://console.example.test/wallet"}, config)
			if outcome != "early callback" {
				require.Error(t, err)
				require.NotNil(t, created)
				assert.Equal(t, order.TradeNo, created.Order.TradeNo)
				require.NotNil(t, created.Payment)
			} else {
				require.NoError(t, err)
			}
			assert.EqualValues(t, 1, calls.Load(), "creation is never automatically retried")
			var payment model.GmpayOrder
			require.NoError(t, db.First(&payment, order.Id).Error)
			require.NoError(t, db.First(&user, user.Id).Error)
			if outcome == "unknown outcome" || outcome == "gateway rejection" {
				want := model.GmpayStatusUnknown
				if outcome == "gateway rejection" {
					want = model.GmpayStatusFailed
				}
				assert.Equal(t, want, payment.Status)
				assert.Equal(t, want, created.Payment.Status)
				assert.Zero(t, user.Quota)
			} else {
				assert.Equal(t, model.GmpayStatusSettled, payment.Status)
				assert.Equal(t, 500000, user.Quota)
				if outcome == "early callback then failure" {
					assert.Equal(t, model.GmpayStatusSettled, created.Payment.Status)
				}
			}
			config.SecretKey = "rotated-secret"
			require.NoError(t, SaveGmpayConfig(context.Background(), config))
			callback := map[string]string{"pid": "merchant", "trade_id": "remote-order", "order_id": order.TradeNo, "amount": "7.30", "token": "usdt", "actual_amount": "1.00", "receive_address": "address", "block_transaction_id": "transaction", "status": "2"}
			// 签名有效但法币金额与订单冻结的应付值不符：拒绝且不入账。
			tampered := map[string]string{}
			for key, value := range callback {
				tampered[key] = value
			}
			tampered["amount"] = "7.31"
			tampered["signature"], err = gmpay.Sign(tampered, "old-secret")
			require.NoError(t, err)
			quotaBefore := user.Quota
			err = SettleGmpayNotification(context.Background(), tampered, "")
			assert.Equal(t, "PAYMENT_MISMATCH", GmpayFailureCode(err))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, quotaBefore, user.Quota)
			callback["signature"], err = gmpay.Sign(callback, "old-secret")
			require.NoError(t, err)
			require.NoError(t, SettleGmpayNotification(context.Background(), callback, ""), "old order stays bound to its credential version after rotation")
			require.NoError(t, SettleGmpayNotification(context.Background(), callback, ""))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 500000, user.Quota)
		})
	}
}
