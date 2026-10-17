package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGmpayDisabledNewOrdersPreservesExistingPayments(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "disabled-gmpay", AffCode: "disabled-gmpay", Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	oldPayment := *operation_setting.GetPaymentSetting()
	oldDisplay, oldMinimum := operation_setting.GetQuotaDisplayType(), operation_setting.MinTopUp
	oldTransport := http.DefaultTransport
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = oldPayment
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.MinTopUp = oldMinimum
		http.DefaultTransport = oldTransport
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.MinTopUp = 1
	var gatewayCalls atomic.Int32
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer gateway.Close()
	http.DefaultTransport = gateway.Client().Transport
	config := setting.GmpayConfig{PayAddress: gateway.URL, MerchantID: "merchant", SecretKey: "old-secret", Enabled: true, Assets: []setting.GmpayAsset{{Name: "USDT", Token: "usdt", Network: "tron"}}}
	require.NoError(t, service.SaveGmpayConfig(context.Background(), config))
	orders := []string{"callback-first", "manual-first", "concurrent"}
	for _, number := range orders {
		order := model.TopUp{UserId: user.Id, TradeNo: number, Amount: 1, Money: 7.3, PaymentProvider: model.PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending}
		require.NoError(t, model.CreateGmpayOrder(context.Background(), &order, &model.GmpayOrder{ExpectedQuota: 500000, AmountMinor: 730, Currency: "CNY", Token: "usdt", Network: "tron"}, &model.GmpayMerchantVersion{GatewayURL: config.PayAddress, MerchantID: config.MerchantID, Key: config.SecretKey}))
	}
	config.Enabled, config.SecretKey = false, "rotated-secret"
	config.Assets = nil
	require.NoError(t, service.SaveGmpayConfig(context.Background(), config))
	// A stale, previously accepted option has never been an authority for funds.
	require.NoError(t, db.Create(&model.Option{Key: "payment_setting.gmpay_settlement_enabled", Value: "false"}).Error)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	engine.POST("/pay", GmpayPay)
	engine.POST("/notify", GmpayNotify)
	request := httptest.NewRequest(http.MethodPost, "/pay", strings.NewReader(`{"amount":1,"payment_method":"gmpay:usdt.tron"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	assert.Contains(t, response.Body.String(), "Payment method is unavailable")
	assert.Zero(t, gatewayCalls.Load(), "disabled orders must never reach the gateway")
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.EqualValues(t, len(orders), count, "rejected new orders must not be persisted")
	for i, number := range orders {
		t.Run(number, func(t *testing.T) {
			params := map[string]string{"pid": "merchant", "trade_id": "receipt-" + number, "order_id": number, "amount": "7.30", "token": "usdt", "actual_amount": "1.00", "receive_address": "address", "block_transaction_id": "transaction-" + number, "status": "2"}
			var err error
			params["signature"], err = gmpay.Sign(params, "old-secret")
			require.NoError(t, err)
			body, err := common.Marshal(params)
			require.NoError(t, err)
			callback := func() error {
				request := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				if response.Code != http.StatusOK || response.Body.String() != "success" {
					return fmt.Errorf("callback rejected: %d %s", response.Code, response.Body.String())
				}
				return nil
			}
			manual := func() error { return model.ManualCompleteTopUp(number, "") }
			switch number {
			case "callback-first":
				require.NoError(t, callback())
				require.NoError(t, manual())
			case "manual-first":
				require.NoError(t, manual())
				require.NoError(t, callback())
			case "concurrent":
				start, results := make(chan struct{}), make(chan error, 2)
				go func() { <-start; results <- callback() }()
				go func() { <-start; results <- manual() }()
				close(start)
				for range 2 {
					assert.NoError(t, <-results)
				}
			}
			require.NoError(t, callback(), "repeated valid callbacks still acknowledge success")
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, (i+1)*500000, user.Quota, "each existing order credits exactly its recorded quota once")
		})
	}
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).Count(&logs).Error)
	assert.EqualValues(t, 3, logs)
}
