package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTopUpInfoListsBackupPaymentsOnlyAfterCompliance(t *testing.T) {
	useGmpayControllerDB(t)
	oldPayment, oldLink, oldOptions := *operation_setting.GetPaymentSetting(), common.TopUpLink, common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		require.NoError(t, model.UpdateOption(operation_setting.BackupPaymentLinksOptionKey, "[]"))
		*operation_setting.GetPaymentSetting() = oldPayment
		common.TopUpLink, common.OptionMap = oldLink, oldOptions
	})
	common.TopUpLink = "https://codes.example.com"
	require.NoError(t, model.UpdateOption(operation_setting.BackupPaymentLinksOptionKey, `[{"name":"Shop","url":"https://shop.example.com","icon":"telegram"}]`))
	require.Error(t, model.UpdateOption(operation_setting.BackupPaymentLinksOptionKey, `[{"name":"Shop","url":"javascript:alert(1)"}]`))

	info := func() []byte {
		engine := gin.New()
		engine.GET("/info", GetTopUpInfo)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/info", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var body struct {
			Data struct {
				Links []operation_setting.BackupPaymentLink `json:"backup_payment_links"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		links, err := common.Marshal(body.Data.Links)
		require.NoError(t, err)
		return links
	}

	operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	require.JSONEq(t, `[]`, string(info()))
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.JSONEq(t, `[{"name":"","url":"https://codes.example.com","icon":""},{"name":"Shop","url":"https://shop.example.com","icon":"telegram"}]`, string(info()))
}
