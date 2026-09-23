package gmpay

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// documentedSecret and documentedCanonical come from the gateway's own API
// documentation (wiki/API.md): the example assumes secret_key
// "epusdt_secret_key" and publishes both the canonical string and the resulting
// digest. Cross-checking against that pair proves this implementation agrees
// with the gateway's spec rather than merely with itself.
const documentedSecret = "epusdt_secret_key"

func documentedParams() map[string]string {
	return map[string]string{
		"pid":          "1000",
		"order_id":     "ORD202605230001",
		"currency":     "cny",
		"token":        "usdt",
		"network":      "tron",
		"amount":       "100",
		"notify_url":   "https://merchant.example/notify",
		"redirect_url": "https://merchant.example/return",
		"name":         "VIP",
	}
}

func TestCanonicalMatchesGatewayDocumentation(t *testing.T) {
	assert.Equal(t,
		"amount=100&currency=cny&name=VIP&network=tron&notify_url=https://merchant.example/notify&order_id=ORD202605230001&pid=1000&redirect_url=https://merchant.example/return&token=usdt",
		Canonical(documentedParams()))
}

func TestSignMatchesGatewayDocumentedDigest(t *testing.T) {
	signature, err := Sign(documentedParams(), documentedSecret)
	require.NoError(t, err)
	assert.Equal(t, "6f874b1919d95081835e2809b620e354a5866f5a6dbb2e432d1627f1eb10059d", signature)
}

func TestCanonicalExcludesSignatureAndEmptyValues(t *testing.T) {
	params := map[string]string{"b": "2", "a": "1", "empty": "", "signature": "old"}
	assert.Equal(t, "a=1&b=2", Canonical(params))
	assert.Equal(t, "old", params["signature"], "signing must not mutate the input map")
	assert.Len(t, params, 4)
}

func TestCanonicalSortsCompletePairsLikeGateway(t *testing.T) {
	// epusdt sorts the complete key=value strings, including extension fields.
	// Sorting only field names differs when one name prefixes another.
	assert.Equal(t, "amount-extra=1&amount=73", Canonical(map[string]string{
		"amount": "73", "amount-extra": "1",
	}))
}

func TestSignRequiresKey(t *testing.T) {
	_, err := Sign(map[string]string{"a": "1"}, "")
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// The gateway reformats JSON numbers through float64, so a literal such as
// "100.00" signs as "100". Formatting must therefore stay under our control.
func TestFormatFiatAmountKeepsTwoDecimals(t *testing.T) {
	for _, test := range []struct {
		cents int64
		want  string
	}{
		{10000, "100.00"}, {1, "0.01"}, {7300, "73.00"}, {5, "0.05"}, {999999, "9999.99"},
	} {
		assert.Equal(t, test.want, FormatFiatAmount(test.cents), test.cents)
	}
}

func TestParseFiatAmountIsExactCents(t *testing.T) {
	for _, test := range []struct {
		raw   string
		cents int64
	}{
		{"1", 100}, {"1.1", 110}, {"1.10", 110}, {"1.1000", 110}, {"0.01", 1},
		{"73.00", 7300}, {"0002.00", 200}, {"92233720368547758.07", math.MaxInt64},
	} {
		t.Run(test.raw, func(t *testing.T) {
			actual, err := ParseFiatAmount(test.raw)
			require.NoError(t, err)
			assert.Equal(t, test.cents, actual)
			// The formatted value must parse back to the same cents.
			roundtrip, err := ParseFiatAmount(FormatFiatAmount(actual))
			require.NoError(t, err)
			assert.Equal(t, actual, roundtrip)
		})
	}
	for _, raw := range []string{"", "0", "0.00", "-1", "+1", " 1", "1 ", ".1", "1.", "1e2", "NaN", "Inf", "1.001", "1.1001", "92233720368547758.08", "92233720368547759", "1.1.1"} {
		_, err := ParseFiatAmount(raw)
		assert.ErrorIs(t, err, ErrInvalidParameters, raw)
	}
}

func TestParseTokenAmountBounds(t *testing.T) {
	for _, raw := range []string{"14.2857", "1", "0.000001", "100", "0.5"} {
		amount, err := ParseTokenAmount(raw)
		require.NoError(t, err, raw)
		assert.True(t, amount.IsPositive())
	}
	for _, raw := range []string{"", "0", "0.0", "-1", "+1", " 1", "1 ", ".1", "1.", "1e2", "NaN", "abc", "1.0000001", "1.1.1"} {
		_, err := ParseTokenAmount(raw)
		assert.ErrorIs(t, err, ErrInvalidParameters, raw)
	}
	// Six fractional digits is the documented ceiling and stays accepted.
	_, err := ParseTokenAmount("1.000001")
	require.NoError(t, err)
}

func TestNormalizeGatewayURL(t *testing.T) {
	for _, raw := range []string{
		"https://pay.example.com",
		"https://pay.example.com/",
		"https://pay.example.com/payments/gmpay/v1/order/create-transaction",
		"https://pay.example.com/payments/gmpay/v1/config",
		"https://PAY.example.com:443",
	} {
		actual, err := NormalizeGatewayURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "https://pay.example.com", actual)
	}
	// A path prefix is legitimate (the gateway may be mounted under one); only
	// the known full endpoint suffixes are stripped.
	for _, raw := range []string{"https://pay.example.com/pay/", "https://pay.example.com/pay"} {
		actual, err := NormalizeGatewayURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "https://pay.example.com/pay", actual)
	}
	for _, raw := range []string{
		"http://pay.example.com", "https://user:pass@pay.example.com",
		"https://pay.example.com?key=x", "https://pay.example.com?", "https://pay.example.com#f",
		"https:///pay", "https://pay.example.com:0", "https://pay.example.com:65536", "https://pay.example.com:",
	} {
		_, err := NormalizeGatewayURL(raw)
		assert.ErrorIs(t, err, ErrInvalidConfig, raw)
	}
}

