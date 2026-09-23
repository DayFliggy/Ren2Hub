package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const gmpayControllerTestSecret = "controller-test-key"

// setupGmpayControllerTestDB mirrors setupEpayControllerTestDB for the crypto
// rail: the same option surface, plus the two GMPay tables that hold the frozen
// snapshot and the credential version.
func setupGmpayControllerTestDB(t *testing.T, gateway string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.GmpayOrder{}, &model.GmpayMerchantVersion{}, &model.User{}, &model.TopUp{}, &model.BillingEvent{}, &model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousAffiliate := common.RedisEnabled, common.AffiliateEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.AffiliateEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.AffiliateEnabled = previousRedis, previousAffiliate
		common.SetDatabaseTypes(previousMainType, previousLogType)
		require.NoError(t, connection.Close())
	})
	options := map[string]string{
		"GmpayPayAddress": gateway, "GmpayId": "1", "GmpayKey": gmpayControllerTestSecret,
		"GmpayPrice": "1", "GmpayMinTopUp": "1", "GmpayGroupRatio": `{"default":1}`,
		"GmpayAssets":                              `[{"name":"USDT-TRON","token":"usdt","network":"tron"}]`,
		"payment_setting.amount_discount":          "{}",
		"payment_setting.compliance_confirmed":     "true",
		"payment_setting.compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"general_setting.quota_display_type":       "USD",
		"ServerAddress":                            "https://shop.example.test",
	}
	for key, value := range options {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "gmpay-user", Group: "default", Quota: 7, Status: common.UserStatusEnabled}).Error)
	// Every case here reaches the gateway through the shared outbound client,
	// which only exists once the process initializes it.
	gmpayControllerTestHTTPClient(t)
	return db
}

// gmpayControllerTestHTTPClient points the shared outbound client at the test
// gateway's certificate and restores the production stack afterwards.
func gmpayControllerTestHTTPClient(t *testing.T) {
	t.Helper()
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = true
	service.InitHttpClient()
	t.Cleanup(func() {
		common.TLSInsecureSkipVerify = previous
		service.InitHttpClient()
	})
}

