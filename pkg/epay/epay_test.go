package epay

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func notificationValues() url.Values {
	params := map[string]string{
		"pid": "1001", "trade_no": "gateway-123", "out_trade_no": "order-456",
		"type": "alipay", "money": "10.20", "trade_status": StatusTradeSuccess,
		"sign_type": "MD5", "name": "充值", "extension": "a & b = c",
	}
	params["sign"] = Sign(params, "test-secret")
	values := make(url.Values)
	for name, value := range params {
		values.Set(name, value)
	}
	return values
}

func TestSignProtocolCompatibility(t *testing.T) {
	params := map[string]string{"b": "2", "a": "1", "empty": "", "sign": "old-sign", "sign_type": "MD5"}
	assert.Equal(t, "8d9f51949e440aa629fd1a035708473a", Sign(params, "secret"))
	assert.Equal(t, "old-sign", params["sign"], "signing must not mutate input")
	assert.Len(t, params, 5)
}

func TestNotificationRoundTripAndSignedExtension(t *testing.T) {
	config := Config{MerchantID: "1001", Key: "test-secret"}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			values := notificationValues()
			var request *http.Request
			if method == http.MethodGet {
				request = httptest.NewRequest(method, "/notify?"+values.Encode(), nil)
			} else {
				request = httptest.NewRequest(method, "/notify", strings.NewReader(values.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			}
			params, err := ParseNotification(request)
			require.NoError(t, err)
			payment, err := VerifyNotification(params, config)
			require.NoError(t, err)
			assert.Equal(t, &Payment{OrderNo: "order-456", TradeNo: "gateway-123", MerchantID: "1001", Method: "alipay", Status: StatusTradeSuccess, AmountMinor: 1020}, payment)
			params["extension"] = "modified"
			_, err = VerifyNotification(params, config)
			assert.ErrorIs(t, err, ErrInvalidSignature)
		})
	}
}

func TestNotificationRejectsInvalidInputBeforeSignature(t *testing.T) {
	valid := notificationValues().Encode()
	tooMany := notificationValues()
	for i := 0; i < 33; i++ {
		tooMany.Set(strings.Repeat("x", i+1), "1")
	}
	longKey := notificationValues()
	longKey.Set(strings.Repeat("k", 65), "1")
	longValue := notificationValues()
	longValue.Set("extra", strings.Repeat("v", 4097))
	missing := notificationValues()
	missing.Del("pid")
	for _, test := range []struct{ name, raw string }{
		{"duplicate", valid + "&pid=1001"},
		{"encoded duplicate", valid + "&%70id=1001"},
		{"invalid escaping", valid + "&other=%zz"},
		{"invalid separator", valid + ";other=x"},
		{"too many fields", tooMany.Encode()},
		{"long key", longKey.Encode()},
		{"long value", longValue.Encode()},
		{"missing merchant", missing.Encode()},
		{"oversize", valid + "&extra=" + strings.Repeat("x", MaxNotificationBytes)},
		{"invalid utf8", valid + "&other=%ff"},
		{"control character", valid + "&other=%0d%0a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				var request *http.Request
				if method == http.MethodGet {
					request = httptest.NewRequest(method, "/notify?"+test.raw, nil)
				} else {
					request = httptest.NewRequest(method, "/notify", strings.NewReader(test.raw))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.ContentLength = -1 // Exercise streaming limits as well as declared length.
				}
				_, err := ParseNotification(request)
				assert.ErrorIs(t, err, ErrInvalidParameters)
			}
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/notify?pid=1001", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := ParseNotification(request)
	assert.ErrorIs(t, err, ErrInvalidParameters, "mixed query/body duplicate")
}

func TestNotificationAcceptsExactLimits(t *testing.T) {
	values := notificationValues()
	for len(values) < 32 {
		values.Set(strings.Repeat("k", len(values)+1), "v")
	}
	_, err := ParseNotification(httptest.NewRequest(http.MethodGet, "/notify?"+values.Encode(), nil))
	require.NoError(t, err, "the field limit is inclusive")
	values = notificationValues()
	for _, key := range []string{"large1", "large2", "large3"} {
		values.Set(key, strings.Repeat("v", 4096))
	}
	values.Set("padding", "")
	values.Set("padding", strings.Repeat("v", MaxNotificationBytes-len(values.Encode())))
	raw := values.Encode()
	require.Len(t, raw, MaxNotificationBytes)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, "/notify?"+raw, nil)
		if method == http.MethodPost {
			request = httptest.NewRequest(method, "/notify", strings.NewReader(raw))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		_, err := ParseNotification(request)
		assert.NoError(t, err, "the raw byte and individual value limits are inclusive")
	}
}

