package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGmpayCreationResponseMustMatchFrozenOrder(t *testing.T) {
	for _, test := range []struct {
		name       string
		httpStatus int
		code       int
		field      string
		value      any
		wantStatus string
		wantCrypto string
	}{
		{name: "valid response", wantStatus: model.GmpayStatusReady},
		{name: "uppercase token", field: "token", value: "USDT", wantStatus: model.GmpayStatusReady},
		{name: "numeric fiat exponent", field: "amount", value: json.Number("1e1"), wantStatus: model.GmpayStatusReady},
		{name: "numeric crypto exponent", field: "actual_amount", value: json.Number("1e+21"), wantStatus: model.GmpayStatusReady},
		{name: "padded fiat normalizes before persistence", field: "amount", value: "10." + strings.Repeat("0", 64), wantStatus: model.GmpayStatusReady},
		{name: "padded crypto normalizes before persistence", field: "actual_amount", value: strings.Repeat("0", 64) + "9.998765", wantStatus: model.GmpayStatusReady, wantCrypto: "9.998765"},
		{name: "crypto at storage bound", field: "actual_amount", value: strings.Repeat("9", 57) + ".123456", wantStatus: model.GmpayStatusReady, wantCrypto: strings.Repeat("9", 57) + ".123456"},
		{name: "crypto beyond storage bound", field: "actual_amount", value: strings.Repeat("9", 58) + ".123456"},
		{name: "address at storage bound", field: "receive_address", value: strings.Repeat("a", 256), wantStatus: model.GmpayStatusReady},
		{name: "address beyond storage bound", field: "receive_address", value: strings.Repeat("a", 257)},
		{name: "foreign order", field: "order_id", value: "another-order"},
		{name: "wrong amount", field: "amount", value: "9.99"},
		{name: "wrong currency", field: "currency", value: "cny"},
		{name: "wrong token", field: "token", value: "usdc"},
		{name: "placeholder", field: "status", value: 4},
		{name: "missing address", field: "receive_address", value: ""},
		{name: "invalid crypto amount", field: "actual_amount", value: "0"},
		{name: "missing expiry", field: "expiration_time", value: 0},
		{name: "system error", httpStatus: 400, code: 400},
		{name: "order may already exist", httpStatus: 400, code: 10002},
		{name: "proxy error with rejection body", httpStatus: 502, code: 10004},
		{name: "accepted is not a rejection", httpStatus: 202, code: 10004},
		{name: "created is not a rejection", httpStatus: 201, code: 10004},
		{name: "timeout is not a rejection", httpStatus: 408, code: 10004},
		{name: "rate limit is not a gateway rejection", httpStatus: 429, code: 10004},
		{name: "redirect is not followed", httpStatus: 307},
		{name: "signature rejection", httpStatus: 401, code: 401, wantStatus: model.GmpayStatusFailed},
		{name: "explicit parameter rejection", httpStatus: 400, code: 10004, wantStatus: model.GmpayStatusFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/payments/gmpay/v1/order/create-transaction", r.URL.Path)
				assert.NoError(t, r.ParseForm())
				params := make(map[string]string, len(r.PostForm))
				for key := range r.PostForm {
					params[key] = r.PostForm.Get(key)
				}
				signed, err := gmpay.Sign(params, gmpayTestSecret)
				assert.NoError(t, err)
				assert.Equal(t, signed, params["signature"])
				assert.Equal(t, "usd", params["currency"])
				assert.Equal(t, "10.00", params["amount"])
				assert.Equal(t, "tron", params["network"])
				data := map[string]any{
					"trade_id": "gateway-order", "order_id": params["order_id"],
					"amount": 10, "currency": "usd", "token": "usdt", "status": 1,
					"actual_amount": 9.998765, "receive_address": "test-address", "expiration_time": 1893456000,
					"payment_url": "https://cashier.example.test/cashier/gateway-order",
				}
				if test.field != "" {
					data[test.field] = test.value
				}
				code := test.code
				if code == 0 {
					code = 200
				}
				body, err := common.Marshal(map[string]any{"status_code": code, "data": data})
				assert.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", "/must-not-retry")
				if test.httpStatus != 0 {
					w.WriteHeader(test.httpStatus)
				}
				_, _ = w.Write(body)
			}))
			t.Cleanup(gateway.Close)
			previous := httpClient
			httpClient = gateway.Client()
			t.Cleanup(func() { httpClient = previous })
			db := gmpayServiceTestDB(t, gateway.URL)
			created, err := CreateGmpayTopUp(context.Background(), 1, GmpayQuoteRequest{
				Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron",
			})
			var order model.TopUp
			require.NoError(t, db.First(&order).Error)
			var snapshot model.GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.EqualValues(t, 1, calls.Load(), "non-idempotent creation must never be retried or redirected")
			if test.wantStatus == model.GmpayStatusReady {
				require.NoError(t, err)
				require.NotNil(t, created)
				require.NotNil(t, snapshot.GatewayTradeNo)
				assert.Equal(t, "gateway-order", *snapshot.GatewayTradeNo)
				assert.Equal(t, "10.00", snapshot.GatewayAmount)
				if test.wantCrypto != "" {
					assert.Equal(t, test.wantCrypto, snapshot.GatewayCryptoAmt)
				}
				assert.LessOrEqual(t, len(snapshot.GatewayCryptoAmt), 64)
				assert.LessOrEqual(t, len(snapshot.ReceiveAddress), 256)
			} else {
				require.Error(t, err)
				assert.Nil(t, created)
				assert.Nil(t, snapshot.GatewayTradeNo, "unproven gateway evidence must not bind an order")
				assert.Empty(t, snapshot.GatewayAmount)
				assert.Empty(t, snapshot.GatewayCryptoAmt)
				assert.Empty(t, snapshot.ReceiveAddress)
			}
			wantStatus := test.wantStatus
			if wantStatus == "" {
				wantStatus = model.GmpayStatusUnknown
			}
			assert.Equal(t, wantStatus, snapshot.Status)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Zero(t, gmpayTestUserQuota(t, db), "creation never credits the wallet")
		})
	}
}