// gmpayControllerTestGateway answers the documented creation envelope and echoes
// the order number it was given back to the caller.
func gmpayControllerTestGateway(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/payments/gmpay/v1/order/create-transaction"), "unexpected gateway path %s", r.URL.Path)
		assert.NoError(t, r.ParseForm())
		assert.NotEmpty(t, r.PostForm.Get("signature"), "the gateway request must be signed")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{` +
			`"trade_id":"gateway-trade-1","order_id":"` + r.PostForm.Get("order_id") + `",` +
			`"amount":` + r.PostForm.Get("amount") + `,"currency":"usd","actual_amount":"9.998765",` +
			`"receive_address":"TXtestReceiveAddress","token":"usdt","status":1,` +
			`"expiration_time":1893456000,` +
			`"payment_url":"https://cashier.example.test/pay/checkout-counter/gateway-trade-1"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// gmpayControllerTestNotify posts a signed callback through the registered
// handler, so the acknowledgement the gateway sees is the one under test.
func gmpayControllerTestNotify(t *testing.T, orderNo string, overrides map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	params := map[string]string{
		"pid": "1", "trade_id": "gateway-trade-1", "order_id": orderNo, "token": "usdt",
		"block_transaction_id": "0xdeadbeef", "amount": "10.00", "actual_amount": "9.998765",
		"receive_address": "TXtestReceiveAddress",
		"status":          "2",
	}
	for name, value := range overrides {
		params[name] = value
	}
	signature, err := gmpay.Sign(params, gmpayControllerTestSecret)
	require.NoError(t, err)
	params["signature"] = signature
	body, err := common.Marshal(params)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/notify", GmpayNotify)
	engine.POST("/notify", GmpayNotify)
	request := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

// gmpayControllerTestOrder runs one order through the shared workflow, so every
// callback is exercised against a snapshot creation actually produced.
func gmpayControllerTestOrder(t *testing.T, db *gorm.DB) *service.GmpayCreatedOrder {
	t.Helper()
	order, err := service.CreateGmpayTopUp(context.Background(), 1, service.GmpayQuoteRequest{
		Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron",
	})
	require.NoError(t, err)
	var snapshot model.GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
	assert.Equal(t, model.GmpayStatusReady, snapshot.Status)
	assert.Equal(t, "10.00", snapshot.GatewayAmount)
	assert.Equal(t, "usdt", snapshot.Token)
	assert.Equal(t, "tron", snapshot.Network)
	return order
}

// gmpayControllerTestPendingOrder stores an order whose creation call has not
// returned yet, which is exactly the state a callback may race against.
func gmpayControllerTestPendingOrder(t *testing.T, db *gorm.DB) (*model.TopUp, *model.GmpayOrder, *model.GmpayMerchantVersion) {
	t.Helper()
	order := &model.TopUp{
		UserId: 1, Amount: 10, Money: 10, TradeNo: gmpayControllerTestTradeNo('b'),
		PaymentMethod: "gmpay:usdt.tron", PaymentProvider: model.PaymentProviderGmpay,
		CreateTime: 1_700_000_000, Status: common.TopUpStatusPending,
	}
	snapshot := &model.GmpayOrder{
		InputAmount: 10, AmountUnit: "USD", AmountMinor: 1000, Currency: "USD",
		ExpectedQuota: 5_000_000, UnitPrice: "1", GroupRatio: "1", Discount: "1",
		Token: "usdt", Network: "tron", QuoteFingerprint: "controller-test-quote",
	}
	merchant := &model.GmpayMerchantVersion{
		GatewayURL: "https://payments.example.test", MerchantID: "1", Key: gmpayControllerTestSecret,
	}
	require.NoError(t, model.CreateGmpayOrder(context.Background(), order, snapshot, merchant))
	require.NoError(t, db.First(snapshot, order.Id).Error)
	assert.Equal(t, model.GmpayStatusCreating, snapshot.Status)
	return order, snapshot, merchant
}

// gmpayControllerTestTradeNo is exactly 32 characters, the gateway's documented
// ceiling, and distinct per suite so no case can touch another case's order.
func gmpayControllerTestTradeNo(suite rune) string {
	return "GP" + string(suite) + "000000000000000000000000000"
}

// TestGmpayNotifyAcknowledgementSemantics pins the answers the gateway can
// receive. Only a settled callback stops the retry budget, so a callback the
// gateway could retry successfully must never be acknowledged as done.
func TestGmpayNotifyAcknowledgementSemantics(t *testing.T) {
	server := gmpayControllerTestGateway(t)
	db := setupGmpayControllerTestDB(t, server.URL)
	gmpayControllerTestHTTPClient(t)
	order := gmpayControllerTestOrder(t, db)

	t.Run("settled callback is acknowledged", func(t *testing.T) {
		response := gmpayControllerTestNotify(t, order.Order.TradeNo, nil)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "success", response.Body.String())
		var user model.User
		require.NoError(t, db.First(&user, 1).Error)
		assert.Equal(t, common.Quota(5_000_007), user.Quota)
		var stored model.TopUp
		require.NoError(t, db.First(&stored, order.Order.Id).Error)
		assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
	})

	t.Run("repeated callback stays acknowledged and credits once", func(t *testing.T) {
		response := gmpayControllerTestNotify(t, order.Order.TradeNo, nil)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "success", response.Body.String())
		var events int64
		require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
		assert.EqualValues(t, 1, events)
	})

	t.Run("malformed callback is rejected without an ack", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.POST("/notify", GmpayNotify)
		request := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"order_id":"GP-x"}`))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Equal(t, "fail", response.Body.String())
	})

	t.Run("another token keeps the order pending", func(t *testing.T) {
		second, _, _ := gmpayControllerTestPendingOrder(t, db)
		response := gmpayControllerTestNotify(t, second.TradeNo, map[string]string{
			"token": "usdc", "trade_id": "gateway-trade-2",
		})
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "fail", response.Body.String(), "a callback that did not settle must be retried by the gateway")
		var stored model.TopUp
		require.NoError(t, db.First(&stored, second.Id).Error)
		assert.Equal(t, common.TopUpStatusPending, stored.Status)
	})
}

// TestGmpayCallbackArrivingBeforeTheCreationResponse covers the first ordering
// window: the gateway settles an order the local snapshot still records as being
// created. The payment is credited, and the late creation result cannot reopen
// or double-credit the order.
func TestGmpayCallbackArrivingBeforeTheCreationResponse(t *testing.T) {
	db := setupGmpayControllerTestDB(t, "https://payments.example.test")
	order, _, _ := gmpayControllerTestPendingOrder(t, db)

	response := gmpayControllerTestNotify(t, order.TradeNo, nil)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "success", response.Body.String(), "an early callback settles the order it belongs to")

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(5_000_007), user.Quota)

	err := model.MarkGmpayOrderResult(context.Background(), order.Id, model.GmpayOrderResult{
		Status: model.GmpayStatusReady, GatewayTradeNo: "gateway-trade-1", GatewayAmount: "10.00",
		GatewayCryptoAmount: "9.998765", ReceiveAddress: "TXtestReceiveAddress", ExpirationTime: 1893456000,
	})
	require.NoError(t, err, "a matching late creation response must preserve the settled order")

	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events)
}

// TestGmpayCreationTimeoutKeepsTheOrderForReview covers the second ordering
// window: the create call cannot be determined. The local order must survive
// untouched, keep its number, and be visible as an unknown result rather than
// silently retried, because the gateway create is not idempotent.
func TestGmpayCreationTimeoutKeepsTheOrderForReview(t *testing.T) {
	unreachable := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachable.Close()
	db := setupGmpayControllerTestDB(t, unreachable.URL)
	gmpayControllerTestHTTPClient(t)

	_, err := service.CreateGmpayTopUp(context.Background(), 1, service.GmpayQuoteRequest{
		Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron",
	})
	require.Error(t, err)

	var orders []model.TopUp
	require.NoError(t, db.Find(&orders).Error)
	require.Len(t, orders, 1, "an undetermined creation keeps exactly one local order")
	assert.Equal(t, common.TopUpStatusPending, orders[0].Status)
	assert.Len(t, orders[0].TradeNo, 32, "the order number must fit the gateway bound of 32 characters")

	var snapshot model.GmpayOrder
	require.NoError(t, db.First(&snapshot, orders[0].Id).Error)
	assert.Equal(t, model.GmpayStatusUnknown, snapshot.Status)
	assert.Equal(t, "TRANSPORT", snapshot.LastFailure)
	assert.Empty(t, snapshot.ReceiveAddress, "an unreachable gateway has no receipt to store")

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(7), user.Quota)
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

// TestGmpayGatewayRejectionIsTerminalAndRetryable separates the third window:
// the gateway answered a business rejection. That is a determined failure, so
// the order is marked failed and a new attempt is still allowed to succeed.
func TestGmpayGatewayRejectionIsTerminalAndRetryable(t *testing.T) {
	reject := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if reject {
			_, _ = w.Write([]byte(`{"status_code":10004,"message":"invalid payment amount","data":null}`))
			return
		}
		assert.NoError(t, r.ParseForm())
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{` +
			`"trade_id":"gateway-trade-retry","order_id":"` + r.PostForm.Get("order_id") + `",` +
			`"amount":10.00,"currency":"usd","actual_amount":"9.998765",` +
			`"receive_address":"TXtestReceiveAddress","token":"usdt","status":1,` +
			`"expiration_time":1893456000,` +
			`"payment_url":"https://cashier.example.test/pay/checkout-counter/gateway-trade-retry"}}`))
	}))
	t.Cleanup(server.Close)
	db := setupGmpayControllerTestDB(t, server.URL)
	gmpayControllerTestHTTPClient(t)

	_, err := service.CreateGmpayTopUp(context.Background(), 1, service.GmpayQuoteRequest{
		Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron",
	})
	require.Error(t, err)
	var snapshot model.GmpayOrder
	require.NoError(t, db.First(&snapshot).Error)
	assert.Equal(t, model.GmpayStatusFailed, snapshot.Status)
	assert.Equal(t, "GATEWAY_REJECTED", snapshot.LastFailure)
	assert.Empty(t, snapshot.ReceiveAddress)

	reject = false
	order, err := service.CreateGmpayTopUp(context.Background(), 1, service.GmpayQuoteRequest{
		Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://cashier.example.test/pay/checkout-counter/gateway-trade-retry", order.PaymentURL)

	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "a rejected attempt keeps its order and a retry creates a new one")
}

