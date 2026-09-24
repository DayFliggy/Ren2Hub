package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentOptionCatalogPreservesMerchantAndSecretStringContracts(t *testing.T) {
	for _, value := range []string{"00123", "9007199254740993", "true", "1e20"} {
		t.Run(value, func(t *testing.T) {
			withOptionMap(t, map[string]string{
				"EpayId": value, "GmpayId": value, "EpayKey": value, "GmpayKey": value,
			})
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/option/catalog", nil)
			GetOptionCatalog(context)
			require.Equal(t, http.StatusOK, response.Code)
			var catalog struct {
				Success bool               `json:"success"`
				Data    []OptionDescriptor `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
			require.True(t, catalog.Success)
			require.Len(t, catalog.Data, 4)
			for _, descriptor := range catalog.Data {
				assert.Equal(t, "string", descriptor.ValueType, descriptor.Key)
				assert.Equal(t, "", descriptor.DefaultValue, descriptor.Key)
				if descriptor.Key == "EpayKey" || descriptor.Key == "GmpayKey" {
					assert.True(t, descriptor.Sensitive)
					assert.Equal(t, "secret", descriptor.Editor)
				} else {
					assert.False(t, descriptor.Sensitive)
					assert.Equal(t, "text", descriptor.Editor)
				}
			}
			options := httptest.NewRecorder()
			context, _ = gin.CreateTestContext(options)
			GetOptions(context)
			var values struct {
				Data []struct{ Key, Value string } `json:"data"`
			}
			require.NoError(t, common.Unmarshal(options.Body.Bytes(), &values))
			require.Len(t, values.Data, 2)
			for _, option := range values.Data {
				assert.Contains(t, []string{"EpayId", "GmpayId"}, option.Key)
				assert.Equal(t, value, option.Value)
			}
		})
	}
}

func TestPaymentOptionCatalogDefaultsDoNotDependOnSavedValues(t *testing.T) {
	for _, tc := range []struct {
		key, valueType, editor string
		defaultValue           any
	}{
		{"PayAddress", "string", "url", ""},
		{"GmpayPayAddress", "string", "url", ""},
		{"CustomCallbackAddress", "string", "url", ""},
		{"Price", "number", "ratio", 7.3},
		{"GmpayPrice", "number", "ratio", 1.0},
		{"MinTopUp", "number", "number", 1},
		{"GmpayMinTopUp", "number", "number", 1},
		{"GmpayAssets", "json", "json", []any{}},
		{"GmpayGroupRatio", "json", "ratio", map[string]any{"default": 1}},
		{"TopupGroupRatio", "json", "ratio", map[string]any{"default": 1, "vip": 1, "svip": 1}},
		{"payment_setting.amount_options", "json", "amount-list", []int{10, 20, 50, 100, 200, 500}},
		{"payment_setting.amount_discount", "json", "discount", map[string]any{}},
		{"payment_setting.epay_settlement_enabled", "boolean", "toggle", true},
		{"payment_setting.gmpay_settlement_enabled", "boolean", "toggle", true},
	} {
		t.Run(tc.key, func(t *testing.T) {
			for _, saved := range []string{"", "false", "999", "{}"} {
				descriptor := optionDescriptorFor(tc.key, saved)
				assert.Equal(t, tc.valueType, descriptor.ValueType)
				assert.Equal(t, tc.editor, descriptor.Editor)
				assert.Equal(t, tc.defaultValue, descriptor.DefaultValue)
			}
		})
	}
	descriptor := optionDescriptorFor("PayMethods", "null")
	assert.Equal(t, "json", descriptor.ValueType)
	encoded, err := common.Marshal(descriptor.DefaultValue)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"name":"支付宝","type":"alipay","icon":"SiAlipay"},{"name":"微信","type":"wxpay","icon":"SiWechat"},{"name":"自定义1","type":"custom1","icon":"LuCreditCard","min_topup":"50"}]`, string(encoded))
}