func TestValidatePaymentURL(t *testing.T) {
	actual, err := ValidatePaymentURL("https://pay.example.com/pay/checkout-counter/abc")
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/pay/checkout-counter/abc", actual)
	actual, err = ValidatePaymentURL("https://pay.example.com:8443/cashier/t1?language=zh")
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com:8443/cashier/t1?language=zh", actual)
	actual, err = ValidatePaymentURL("https://[2001:db8::1]:8443/cashier/t1")
	require.NoError(t, err)
	assert.Equal(t, "https://[2001:db8::1]:8443/cashier/t1", actual)
	for _, raw := range []string{
		"", "http://pay.example.com/x", "https://", "https://user:pass@pay.example.com/x",
		"https://pay.example.com/x#f", "https://pay.example.com/x#", "/pay/checkout-counter/abc",
		"https://pay.example.com:0/x", "https://pay.example.com:65536/x", "https://pay.example.com:/x",
		"https://::1/x", "https://[not-an-ip]/x",
	} {
		_, err := ValidatePaymentURL(raw)
		assert.ErrorIs(t, err, ErrInvalidParameters, raw)
	}
}

func TestBuildCreateOrderSignsExactAmountLiteral(t *testing.T) {
	config := Config{GatewayURL: "https://pay.example.com/payments/gmpay/v1/config", MerchantID: "1000", Key: "k"}
	endpoint, values, err := BuildCreateOrder(config, CreateOrder{
		OrderNo: "GP0123456789012345678901234567", Currency: "usd", Token: "usdt", Network: "tron",
		Amount: "73.00", NotifyURL: "https://shop.example/api/user/gmpay/notify",
		ReturnURL: "https://shop.example/wallet", Name: "TUC10",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/payments/gmpay/v1/order/create-transaction", endpoint)
	assert.Equal(t, "73.00", values.Get("amount"), "the literal amount must survive form encoding")
	assert.Equal(t, "usdt", values.Get("token"))
	assert.Equal(t, "tron", values.Get("network"))
	signature, err := Sign(map[string]string{
		"pid": "1000", "order_id": "GP0123456789012345678901234567", "currency": "usd",
		"token": "usdt", "network": "tron", "amount": "73.00",
		"notify_url":   "https://shop.example/api/user/gmpay/notify",
		"redirect_url": "https://shop.example/wallet", "name": "TUC10",
	}, "k")
	require.NoError(t, err)
	assert.Equal(t, signature, values.Get("signature"))
}

func TestBuildCreateOrderRejectsInvalidInput(t *testing.T) {
	valid := CreateOrder{
		OrderNo: "GP0123456789012345678901234567", Currency: "usd", Token: "usdt", Network: "tron",
		Amount: "1.00", NotifyURL: "https://shop.example/notify", Name: "n",
	}
	config := Config{GatewayURL: "https://pay.example.com", MerchantID: "1000", Key: "k"}
	for _, test := range []struct {
		name   string
		config Config
		order  CreateOrder
		want   error
	}{
		{"missing key", Config{GatewayURL: config.GatewayURL, MerchantID: "1000"}, valid, ErrInvalidConfig},
		{"missing merchant", Config{GatewayURL: config.GatewayURL, Key: "k"}, valid, ErrInvalidConfig},
		{"invalid gateway", Config{GatewayURL: "http://pay.example.com", MerchantID: "1000", Key: "k"}, valid, ErrInvalidConfig},
		{"empty order", config, func() CreateOrder { o := valid; o.OrderNo = ""; return o }(), ErrInvalidConfig},
		{"long order", config, func() CreateOrder { o := valid; o.OrderNo = strings.Repeat("x", 33); return o }(), ErrInvalidConfig},
		{"empty amount", config, func() CreateOrder { o := valid; o.Amount = ""; return o }(), ErrInvalidParameters},
		{"zero amount", config, func() CreateOrder { o := valid; o.Amount = "0.00"; return o }(), ErrInvalidParameters},
		{"gateway minimum", config, func() CreateOrder { o := valid; o.Amount = "0.01"; return o }(), ErrInvalidParameters},
		{"fractional cent", config, func() CreateOrder { o := valid; o.Amount = "1.001"; return o }(), ErrInvalidParameters},
		{"invalid amount", config, func() CreateOrder { o := valid; o.Amount = "NaN"; return o }(), ErrInvalidParameters},
		{"float precision loss", config, func() CreateOrder { o := valid; o.Amount = "9007199254740991.01"; return o }(), ErrInvalidParameters},
		{"empty token", config, func() CreateOrder { o := valid; o.Token = ""; return o }(), ErrInvalidParameters},
		{"empty network", config, func() CreateOrder { o := valid; o.Network = ""; return o }(), ErrInvalidParameters},
		{"empty name", config, func() CreateOrder { o := valid; o.Name = ""; return o }(), ErrInvalidParameters},
		{"missing notify", config, func() CreateOrder { o := valid; o.NotifyURL = ""; return o }(), ErrInvalidParameters},
		{"bad notify", config, func() CreateOrder { o := valid; o.NotifyURL = "not-a-url"; return o }(), ErrInvalidParameters},
		{"notify with userinfo", config, func() CreateOrder { o := valid; o.NotifyURL = "https://u:p@shop.example/notify"; return o }(), ErrInvalidParameters},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := BuildCreateOrder(test.config, test.order)
			assert.ErrorIs(t, err, test.want)
		})
	}
	// A 32-character order number is the documented maximum and stays accepted.
	order := valid
	order.OrderNo = strings.Repeat("x", 32)
	order.Amount = "0.02"
	_, _, err := BuildCreateOrder(config, order)
	require.NoError(t, err)
}