// TestRequestGmpayReturnsValidatedCashierURL pins the wallet contract: the
// address handed to the browser is the gateway cashier, and the chain and
// payable amount are echoed from the frozen quote rather than taken from the
// request. Credentials must never reach the client.
func TestRequestGmpayReturnsValidatedCashierURL(t *testing.T) {
	server := gmpayControllerTestGateway(t)
	setupGmpayControllerTestDB(t, server.URL)
	gmpayControllerTestHTTPClient(t)

	body, err := common.Marshal(GmpayRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/gmpay/request", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	RequestGmpay(c)

	assert.Equal(t, http.StatusOK, response.Code)
	rendered := response.Body.String()
	assert.Contains(t, rendered, `"url":"https://cashier.example.test/pay/checkout-counter/gateway-trade-1"`)
	assert.Contains(t, rendered, `"currency":"USD"`)
	assert.Contains(t, rendered, `"pay_amount":"10.00"`)
	assert.Contains(t, rendered, `"token":"usdt"`)
	assert.Contains(t, rendered, `"network":"tron"`)
	assert.Contains(t, rendered, `"expected_quota":"5000000"`)
	assert.NotContains(t, rendered, gmpayControllerTestSecret, "merchant credentials must never reach a client")
}

// TestGmpayGatewayRetriesAfterSettlementFailure proves the acknowledgement is
// tied to settlement rather than to receipt: a paused switch or a failed funds
// transaction keeps the order pending with nothing credited, and the retried
// callback then settles it exactly once.
func TestGmpayGatewayRetriesAfterSettlementFailure(t *testing.T) {
	for _, failure := range []string{"settlement paused", "funds transaction failed"} {
		t.Run(failure, func(t *testing.T) {
			server := gmpayControllerTestGateway(t)
			db := setupGmpayControllerTestDB(t, server.URL)
			gmpayControllerTestHTTPClient(t)
			order := gmpayControllerTestOrder(t, db)

			switch failure {
			case "settlement paused":
				require.NoError(t, db.Create(&model.Option{Key: "payment_setting.gmpay_settlement_enabled", Value: "false"}).Error)
			case "funds transaction failed":
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_gmpay_credit", func(tx *gorm.DB) {
					if tx.Statement.Table == "billing_events" {
						tx.AddError(errors.New("injected ledger failure"))
					}
				}))
				t.Cleanup(func() { db.Callback().Create().Remove("reject_gmpay_credit") })
			}

			response := gmpayControllerTestNotify(t, order.Order.TradeNo, nil)
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "fail", response.Body.String(), "the gateway must receive a failed acknowledgement so it can retry")

			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, common.Quota(7), user.Quota)
			var stored model.TopUp
			require.NoError(t, db.First(&stored, order.Order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, stored.Status)
			var snapshot model.GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
			require.NotNil(t, snapshot.GatewayTradeNo)
			assert.Equal(t, "gateway-trade-1", *snapshot.GatewayTradeNo, "creation evidence survives a rejected settlement")
			var events int64
			require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
			assert.Zero(t, events)

			switch failure {
			case "settlement paused":
				require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "payment_setting.gmpay_settlement_enabled"}).Update("value", "true").Error)
			case "funds transaction failed":
				require.NoError(t, db.Callback().Create().Remove("reject_gmpay_credit"))
			}

			retried := gmpayControllerTestNotify(t, order.Order.TradeNo, nil)
			assert.Equal(t, http.StatusOK, retried.Code)
			assert.Equal(t, "success", retried.Body.String())
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, common.Quota(5_000_007), user.Quota)
			require.NoError(t, db.First(&stored, order.Order.Id).Error)
			assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
			assert.Equal(t, common.Quota(5_000_000), stored.CreditedQuota)
			require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
			assert.EqualValues(t, 1, events)
		})
	}
}

