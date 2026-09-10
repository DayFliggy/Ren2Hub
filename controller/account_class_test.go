package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountClassControllerTreatsCodesAsData(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/account-classes", CreateAccountClass)
	router.PUT("/api/account-classes", UpdateAccountClass)
	router.DELETE("/api/account-classes", DeleteAccountClass)

	for _, code := range []string{"legacy/a?b#c", ".", "../partner", "合作方/?#", "group"} {
		t.Run(code, func(t *testing.T) {
			input := model.AccountClass{Code: code, Name: "Partner", DefaultConcurrency: 5, TopupMultiplier: 1}
			payload, err := common.Marshal(input)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/api/account-classes", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			created := decodeNextResponse[map[string]any](t, recorder)
			assert.Equal(t, code, created["code"])

			input.Name, input.PolicyVersion, input.DefaultConcurrency = "Updated", 1, 0
			payload, err = common.Marshal(input)
			require.NoError(t, err)
			request = httptest.NewRequest(http.MethodPut, "/api/account-classes", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			recorder = httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			updated := decodeNextResponse[map[string]any](t, recorder)
			assert.Equal(t, code, updated["code"])
			assert.Equal(t, float64(2), updated["policy_version"])
			var stored model.AccountClass
			require.NoError(t, db.First(&stored, "code = ?", code).Error)
			assert.Equal(t, "Updated", stored.Name)
			assert.Zero(t, stored.DefaultConcurrency)

			request = httptest.NewRequest(http.MethodDelete, "/api/account-classes?"+url.Values{"code": {code}}.Encode(), nil)
			recorder = httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			deleted := decodeNextResponse[map[string]any](t, recorder)
			assert.Equal(t, code, deleted["code"])
			var count int64
			require.NoError(t, db.Model(&model.AccountClass{}).Where("code = ?", code).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestAccountClassControllerRejectsRetiredRoutingFields(t *testing.T) {
	for _, test := range []struct {
		name, method, target, body string
		handler                    gin.HandlerFunc
	}{
		{"list", http.MethodGet, "/api/account-classes?group=", "", ListAccountClasses},
		{"limits", http.MethodGet, "/api/user/limits?group=vip", "", GetOwnAdmissionLimits},
		{"create", http.MethodPost, "/api/account-classes", `{"code":"partner","group":null}`, CreateAccountClass},
		{"update", http.MethodPut, "/api/account-classes", `{"code":"default","group_ratio":0}`, UpdateAccountClass},
		{"delete", http.MethodDelete, "/api/account-classes?code=partner&Group=vip", "", DeleteAccountClass},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performNextAdminJSON(t, test.method, test.target, test.body, test.handler)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, "retired_field", response.Code)
		})
	}
}