// The gateway emits amounts as JSON numbers, so decoding must not route them
// through a float64 or a plain string field.
func TestParseCreatedOrderAcceptsNumericAndStringAmounts(t *testing.T) {
	numeric := `{"status_code":200,"message":"success","data":{"trade_id":"t1","order_id":"o1","amount":100,"currency":"USD","actual_amount":14.2857,"receive_address":"TAddr","token":"USDT","status":1,"expiration_time":1779530812,"payment_url":"https://pay.example.com/pay/checkout-counter/t1"}}`
	created, err := ParseCreatedOrder([]byte(numeric))
	require.NoError(t, err)
	assert.Equal(t, "100", created.Amount, "the numeric literal must survive decoding")
	assert.Equal(t, "14.2857", created.ActualAmount)
	assert.Equal(t, "t1", created.TradeNo)
	assert.Equal(t, StatusWaitPay, created.Status)

	quoted := `{"status_code":200,"data":{"trade_id":"t2","order_id":"o2","amount":"73.00","actual_amount":"10.5","receive_address":"A","token":"USDT","status":1,"expiration_time":1,"payment_url":"https://pay.example.com/pay/checkout-counter/t2"}}`
	created, err = ParseCreatedOrder([]byte(quoted))
	require.NoError(t, err)
	assert.Equal(t, "73.00", created.Amount, "a quoted literal keeps trailing zeros")
	assert.Equal(t, "10.5", created.ActualAmount)
}

