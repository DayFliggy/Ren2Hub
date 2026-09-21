package epay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryOrderTLSProtocol(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/gateway/api.php", r.URL.Path)
		params := r.URL.Query()
		assert.Equal(t, "order", params.Get("act"))
		assert.Equal(t, "1001", params.Get("pid"))
		assert.Equal(t, "test-secret", params.Get("key"))
		assert.Equal(t, "order-456", params.Get("out_trade_no"))
		assert.Zero(t, r.ContentLength, "V1 queries use URL parameters, not a form body")
		_, _ = io.WriteString(w, `{"code":1,"pid":1001,"out_trade_no":"order-456","trade_no":"gateway-123","type":"alipay","money":"10.20","status":1}`)
	}))
	defer server.Close()
	client := NewClient(server.Client())
	payment, err := client.QueryOrder(context.Background(), Config{GatewayURL: server.URL + "/gateway", MerchantID: "1001", Key: "test-secret"}, "order-456")
	require.NoError(t, err)
	assert.Equal(t, &Payment{OrderNo: "order-456", TradeNo: "gateway-123", MerchantID: "1001", Method: "alipay", Status: StatusTradeSuccess, AmountMinor: 1020}, payment)
	assert.EqualValues(t, 1, calls.Load())
}

func TestQueryResponseRequiresConsistentPaymentEvidence(t *testing.T) {
	valid := `"pid":"1001","out_trade_no":"order-456","trade_no":"gateway-123","type":"alipay","money":"10.20"`
	for _, test := range []struct {
		name, body string
		wantStatus string
		wantError  error
	}{
		{"paid", `{"code":1,` + valid + `,"status":1}`, StatusTradeSuccess, nil},
		{"pending", `{"code":1,` + valid + `,"status":0}`, StatusPending, nil},
		{"string pending", `{"code":"1",` + valid + `,"status":"0"}`, StatusPending, nil},
		{"standard metadata", `{"code":1,` + valid + `,"status":1,"msg":"查询订单成功！","buyer":"redacted","addtime":"2026-09-20 00:00:00","param":""}`, StatusTradeSuccess, nil},
		{"nested", `{"code":1,"data":{` + valid + `,"trade_status":"TRADE_SUCCESS"}}`, "", ErrInvalidResponse},
		{"callback status", `{"code":1,` + valid + `,"trade_status":"TRADE_SUCCESS"}`, "", ErrInvalidResponse},
		{"duplicate state representations", `{"code":1,` + valid + `,"status":1,"trade_status":"TRADE_SUCCESS"}`, "", ErrInvalidResponse},
		{"gateway rejects", `{"code":-1,"msg":"private information"}`, "", ErrGatewayRejected},
		{"missing code", `{` + valid + `,"status":1}`, "", ErrInvalidResponse},
		{"wrong merchant", `{"code":1,` + strings.Replace(valid, `"pid":"1001"`, `"pid":"9999"`, 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"wrong order", `{"code":1,` + strings.Replace(valid, "order-456", "another-order", 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"missing transaction", `{"code":1,` + strings.Replace(valid, `"trade_no":"gateway-123",`, "", 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"oversized transaction", `{"code":1,` + strings.Replace(valid, "gateway-123", strings.Repeat("x", 129), 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"oversized payment method", `{"code":1,` + strings.Replace(valid, "alipay", strings.Repeat("x", 129), 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"fractional cent", `{"code":1,` + strings.Replace(valid, "10.20", "10.201", 1) + `,"status":1}`, "", ErrInvalidResponse},
		{"missing status", `{"code":1,` + valid + `}`, "", ErrInvalidResponse},
		{"unknown status", `{"code":1,` + valid + `,"status":2}`, "", ErrInvalidResponse},
		{"status conflict", `{"code":1,` + valid + `,"status":0,"trade_status":"TRADE_SUCCESS"}`, "", ErrInvalidResponse},
		{"nested conflict", `{"code":1,` + valid + `,"status":1,"data":{"money":"1.00"}}`, "", ErrInvalidResponse},
		{"nested status conflict", `{"code":1,` + valid + `,"status":1,"data":{"trade_status":"WAIT_BUYER_PAY"}}`, "", ErrInvalidResponse},
		{"nested failure", `{"code":1,` + valid + `,"status":1,"data":{"code":-1}}`, "", ErrInvalidResponse},
		{"null", `null`, "", ErrInvalidResponse},
		{"trailing json", `{"code":1,` + valid + `,"status":1}{}`, "", ErrInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			payment, err := parseQueryResponse([]byte(test.body), "1001", "order-456")
			if test.wantError != nil {
				require.ErrorIs(t, err, test.wantError)
				assert.Nil(t, payment)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, payment.Status)
		})
	}
}

func TestQueryLimitsAndRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}, ErrQueryFailed},
		{"oversized", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat(" ", maxQueryResponseBytes+1))
		}, ErrInvalidResponse},
		{"error status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, ErrQueryFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(test.handler)
			defer server.Close()
			payment, err := NewClient(server.Client()).QueryOrder(context.Background(), Config{GatewayURL: server.URL, MerchantID: "1001", Key: "test-secret"}, "order-456")
			assert.ErrorIs(t, err, test.want)
			assert.Nil(t, payment)
		})
	}
	assert.Zero(t, redirected.Load(), "redirects must not forward credentials")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := QueryOrder(ctx, Config{GatewayURL: "https://example.invalid", MerchantID: "1001", Key: "test-secret"}, "order-456")
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, ErrQueryFailed)
}

type failingTransport struct{ err error }

func (transport failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

func TestQueryTransportErrorsAreRedacted(t *testing.T) {
	for _, test := range []struct {
		name           string
		transportError error
		safeCause      error
	}{
		{"request detail", errors.New("https://provider.example/private?key=test-secret order-456"), nil},
		{"cancelled", errors.Join(context.Canceled, errors.New("key=test-secret")), context.Canceled},
		{"timeout", errors.Join(context.DeadlineExceeded, errors.New("key=test-secret")), context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: failingTransport{err: test.transportError}})
			payment, err := client.QueryOrder(context.Background(), Config{GatewayURL: "https://provider.example/private", MerchantID: "1001", Key: "test-secret"}, "order-456")
			require.ErrorIs(t, err, ErrQueryFailed)
			assert.Nil(t, payment)
			assert.NotContains(t, err.Error(), "provider.example")
			assert.NotContains(t, err.Error(), "test-secret")
			assert.NotContains(t, err.Error(), "order-456")
			if test.safeCause != nil {
				assert.ErrorIs(t, err, test.safeCause)
			}
		})
	}
}
