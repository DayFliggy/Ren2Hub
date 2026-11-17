package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setHeaderNavModulesOption(t *testing.T, raw string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	previous, hadPrevious := common.OptionMap["HeaderNavModules"]
	common.OptionMap["HeaderNavModules"] = raw
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadPrevious {
			common.OptionMap["HeaderNavModules"] = previous
			return
		}
		delete(common.OptionMap, "HeaderNavModules")
	})
}

func getAPIWithBearer(engine http.Handler, path, bearer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func TestPricingCountFollowsThePricingCatalogAndGate(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	for _, channel := range []model.Channel{
		{Id: 1, Type: 1, Key: "pricing-count-1", Status: common.ChannelStatusEnabled, Name: "pricing-count-1"},
		{Id: 2, Type: 1, Key: "pricing-count-2", Status: common.ChannelStatusEnabled, Name: "pricing-count-2"},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	for _, ability := range []model.Ability{
		{Model: "alpha", ChannelId: 1, Enabled: true},
		{Model: "beta", ChannelId: 1, Enabled: true},
		{Model: "beta", ChannelId: 2, Enabled: true},
		{Model: "gamma", ChannelId: 2, Enabled: true},
		{Model: "delta", ChannelId: 2, Enabled: false},
	} {
		require.NoError(t, db.Create(&ability).Error)
	}
	hidden := model.Model{ModelName: "gamma"}
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Model(&hidden).Update("status", 0).Error)
	_, userToken := createSystemStatusSession(t, db, 91201, "pricing-count-user", common.RoleCommonUser)
	_, rootToken := createSystemStatusSession(t, db, 91202, "pricing-count-root", common.RoleRootUser)

	disabled := `{"pricing":{"enabled":false,"requireAuth":false}}`
	loginRequired := `{"pricing":{"enabled":true,"requireAuth":true}}`
	for _, tc := range []struct {
		name, options, bearer string
		status                int
	}{
		{"public guest", "", "", http.StatusOK},
		{"disabled guest", disabled, "", http.StatusForbidden},
		{"disabled user", disabled, userToken, http.StatusForbidden},
		{"disabled root", disabled, rootToken, http.StatusOK},
		{"login required guest", loginRequired, "", http.StatusUnauthorized},
		{"login required user", loginRequired, userToken, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setHeaderNavModulesOption(t, tc.options)

			catalog := getAPIWithBearer(engine, "/api/pricing", tc.bearer)
			count := getAPIWithBearer(engine, "/api/pricing/count", tc.bearer)

			require.Equal(t, tc.status, catalog.Code, catalog.Body.String())
			require.Equal(t, tc.status, count.Code, count.Body.String())
			if tc.status != http.StatusOK {
				return
			}
			var catalogBody struct {
				Data []any `json:"data"`
			}
			var countBody struct {
				Success bool `json:"success"`
				Data    struct {
					Models int `json:"models"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(catalog.Body.Bytes(), &catalogBody))
			require.NoError(t, common.Unmarshal(count.Body.Bytes(), &countBody))
			assert.True(t, countBody.Success)
			assert.Equal(t, 2, countBody.Data.Models, "alpha and beta; gamma is hidden and delta disabled")
			assert.Len(t, catalogBody.Data, countBody.Data.Models)
		})
	}
}
