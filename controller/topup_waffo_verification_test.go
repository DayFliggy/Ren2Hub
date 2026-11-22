package controller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	wnet "github.com/waffo-com/waffo-go/net"
	"github.com/waffo-com/waffo-go/utils"
)

type localWaffoInquiryTransport struct {
	client *http.Client
	url    string
}

func (tr localWaffoInquiryTransport) Send(ctx context.Context, request *wnet.HttpRequest) (*wnet.HttpResponse, error) {
	req, err := http.NewRequestWithContext(ctx, request.Method, tr.url, strings.NewReader(string(request.Body)))
	if err != nil {
		return nil, err
	}
	for key, value := range request.Headers {
		req.Header.Set(key, value)
	}
	res, err := tr.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return wnet.NewHttpResponse(res.StatusCode, map[string]string{"X-SIGNATURE": res.Header.Get("X-SIGNATURE")}, body), nil
}

func TestWaffoMerchantInquiryUsesAuthenticatedOfficialSDK(t *testing.T) {
	pair, err := utils.GenerateKeyPair()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, code, message string
		edit                func(map[string]any)
		status              int
		accepted            bool
	}{
		{name: "owned_order", code: "0", status: 200, accepted: true},
		{name: "equivalent_decimal", code: "0", edit: func(d map[string]any) { d["orderAmount"] = "1.00" }, status: 200, accepted: true},
		{name: "foreign_merchant", code: "0", edit: func(d map[string]any) { d["merchantInfo"] = map[string]any{"merchantId": "foreign"} }, status: 200},
		{name: "wrong_request", code: "0", edit: func(d map[string]any) { d["paymentRequestId"] = "foreign" }, status: 200},
		{name: "wrong_order", code: "0", edit: func(d map[string]any) { d["merchantOrderId"] = "foreign" }, status: 200},
		{name: "pending", code: "0", edit: func(d map[string]any) { d["orderStatus"] = "PAY_IN_PROGRESS" }, status: 200},
		{name: "amount_mismatch", code: "0", edit: func(d map[string]any) { d["orderAmount"] = "0.99" }, status: 200},
		{name: "currency_mismatch", code: "0", edit: func(d map[string]any) { d["orderCurrency"] = "EUR" }, status: 200},
		{name: "not_found", code: "A0009", status: 200},
		{name: "unknown_status", code: "E0001", status: 503},
		{name: "channel_failure", code: "C0001", status: 503},
		{name: "sdk_network", code: "A0009", message: "[S0001] unavailable", status: 503},
		{name: "sdk_signature", code: "A0009", message: "[S0004] unavailable", status: 503},
		{name: "sdk_parse", code: "A0009", message: "[S0006] unavailable", status: 503},
		{name: "unknown_code", code: "FUTURE_CODE", status: 503},
		{name: "invalid_response_signature", code: "0", status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := useGmpayControllerDB(t)
			require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
			old := setting.WaffoMerchantId
			setting.WaffoMerchantId = "merchant-fixture"
			t.Cleanup(func() { setting.WaffoMerchantId = old })
			user := model.User{Username: "waffo-inquiry", AffCode: "waffo-inquiry"}
			require.NoError(t, db.Create(&user).Error)
			topup := model.TopUp{UserId: user.Id, TradeNo: "inquiry-order", PaymentProvider: model.PaymentProviderWaffo, PaymentMethod: model.PaymentMethodWaffo, Status: common.TopUpStatusPending, ExpectedQuota: 100, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
			require.NoError(t, topup.Insert())
			calls := 0
			stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "inquiry-api-key", r.Header.Get("X-API-KEY"))
				require.True(t, utils.Verify(string(body), r.Header.Get("X-SIGNATURE"), pair.PublicKey))
				var query map[string]any
				require.NoError(t, json.Unmarshal(body, &query))
				require.Equal(t, "inquiry-payment", query["paymentRequestId"])
				data := map[string]any{"paymentRequestId": "inquiry-payment", "merchantOrderId": "inquiry-order", "orderStatus": "PAY_SUCCESS", "orderAmount": "1", "orderCurrency": "USD"}
				if tc.edit != nil {
					tc.edit(data)
				}
				raw, err := json.Marshal(map[string]any{"code": tc.code, "msg": tc.message, "data": data})
				require.NoError(t, err)
				signature, err := utils.Sign(string(raw), pair.PrivateKey)
				require.NoError(t, err)
				if tc.name == "invalid_response_signature" {
					signature = "invalid"
				}
				w.Header().Set("X-SIGNATURE", signature)
				w.Write(raw)
			}))
			defer stub.Close()
			cfg, err := config.NewConfigBuilder().APIKey("inquiry-api-key").PrivateKey(pair.PrivateKey).WaffoPublicKey(pair.PublicKey).MerchantID(setting.WaffoMerchantId).CustomTransport(localWaffoInquiryTransport{stub.Client(), stub.URL}).Build()
			require.NoError(t, err)
			sdk := waffo.New(cfg)
			var payload webhookPayloadWithSubInfo
			require.NoError(t, json.Unmarshal([]byte(`{"eventType":"PAYMENT_NOTIFICATION","result":{"paymentRequestId":"inquiry-payment","merchantOrderId":"inquiry-order","orderStatus":"PAY_SUCCESS","orderAmount":"1","orderCurrency":"USD"}}`), &payload))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", nil)
			handleVerifiedWaffoPayment(c, sdk, &payload)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.NoError(t, db.First(&user, user.Id).Error)
			saved := model.GetTopUpByTradeNo(topup.TradeNo)
			if tc.accepted {
				require.Equal(t, 100, user.Quota)
				require.Equal(t, common.TopUpStatusSuccess, saved.Status)
			} else {
				require.Zero(t, user.Quota)
				require.Equal(t, common.TopUpStatusPending, saved.Status)
			}
			var audits int64
			require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "payment.callback_rejected").Count(&audits).Error)
			if !tc.accepted && tc.status == 200 {
				require.EqualValues(t, 1, audits)
				require.Contains(t, recorder.Body.String(), "success")
			} else if tc.status != 200 {
				require.Zero(t, audits)
				require.Contains(t, recorder.Body.String(), "failed")
			}
		})
	}
}