func TestGmpayPublicProbeUsesOnlyTheCatalogEndpoint(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		valid  bool
	}{
		{"catalog", http.StatusOK, `{"status_code":200,"data":{"supported_assets":[{"network":"tron","tokens":["USDT"]}]}}`, true},
		{"missing catalog", http.StatusOK, `{"status_code":200,"data":{}}`, false},
		{"empty catalog", http.StatusOK, `{"status_code":200,"data":{"supported_assets":[]}}`, true},
		{"redirect", http.StatusTemporaryRedirect, `{}`, false},
		{"oversized response", http.StatusOK, strings.Repeat(" ", gmpay.MaxNotificationBytes+1), false},
		{"gateway failure", http.StatusServiceUnavailable, `{"status_code":200,"data":{"supported_assets":[]}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/payments/gmpay/v1/config", r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)
				assert.Empty(t, r.Header.Get("Authorization"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Empty(t, body, "probing must not send merchant credentials or create an order")
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(gateway.Close)
			previous := httpClient
			httpClient = gateway.Client()
			t.Cleanup(func() { httpClient = previous })
			assets, err := GmpayGatewayIsReachable(t.Context(), gateway.URL)
			if test.valid {
				require.NoError(t, err)
				if test.name == "catalog" {
					assert.Equal(t, []gmpay.SupportedAsset{{Network: "tron", DisplayName: "tron", Tokens: []string{"usdt"}}}, assets)
				} else {
					assert.Empty(t, assets)
				}
			} else {
				require.Error(t, err)
				assert.Nil(t, assets)
			}
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

type gmpayTestTransport func(*http.Request) (*http.Response, error)

func (transport gmpayTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestGmpayCancelledRequestPersistsUnknownCreation(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://gateway.example.test")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	previous := httpClient
	httpClient = &http.Client{Transport: gmpayTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, request.Context().Err()
	})}
	t.Cleanup(func() { httpClient = previous })
	_, err := CreateGmpayTopUp(ctx, 1, GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "usdt.tron"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
	var snapshots []model.GmpayOrder
	require.NoError(t, db.Find(&snapshots).Error)
	require.Len(t, snapshots, 1)
	assert.Equal(t, model.GmpayStatusUnknown, snapshots[0].Status)
	assert.Equal(t, "TRANSPORT", snapshots[0].LastFailure)
	assert.Zero(t, gmpayTestUserQuota(t, db))
}