func TestParseCreatedOrderRejectsGatewayErrors(t *testing.T) {
	for _, code := range []int{401, 10003, 10004, 10005, 10006, 10009, 10014, 10016, 10041} {
		rejected := `{"status_code":` + strconv.Itoa(code) + `,"data":null}`
		_, err := ParseCreatedOrder([]byte(rejected))
		assert.ErrorIs(t, err, ErrGatewayRejected, code)
	}

	for _, body := range []string{
		``,
		`not json`,
		`{"status_code":200,"data":null}`,
		`{"status_code":200,"data":{"trade_id":"","order_id":"o","amount":1,"status":1,"payment_url":"https://p.example/x"}}`,
		`{"status_code":200,"data":{"trade_id":"t","order_id":"o","amount":1,"status":9,"payment_url":"https://p.example/x"}}`,
		`{"status_code":200,"data":{"trade_id":"t","order_id":"o","amount":1,"status":1,"payment_url":"http://p.example/x"}}`,
		`{"status_code":200,"data":{"trade_id":"t","order_id":"o","amount":1,"status":1,"payment_url":""}}`,
	} {
		_, err := ParseCreatedOrder([]byte(body))
		assert.Error(t, err, body)
	}
}

func TestParseCreatedOrderKeepsIncompleteResponsesUnknown(t *testing.T) {
	// A missing status or payload cannot establish that the gateway rejected
	// this non-idempotent creation request. Callers must reconcile it.
	for _, body := range []string{
		`{}`, `null`, `{"data":{}}`, `{"status_code":0}`, `{"status_code":200}`, `{"status_code":200,"data":null}`,
		`{"status_code":400,"message":"system error"}`,
		`{"status_code":10002,"message":"order already exists"}`,
		`{"status_code":-1}`, `{"status_code":10099}`, `{"status_code":500}`,
	} {
		_, err := ParseCreatedOrder([]byte(body))
		assert.ErrorIs(t, err, ErrInvalidParameters, body)
		assert.NotErrorIs(t, err, ErrGatewayRejected, body)
	}
}

func TestGatewayResponsesRejectDuplicateFields(t *testing.T) {
	for _, body := range []string{
		`{"status_code":200,"status_code":10004}`,
		`{"status_code":200,"STATUS_CODE":10004}`,
		`{"status_code":200,"status_\u0063ode":10004}`,
		`{"status_code":200,"data":{"trade_id":"wrong","trade_id":"t","order_id":"o","amount":1,"status":1,"payment_url":"https://pay.example/x"}}`,
	} {
		_, err := ParseCreatedOrder([]byte(body))
		assert.ErrorIs(t, err, ErrInvalidParameters, body)
		assert.NotErrorIs(t, err, ErrGatewayRejected, body)
	}
	_, err := ParsePublicConfig([]byte(`{"status_code":200,"data":{"supported_assets":[{"network":"tron","network":"ethereum","tokens":["USDT"]}]}}`))
	assert.ErrorIs(t, err, ErrInvalidParameters)
}

func TestParseCreatedOrderRejectsNonNumericAmounts(t *testing.T) {
	for _, value := range []string{`true`, `{}`, `[]`} {
		body := `{"status_code":200,"data":{"trade_id":"t","order_id":"o","amount":` + value + `,"actual_amount":1,"status":1,"payment_url":"https://pay.example/x"}}`
		_, err := ParseCreatedOrder([]byte(body))
		assert.ErrorIs(t, err, ErrInvalidParameters, value)
	}
}

