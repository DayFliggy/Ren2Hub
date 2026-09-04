package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withOptionMap(t *testing.T, values map[string]string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = values
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

func TestValidateOptionPatchUsesFinalOAuthState(t *testing.T) {
	withOptionMap(t, map[string]string{
		"GitHubClientId":     "",
		"GitHubClientSecret": "",
	})

	err := validateOptionPatch(map[string]string{
		"GitHubClientId":     "client-id",
		"GitHubClientSecret": "client-secret",
		"GitHubOAuthEnabled": "true",
	})
	require.NoError(t, err)
}

func TestValidateOptionPatchRequiresSafeCompleteOIDCEndpoints(t *testing.T) {
	withOptionMap(t, map[string]string{
		"oidc.client_id":     "client-id",
		"oidc.client_secret": "client-secret",
	})

	assert.Error(t, validateOptionPatch(map[string]string{
		"oidc.enabled": "true",
	}))

	assert.Error(t, validateOptionPatch(map[string]string{
		"oidc.enabled":                "true",
		"oidc.authorization_endpoint": "https://127.0.0.1/authorize",
		"oidc.token_endpoint":         "https://8.8.8.8/token",
		"oidc.user_info_endpoint":     "https://8.8.8.8/userinfo",
	}))

	assert.NoError(t, validateOptionPatch(map[string]string{
		"oidc.enabled":                "true",
		"oidc.authorization_endpoint": "https://8.8.8.8/authorize",
		"oidc.token_endpoint":         "https://8.8.8.8/token",
		"oidc.user_info_endpoint":     "https://8.8.8.8/userinfo",
	}))
}

func TestUpdateOptionsBulkRejectsInvalidPatchBeforePersistence(t *testing.T) {
	withOptionMap(t, map[string]string{
		"GitHubClientId":     "",
		"GitHubClientSecret": "",
	})

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/bulk",
		strings.NewReader(`{"options":{"GitHubOAuthEnabled":true}}`),
	)

	UpdateOptionsBulk(context)

	assert.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.Contains(t, payload.Message, "GitHub Client Id")
	assert.Equal(t, "", common.OptionMap["GitHubClientId"])
}

func TestUpdateOptionsBulkRejectsNonScalarValues(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/bulk",
		strings.NewReader(`{"options":{"SystemName":{"invalid":true}}}`),
	)

	UpdateOptionsBulk(context)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "字符串、数值或布尔值")
}

func TestGetOptionSecretStatusNeverReturnsSecretValues(t *testing.T) {
	withOptionMap(t, map[string]string{
		"SystemName":         "Ren2Hub",
		"GitHubClientSecret": "private-value",
		"SMTPToken":          "mail-password",
		"WaffoPublicCert":    "certificate-value",
	})

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/option/secret-status", nil)

	GetOptionSecretStatus(context)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), "private-value")
	assert.NotContains(t, response.Body.String(), "mail-password")
	assert.NotContains(t, response.Body.String(), "certificate-value")
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Configured []string `json:"configured"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, []string{"GitHubClientSecret", "SMTPToken", "WaffoPublicCert"}, payload.Data.Configured)
}

func TestValidateOptionPatchAcceptsNewStructuredSettings(t *testing.T) {
	withOptionMap(t, map[string]string{})
	err := validateOptionPatch(map[string]string{
		"billing_setting.billing_mode":      `{"gpt-4":"ratio"}`,
		"billing_setting.billing_expr":      `{"gpt-4":"p + c"}`,
		"payment_setting.amount_options":    `[10,20]`,
		"payment_setting.amount_discount":   `{"100":0.9}`,
		"global.thinking_model_blacklist":   `["kimi-k2-thinking"]`,
		"channel_affinity_setting.rules":    `[{"name":"codex","model_regex":["^gpt-"],"path_regex":["/v1/responses"],"ttl_seconds":0}]`,
		"monitor_setting.channel_test_mode": "passive_recovery",
		"FileUploadPermission":              "10",
	})
	require.NoError(t, err)
}

func TestValidateOptionPatchRejectsInvalidStructuredSettings(t *testing.T) {
	withOptionMap(t, map[string]string{})
	assert.Error(t, validateOptionPatch(map[string]string{"payment_setting.amount_discount": `{"100":1.2}`}))
	assert.Error(t, validateOptionPatch(map[string]string{"monitor_setting.channel_test_mode": "invalid"}))
	assert.Error(t, validateOptionPatch(map[string]string{"FileUploadPermission": "2"}))
}
