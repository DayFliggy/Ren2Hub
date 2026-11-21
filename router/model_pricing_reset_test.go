package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPricingResetIsRejectedWithoutWriting(t *testing.T) {
	setupRelayRouterTestDB(t)
	db := model.DB
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })
	require.NoError(t, db.AutoMigrate(&model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}))
	engine := gin.New()
	SetApiRouter(engine)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	_, bearer := createSystemStatusSession(t, db, 91991, "pricing-reset-root", common.RoleRootUser)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"reset-test":2}`}).Error)
	version := model.ModelPricingVersion(model.PricingValues{"ModelPrice": float64(2)})
	body := fmt.Sprintf(`{"changes":[{"model_name":"reset-test","expected_version":%q,"pricing":{},"reset":true}]}`, version)
	for _, token := range []string{"", bearer, bearer} {
		req := httptest.NewRequest(http.MethodPatch, "/api/option/model_pricing", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if token == "" {
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			continue
		}
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "no built-in prices to restore; submit the prices to save")
		var option model.Option
		require.NoError(t, db.Where("key = ?", "ModelPrice").First(&option).Error)
		require.Equal(t, `{"reset-test":2}`, option.Value)
		var count int64
		require.NoError(t, db.Model(&model.Option{}).Count(&count).Error)
		require.EqualValues(t, 1, count, "reject before inserting any default options")
	}
}