func notificationParams() map[string]string {
	return map[string]string{
		"pid": "1000", "trade_id": "t1", "order_id": "o1", "amount": "73.00",
		"actual_amount": "10", "receive_address": "TAddr", "token": "USDT",
		"block_transaction_id": "0xabc", "status": "2",
	}
}

func postNotification(t *testing.T, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/user/gmpay/notify", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func signedNotificationBody(t *testing.T, params map[string]string, key string) string {
	t.Helper()
	signature, err := Sign(params, key)
	require.NoError(t, err)
	params["signature"] = signature
	body, err := common.Marshal(params)
	require.NoError(t, err)
	return string(body)
}

func TestNotificationRoundTrip(t *testing.T) {
	params := notificationParams()
	body := signedNotificationBody(t, params, "secret")
	parsed, err := ParseNotification(postNotification(t, body))
	require.NoError(t, err)
	notification, err := VerifyNotification(parsed, Config{MerchantID: "1000", Key: "secret"})
	require.NoError(t, err)
	assert.Equal(t, &Notification{
		PID: "1000", TradeNo: "t1", OrderNo: "o1", Token: "USDT",
		ReceiveAddress: "TAddr", BlockTransactionID: "0xabc", Amount: "73.00", ActualAmount: "10", Status: StatusPaySuccess,
	}, notification)

	// A tampered field must fail the signature even when well formed.
	parsed["amount"] = "0.01"
	_, err = VerifyNotification(parsed, Config{MerchantID: "1000", Key: "secret"})
	assert.ErrorIs(t, err, ErrInvalidSignature)
}

func TestNotificationVerifiesGatewayNumericJSON(t *testing.T) {
	// This fixed digest is HMAC-SHA256(secret, epusdt's canonical pairs),
	// calculated independently of Sign. The gateway signs JSON numbers through
	// float64: both trailing zeros and exponent notation must normalize.
	body := `{"pid":"1000","trade_id":"t1","order_id":"o1","amount":73.0000,"actual_amount":1e1,"receive_address":"TAddr","token":"USDT","block_transaction_id":"0xabc","status":2,"signature":"6564efa666b71d0bac09989a54e2d96a17cb6b401e4289f267c861184dd9d77f"}`
	params, err := ParseNotification(postNotification(t, body))
	require.NoError(t, err)
	notification, err := VerifyNotification(params, Config{MerchantID: "1000", Key: "secret"})
	require.NoError(t, err)
	assert.Equal(t, "73", notification.Amount)
	assert.Equal(t, "10", notification.ActualAmount)
	assert.Equal(t, "TAddr", notification.ReceiveAddress)
	assert.Equal(t, StatusPaySuccess, notification.Status)
}

func TestNotificationRejectsDuplicateSignedFields(t *testing.T) {
	valid := signedNotificationBody(t, notificationParams(), "secret")
	for _, prefix := range []string{`"pid":"other",`, `"PID":"other",`, `"pi\u0064":"other",`, `"signature":"wrong",`} {
		body := "{" + prefix + strings.TrimPrefix(valid, "{")
		_, err := ParseNotification(postNotification(t, body))
		assert.ErrorIs(t, err, ErrInvalidParameters, prefix)
	}
}

func TestNotificationUsesJSONMediaType(t *testing.T) {
	body := signedNotificationBody(t, notificationParams(), "secret")
	request := postNotification(t, body)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	_, err := ParseNotification(request)
	require.NoError(t, err)
	for _, mediaType := range []string{"application/json-extra", "application/json; invalid"} {
		request = postNotification(t, body)
		request.Header.Set("Content-Type", mediaType)
		_, err = ParseNotification(request)
		assert.ErrorIs(t, err, ErrInvalidParameters, mediaType)
	}
}