func TestVerifyNotificationRejectsInvalidClaims(t *testing.T) {
	for _, test := range []struct {
		name, field, value string
		resign             bool
		want               error
	}{
		{"signature", "sign", strings.Repeat("0", 32), false, ErrInvalidSignature},
		{"short signature", "sign", "00", false, ErrInvalidSignature},
		{"merchant", "pid", "other", true, ErrInvalidParameters},
		{"amount", "money", "0.001", true, ErrInvalidMoney},
		{"algorithm", "sign_type", "SHA256", true, ErrInvalidParameters},
		{"long transaction", "trade_no", strings.Repeat("x", 129), true, ErrInvalidParameters},
		{"long method", "type", strings.Repeat("x", 129), true, ErrInvalidParameters},
		{"unknown status", "trade_status", "PAID", true, ErrInvalidParameters},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := notificationValues()
			params := make(map[string]string)
			for key := range values {
				params[key] = values.Get(key)
			}
			params[test.field] = test.value
			if test.resign {
				params["sign"] = Sign(params, "test-secret")
			}
			_, err := VerifyNotification(params, Config{MerchantID: "1001", Key: "test-secret"})
			assert.ErrorIs(t, err, test.want)
		})
	}
}

func TestNotificationPaymentMethodFitsStoredOrder(t *testing.T) {
	for _, size := range []int{71, 72} {
		t.Run(strings.Repeat("m", size), func(t *testing.T) {
			params := make(map[string]string)
			for name, values := range notificationValues() {
				params[name] = values[0]
			}
			params["type"] = strings.Repeat("m", size)
			params["sign"] = Sign(params, "test-secret")
			values := make(url.Values)
			for name, value := range params {
				values.Set(name, value)
			}
			_, parseErr := ParseNotification(httptest.NewRequest(http.MethodGet, "/notify?"+values.Encode(), nil))
			payment, verifyErr := VerifyNotification(params, Config{MerchantID: "1001", Key: "test-secret"})
			if size == 72 {
				assert.ErrorIs(t, parseErr, ErrInvalidParameters)
				assert.ErrorIs(t, verifyErr, ErrInvalidParameters)
				assert.Nil(t, payment)
				return
			}
			require.NoError(t, parseErr)
			require.NoError(t, verifyErr)
			assert.Equal(t, params["type"], payment.Method)
		})
	}
}

func TestMoneyPrecisionAndOverflow(t *testing.T) {
	for _, test := range []struct {
		raw   string
		cents int64
	}{
		{"1", 100}, {"1.1", 110}, {"1.10", 110}, {"1.1000", 110}, {"0.01", 1},
		{"92233720368547758.07", math.MaxInt64}, {"0002.00", 200},
	} {
		t.Run(test.raw, func(t *testing.T) {
			actual, err := ParseMoney(test.raw)
			require.NoError(t, err)
			assert.Equal(t, test.cents, actual)
			roundtrip, err := ParseMoney(FormatMoney(actual))
			require.NoError(t, err)
			assert.Equal(t, actual, roundtrip)
		})
	}
	for _, raw := range []string{"", "0", "0.00", "-1", "+1", " 1", "1 ", ".1", "1.", "1e2", "NaN", "Inf", "1.001", "1.1001", "92233720368547758.08", "92233720368547759", "9223372036854775808", "1.1.1"} {
		_, err := ParseMoney(raw)
		assert.ErrorIs(t, err, ErrInvalidMoney, raw)
	}
	assert.Equal(t, "0.00", FormatMoney(0))
	assert.Equal(t, "-92233720368547758.08", FormatMoney(math.MinInt64))
}

func TestNormalizeGatewayAndBuildPurchase(t *testing.T) {
	for _, raw := range []string{"https://EXAMPLE.com:443/pay/", "https://example.com/pay/submit.php", "https://example.com/pay/api.php", "https://example.com/pay/mapi.php"} {
		actual, err := NormalizeGatewayURL(raw)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/pay", actual)
	}
	for _, raw := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?key=x", "https://example.com?", "https://example.com#part", "https:///pay", "https://example.com:0", "https://example.com:65536", "https://example.com:"} {
		_, err := NormalizeGatewayURL(raw)
		assert.ErrorIs(t, err, ErrInvalidConfig, raw)
	}
	config := Config{GatewayURL: "https://example.com/pay/api.php", MerchantID: "1001", Key: "test-secret"}
	endpoint, params, err := BuildPurchase(config, Purchase{OrderNo: "order-1", Method: "alipay", Name: "充值", NotifyURL: "https://shop.example/notify", ReturnURL: "https://shop.example/return", AmountMinor: 123})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/pay/submit.php", endpoint)
	assert.Equal(t, "1.23", params["money"])
	assert.Equal(t, Sign(params, config.Key), params["sign"])
	assert.NotContains(t, params, "key")
}
