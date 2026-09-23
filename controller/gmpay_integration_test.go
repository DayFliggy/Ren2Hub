package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This exercises real HTTP on both sides, with isolated storage and no chain or
// merchant credentials. Authentication is supplied only by this test fixture.
func TestGmpayHTTPConfigurationCheckoutAndSettlement(t *testing.T) {
	gateway := gmpayControllerTestGateway(t)
	db := setupGmpayControllerTestDB(t, gateway.URL)
	withOptionMap(t, map[string]string{})
	oldAddress, oldID, oldKey, oldAssets := setting.GmpayPayAddress, setting.GmpayId, setting.GmpayKey, setting.GmpayAssets
	oldPrice, oldMinimum, oldRatios := setting.GmpayPrice, setting.GmpayMinTopUp, setting.GmpayGroupRatio
	t.Cleanup(func() {
		setting.GmpayPayAddress, setting.GmpayId, setting.GmpayKey, setting.GmpayAssets = oldAddress, oldID, oldKey, oldAssets
		setting.GmpayPrice, setting.GmpayMinTopUp, setting.GmpayGroupRatio = oldPrice, oldMinimum, oldRatios
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 1) })
	engine.POST("/api/option/gmpay/save", SaveGmpay)
	engine.GET("/api/option/", GetOptions)
	engine.GET("/api/next/wallet/gmpay/config", NextGetGmpayTopUpConfig)
	engine.GET("/api/next/wallet/config", NextGetEpayTopUpConfig)
	engine.POST("/api/next/wallet/topup/quote", NextQuoteEpayTopUp)
	engine.POST("/api/next/wallet/gmpay/quote", NextQuoteGmpayTopUp)
	engine.POST("/api/next/wallet/gmpay/topup", NextCreateGmpayTopUp)
	engine.POST("/api/user/gmpay/notify", GmpayNotify)
	engine.GET("/api/next/admin/orders/:order_no", NextGetAdminOrder)
	merchant := httptest.NewServer(engine)
	t.Cleanup(merchant.Close)
	post := func(path string, payload any) []byte {
		t.Helper()
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		response, err := merchant.Client().Post(merchant.URL+path, "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		body, err = io.ReadAll(response.Body)
		require.NoError(t, err)
		return body
	}
	get := func(path string) []byte {
		t.Helper()
		response, err := merchant.Client().Get(merchant.URL + path)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return body
	}

	config := saveGmpayRequest{
		PayAddress: gateway.URL, MerchantID: "1", SecretKey: gmpayControllerTestSecret,
		Assets: `[{"name":"USDT / TRON","token":"usdt","network":"tron"}]`,
		Price:  1, MinTopUp: 1, GroupRatio: `{"default":1}`,
	}
	assert.Contains(t, string(post("/api/option/gmpay/save", config)), `"success":true`)
	options := string(get("/api/option/"))
	assert.Contains(t, options, "GmpayGroupRatio")
	assert.NotContains(t, options, "GmpayKey")
	assert.NotContains(t, options, gmpayControllerTestSecret)
	assert.Contains(t, string(get("/api/next/wallet/gmpay/config")), `"enable_gmpay_topup":true`)
	assert.Contains(t, string(get("/api/next/wallet/config")), `"enable_online_topup":false`, "an unconfigured EPay rail does not block GMPay")

	request := service.GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "usdt.tron"}
	var quote struct {
		Success bool               `json:"success"`
		Data    service.GmpayQuote `json:"data"`
	}
	require.NoError(t, common.Unmarshal(post("/api/next/wallet/gmpay/quote", request), &quote))
	require.True(t, quote.Success)
	assert.Equal(t, "10.00", quote.Data.PayAmount)
	assert.Equal(t, "USD", quote.Data.Currency)
	for key, value := range map[string]string{
		"PayAddress": "https://epay.example.test", "EpayId": "2", "EpayKey": "epay-test-only-key",
		"PayMethods": `[{"name":"Alipay","type":"alipay"}]`, "Price": "7.3",
	} {
		require.NoError(t, db.Save(&model.Option{Key: key, Value: value}).Error)
	}
	assert.Contains(t, string(get("/api/next/wallet/config")), `"enable_online_topup":true`)
	assert.Contains(t, string(get("/api/next/wallet/gmpay/config")), `"enable_gmpay_topup":true`)
	var epayQuote struct {
		Success bool              `json:"success"`
		Data    service.EpayQuote `json:"data"`
	}
	require.NoError(t, common.Unmarshal(post("/api/next/wallet/topup/quote", service.EpayQuoteRequest{
		Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay",
	}), &epayQuote))
	require.True(t, epayQuote.Success)
	assert.Equal(t, "CNY", epayQuote.Data.Currency)
	assert.Equal(t, "73.00", epayQuote.Data.PayAmount)
	request.QuoteFingerprint = quote.Data.QuoteFingerprint
	var checkout struct {
		Success bool `json:"success"`
		Data    struct {
			TradeNo string `json:"trade_no"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(post("/api/next/wallet/gmpay/topup", request), &checkout))
	require.True(t, checkout.Success)
	require.NotEmpty(t, checkout.Data.TradeNo)
	assert.Equal(t, "https://cashier.example.test/pay/checkout-counter/gateway-trade-1", checkout.Data.URL)

	// Disabling new crypto orders retains the credential and old-order callbacks.
	config.Assets, config.SecretKey = "[]", ""
	assert.Contains(t, string(post("/api/option/gmpay/save", config)), `"success":true`)
	assert.Contains(t, string(get("/api/next/wallet/gmpay/config")), `"enable_gmpay_topup":false`)
	assert.Contains(t, string(get("/api/next/wallet/config")), `"enable_online_topup":true`)
	loaded, err := service.LoadGmpayConfig(t.Context())
	require.NoError(t, err)
	assert.Equal(t, gmpayControllerTestSecret, loaded.Merchant.Key)

	params := map[string]string{
		"pid": "1", "trade_id": "gateway-trade-1", "order_id": checkout.Data.TradeNo,
		"amount": "10", "actual_amount": "9.998765", "token": "USDT",
		"receive_address": "final-test-address", "block_transaction_id": "test-chain-transaction", "status": "2",
	}
	signature, err := gmpay.Sign(params, gmpayControllerTestSecret)
	require.NoError(t, err)
	callback := map[string]any{}
	for key, value := range params {
		callback[key] = value
	}
	callback["amount"], callback["actual_amount"], callback["status"] = 10.0, 9.998765, 2
	callback["signature"] = signature
	assert.Equal(t, "success", string(post("/api/user/gmpay/notify", callback)))
	assert.Equal(t, "success", string(post("/api/user/gmpay/notify", callback)))
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(5_000_007), user.Quota)
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events)
	_, snapshot, _, err := model.GetGmpayOrder(t.Context(), checkout.Data.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "9.998765", snapshot.SettledCryptoAmount)
	assert.Equal(t, "final-test-address", snapshot.SettledReceiveAddress)
	assert.Equal(t, "test-chain-transaction", snapshot.SettledTransactionID)
	detail := string(get("/api/next/admin/orders/" + checkout.Data.TradeNo))
	assert.Contains(t, detail, `"settled_transaction_id":"test-chain-transaction"`)
	assert.Contains(t, detail, `"status":"settled"`)
	assert.NotContains(t, detail, gmpayControllerTestSecret)
	assert.NotContains(t, detail, "merchant_version_id")
	assert.NotContains(t, detail, "account_id")
}