func TestNotificationRejectsInvalidInput(t *testing.T) {
	valid := signedNotificationBody(t, notificationParams(), "secret")
	oversized := strings.Repeat("x", MaxNotificationBytes+1)
	for _, test := range []struct {
		name    string
		request *http.Request
	}{
		{"empty body", postNotification(t, "")},
		{"not json", postNotification(t, "{")},
		{"json array", postNotification(t, "[]")},
		{"null", postNotification(t, "null")},
		{"oversize", postNotification(t, oversized)},
		{"wrong content type", func() *http.Request {
			request := httptest.NewRequest(http.MethodPost, "/n", strings.NewReader(valid))
			request.Header.Set("Content-Type", "text/plain")
			return request
		}()},
		{"nested object", postNotification(t, `{"pid":"1000","nested":{"a":1}}`)},
		{"boolean value", postNotification(t, `{"pid":true}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseNotification(test.request)
			assert.ErrorIs(t, err, ErrInvalidParameters)
		})
	}
	tooMany := map[string]string{}
	for i := 0; i < 40; i++ {
		tooMany[string(rune('a'+i%26))+strings.Repeat("z", i/26)] = "1"
	}
	body, err := common.Marshal(tooMany)
	require.NoError(t, err)
	_, err = ParseNotification(postNotification(t, string(body)))
	assert.ErrorIs(t, err, ErrInvalidParameters)
}

func TestVerifyNotificationRejectsInvalidClaims(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]string)
		resign bool
		want   error
	}{
		{"signature", func(p map[string]string) { p["signature"] = strings.Repeat("0", 64) }, false, ErrInvalidSignature},
		{"short signature", func(p map[string]string) { p["signature"] = "00" }, false, ErrInvalidSignature},
		{"missing signature", func(p map[string]string) { delete(p, "signature") }, false, ErrInvalidSignature},
		{"merchant", func(p map[string]string) { p["pid"] = "other" }, true, ErrInvalidParameters},
		{"missing token", func(p map[string]string) { p["token"] = "" }, true, ErrInvalidParameters},
		{"missing fiat amount", func(p map[string]string) { delete(p, "amount") }, true, ErrInvalidParameters},
		{"invalid fiat amount", func(p map[string]string) { p["amount"] = "0" }, true, ErrInvalidParameters},
		{"invalid token amount", func(p map[string]string) { p["actual_amount"] = "-1" }, true, ErrInvalidParameters},
		{"missing address", func(p map[string]string) { delete(p, "receive_address") }, true, ErrInvalidParameters},
		{"missing trade", func(p map[string]string) { p["trade_id"] = "" }, true, ErrInvalidParameters},
		{"missing tx", func(p map[string]string) { p["block_transaction_id"] = "" }, true, ErrInvalidParameters},
		{"long tx", func(p map[string]string) { p["block_transaction_id"] = strings.Repeat("x", 129) }, true, ErrInvalidParameters},
		{"bad status", func(p map[string]string) { p["status"] = "9" }, true, ErrInvalidParameters},
		{"non numeric status", func(p map[string]string) { p["status"] = "paid" }, true, ErrInvalidParameters},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := notificationParams()
			signature, err := Sign(params, "secret")
			require.NoError(t, err)
			params["signature"] = signature
			test.mutate(params)
			if test.resign {
				params["signature"], err = Sign(params, "secret")
				require.NoError(t, err)
			}
			_, err = VerifyNotification(params, Config{MerchantID: "1000", Key: "secret"})
			assert.ErrorIs(t, err, test.want)
		})
	}
}

func TestVerifyNotificationAllowsUnpaidStatus(t *testing.T) {
	// A waiting or expired status is still a well-formed notification; the
	// settlement layer decides what to do with it.
	for _, status := range []int{StatusWaitPay, StatusExpired} {
		params := notificationParams()
		params["status"] = strconv.Itoa(status)
		signature, err := Sign(params, "secret")
		require.NoError(t, err)
		params["signature"] = signature
		notification, err := VerifyNotification(params, Config{MerchantID: "1000", Key: "secret"})
		require.NoError(t, err, status)
		assert.Equal(t, status, notification.Status)
	}
}

func TestVerifyNotificationRequiresConfig(t *testing.T) {
	params := notificationParams()
	params["signature"] = strings.Repeat("a", 64)
	_, err := VerifyNotification(params, Config{MerchantID: "1000"})
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = VerifyNotification(params, Config{Key: "secret"})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}