// TestGmpaySettlementFailureLeavesNoLocalMoney proves a well-formed but unprovable
// callback cannot move the wallet: only the frozen fiat amount authorizes a
// credit.
func TestGmpaySettlementFailureLeavesNoLocalMoney(t *testing.T) {
	server := gmpayControllerTestGateway(t)
	db := setupGmpayControllerTestDB(t, server.URL)
	gmpayControllerTestHTTPClient(t)
	order := gmpayControllerTestOrder(t, db)

	for _, amount := range []string{"10.01", "9.99"} {
		t.Run(amount, func(t *testing.T) {
			response := gmpayControllerTestNotify(t, order.Order.TradeNo, map[string]string{"amount": amount, "trade_id": "gateway-trade-amount"})
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "fail", response.Body.String())
		})
	}

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(7), user.Quota)
	var stored model.TopUp
	require.NoError(t, db.First(&stored, order.Order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

// TestGmpayPendingStatusNotificationKeepsTheOrderOpen covers the well-formed but
// not-yet-paid callback. It must not acknowledge payment completion, and the
// order stays open for a valid successful payment notification.
func TestGmpayPendingStatusNotificationKeepsTheOrderOpen(t *testing.T) {
	server := gmpayControllerTestGateway(t)
	db := setupGmpayControllerTestDB(t, server.URL)
	gmpayControllerTestHTTPClient(t)
	order := gmpayControllerTestOrder(t, db)

	response := gmpayControllerTestNotify(t, order.Order.TradeNo, map[string]string{"status": "1"})
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "fail", response.Body.String())

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(7), user.Quota)
	var stored model.TopUp
	require.NoError(t, db.First(&stored, order.Order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
}

// TestAdminGmpayCompletionAuditIdentifiesOrderAndOperator extends the
// administrative completion contract to the crypto rail: the frozen quota is
// credited, the audit names the order and the operator, and a repeated request
// credits once.
func TestAdminGmpayCompletionAuditIdentifiesOrderAndOperator(t *testing.T) {
	db := setupGmpayControllerTestDB(t, "https://payments.example.test")
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "payment-admin", AffCode: "payment-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	order, snapshot, _ := gmpayControllerTestPendingOrder(t, db)

	body, err := common.Marshal(AdminCompleteTopupRequest{TradeNo: order.TradeNo})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	for range 2 {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/complete", strings.NewReader(string(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", 2)
		c.Set("username", "payment-admin")
		c.Set("role", common.RoleAdminUser)
		AdminCompleteTopUp(c)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `{"success":true,"message":"","data":null}`, response.Body.String())
	}

	var entries []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Order("id").Find(&entries).Error)
	require.Len(t, entries, 2, "each successful administrative request must identify the target order")
	for _, entry := range entries {
		audit := buildOperationLogDTO(&entry)
		assert.Equal(t, "user.topup_complete", audit.Action)
		assert.Equal(t, "payment-admin", audit.Actor.Username)
		assert.Equal(t, 2, entry.UserId)
		assert.Equal(t, order.TradeNo, audit.Params["trade_no"])
		assert.Contains(t, audit.Content, order.TradeNo)
	}

	var stored model.TopUp
	require.NoError(t, db.First(&stored, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
	assert.Equal(t, snapshot.ExpectedQuota, stored.CreditedQuota, "manual completion must credit the frozen quota, never a recomputed one")
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(7)+snapshot.ExpectedQuota, user.Quota)
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events, "repeated completion must credit only once")
}
