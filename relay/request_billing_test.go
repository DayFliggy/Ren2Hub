package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A retry that lands on a pricier channel reuses the first attempt's session
// and must hold the new price before the request is sent; a saturated price
// stops the retry before any further reservation.
func TestPrepareRequestBillingRetryReservesThroughPreConsume(t *testing.T) {
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"retry-billing-model":0.01}`))
	previousCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = previousCountToken })

	firstQuota := int(0.01 * common.QuotaPerUnit)
	for _, tc := range []struct {
		name      string
		limit     int
		clamp     bool
		wantCode  types.ErrorCode
		wantHeld  int
		wantState int
	}{
		{name: "pricier channel is held before sending", limit: 1 << 30, wantHeld: 2 * firstQuota},
		{name: "unaffordable channel is rejected", limit: firstQuota, wantCode: types.ErrorCodeInsufficientUserQuota, wantHeld: firstQuota, wantState: http.StatusForbidden},
		{name: "saturated quota is rejected before reserving", limit: 1 << 30, clamp: true, wantCode: types.ErrorCodeModelPriceError, wantHeld: firstQuota, wantState: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("channel_ratio", "2")
			reservation := &imageReservation{held: firstQuota, limit: tc.limit}
			info := &relaycommon.RelayInfo{OriginModelName: "retry-billing-model", Billing: reservation}
			if tc.clamp {
				info.QuotaClamp = &common.QuotaClamp{Op: "QuotaFromFloat", Kind: common.QuotaClampOverflow, Original: 1e30, Clamped: common.MaxQuota}
			}

			apiErr := PrepareRequestBilling(c, info)

			if tc.wantCode == "" {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
				require.Equal(t, tc.wantCode, apiErr.GetErrorCode())
				require.Equal(t, tc.wantState, apiErr.StatusCode)
			}
			require.Equal(t, tc.wantHeld, reservation.held)
		})
	}
}
