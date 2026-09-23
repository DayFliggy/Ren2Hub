package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	}{
		{name: "valid response", wantStatus: model.GmpayStatusReady},
		{name: "uppercase token", field: "token", value: "USDT", wantStatus: model.GmpayStatusReady},
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
		{name: "redirect is not followed", httpStatus: 307},
		{name: "explicit parameter rejection", httpStatus: 400, code: 10004, wantStatus: model.GmpayStatusFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
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
			assert.Equal(t, 1, calls, "non-idempotent creation must never be retried or redirected")
			if test.wantStatus == model.GmpayStatusReady {
				require.NoError(t, err)
				require.NotNil(t, created)
				require.NotNil(t, snapshot.GatewayTradeNo)
				assert.Equal(t, "gateway-order", *snapshot.GatewayTradeNo)
			} else {
				require.Error(t, err)
				assert.Nil(t, created)
				assert.Nil(t, snapshot.GatewayTradeNo, "unproven gateway evidence must not bind an order")
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
