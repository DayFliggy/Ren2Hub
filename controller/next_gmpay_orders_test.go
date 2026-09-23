package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextAdminGmpayOrdersRemainSeparateFromEpayRevenue(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	user := model.User{Username: "gmpay-facets", AffCode: "gmpay-facets", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	for _, order := range []model.TopUp{
		{TradeNo: "GP-paid", PaymentProvider: model.PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.ethereum", Money: 3, Status: common.TopUpStatusSuccess, CreditedQuota: 1500000},
		{TradeNo: "GP-pending", PaymentProvider: model.PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Money: 7, Status: common.TopUpStatusPending},
		{TradeNo: "EP-paid", PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "alipay", Money: 21, Status: common.TopUpStatusSuccess, CreditedQuota: 1500000},
		{TradeNo: "other-paid", PaymentProvider: "unknown", PaymentMethod: "unknown", Money: 9, Status: common.TopUpStatusSuccess},
	} {
		order.UserId = user.Id
		order.CreateTime = time.Now().Unix()
		require.NoError(t, db.Create(&order).Error)
	}
	type orderPage struct {
		Items               []NextOrderDTO `json:"items"`
		Total               int            `json:"total"`
		MethodCounts        map[string]int `json:"method_counts"`
		FilteredEpayRevenue float64        `json:"filtered_epay_revenue"`
	}
	for _, test := range []struct {
		method  string
		count   int
		revenue float64
	}{
		{"gmpay", 2, 0}, {"epay", 1, 21}, {"other", 1, 0},
	} {
		page := decodeNextResponse[orderPage](t, performNextGet(t,
			"/api/next/admin/orders?method="+test.method, NextListAdminOrders))
		assert.Equal(t, test.count, page.Total)
		require.Len(t, page.Items, test.count)
		assert.Equal(t, test.count, page.MethodCounts[test.method])
		assert.Equal(t, test.revenue, page.FilteredEpayRevenue)
		for _, item := range page.Items {
			assert.Equal(t, test.method, item.Method)
			if test.method == "gmpay" {
				assert.Equal(t, "USD", item.Currency)
				if item.Status == "pending" {
					assert.Zero(t, item.Quota)
				} else {
					assert.Equal(t, common.Quota(1500000), item.Quota)
				}
			}
		}
	}
	stats := decodeNextResponse[struct {
		Currency     string  `json:"currency"`
		TotalRevenue float64 `json:"total_revenue"`
		TotalOrders  int     `json:"total_orders"`
	}](t, performNextGet(t, "/api/next/admin/orders/stats?range=7", NextGetAdminOrderStats))
	assert.Equal(t, "CNY", stats.Currency)
	assert.Equal(t, 1, stats.TotalOrders)
	assert.Equal(t, float64(21), stats.TotalRevenue)
}

func TestNextAdminGmpayOrderDetailSeparatesPaymentEvidenceAndHidesCredentials(t *testing.T) {
	db := setupGmpayControllerTestDB(t, "https://payments.example.test")
	order := model.TopUp{UserId: 1, TradeNo: "GP-detail-evidence", PaymentProvider: model.PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Money: 10}
	snapshot := model.GmpayOrder{ExpectedQuota: 5_000_000, AmountMinor: 1000, Currency: "USD", Token: "usdt", Network: "tron"}
	merchant := model.GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "private-merchant-id", Key: "private-detail-test-key"}
	require.NoError(t, model.CreateGmpayOrder(t.Context(), &order, &snapshot, &merchant))
	require.NoError(t, model.MarkGmpayOrderResult(t.Context(), order.Id, model.GmpayOrderResult{
		Status: model.GmpayStatusReady, GatewayTradeNo: "detail-gateway-trade", GatewayAmount: "10.00",
		GatewayCryptoAmount: "9.99", ReceiveAddress: "initial-address", ExpirationTime: 1893456000,
	}))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/orders/:order_no", NextGetAdminOrder)
	for _, settled := range []bool{false, true} {
		if settled {
			_, err := model.RechargeGmpay(model.GmpayPayment{
				OrderNo: order.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID,
				GatewayTradeNo: "detail-gateway-trade", AmountMinor: 1000, Token: "usdt",
				ActualAmount: "10.012345", ReceiveAddress: "final-address", BlockTransactionID: "final-chain-transaction",
			}, "")
			require.NoError(t, err)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
		for _, private := range []string{merchant.Key, merchant.ID, merchant.MerchantID, merchant.AccountID, "merchant_version_id", "receipt_scope", "account_id"} {
			assert.NotContains(t, recorder.Body.String(), private)
		}
		detail := decodeNextResponse[NextOrderDTO](t, recorder)
		require.NotNil(t, detail.Gmpay)
		assert.Equal(t, model.PaymentProviderGmpay, detail.Method)
		assert.Equal(t, "USD", detail.Currency)
		assert.Equal(t, "10.00", detail.Gmpay.GatewayAmount)
		assert.Equal(t, "9.99", detail.Gmpay.GatewayCryptoAmt)
		assert.Equal(t, "initial-address", detail.Gmpay.ReceiveAddress)
		assert.Equal(t, "tron", detail.Gmpay.Network, "the recorded network is the checkout choice")
		if settled {
			assert.Equal(t, "completed", detail.Status)
			assert.Equal(t, common.Quota(5_000_000), detail.Quota)
			assert.Equal(t, model.GmpayStatusSettled, detail.Gmpay.Status)
			assert.Equal(t, "10.012345", detail.Gmpay.SettledCryptoAmount)
			assert.Equal(t, "final-address", detail.Gmpay.SettledReceiveAddress)
			assert.Equal(t, "final-chain-transaction", detail.Gmpay.SettledTransactionID)
		} else {
			assert.Equal(t, "pending", detail.Status)
			assert.Zero(t, detail.Quota)
			assert.Equal(t, model.GmpayStatusReady, detail.Gmpay.Status)
			assert.Empty(t, detail.Gmpay.SettledCryptoAmount)
			assert.Empty(t, detail.Gmpay.SettledReceiveAddress)
			assert.Empty(t, detail.Gmpay.SettledTransactionID)
		}
	}
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events)
}
