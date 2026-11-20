package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCreationRequiresOnlyKeyWrite(t *testing.T) {
	user, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	router := newAccessTokenTestRouter()
	router.POST("/api/token/", middleware.UserAuth(), AddToken)
	keyOnly, _ := createScopedAccessToken(t, user.Id, 0, "api_key:write")
	for _, tc := range []struct {
		name, extra string
		status      int
	}{
		{"omitted", "", 200}, {"auto", `,"type":"auto"`, 200}, {"manual", `,"type":"manual"`, 400}, {"ordered", `,"type":"ordered"`, 400}, {"unknown", `,"type":"x"`, 400}, {"null", `,"type":null`, 400}, {"numeric", `,"type":42`, 400}, {"empty", `,"type":""`, 400},
	} {
		body := fmt.Sprintf(`{"name":%q,"unlimited_quota":true%s}`, tc.name, tc.extra)
		result := accessTokenRequest(router, http.MethodPost, "/api/token/", keyOnly, "", body)
		assert.Equal(t, tc.status, result.Code, result.Body.String())
	}
	var names []string
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Order("name").Pluck("name", &names).Error)
	require.Equal(t, []string{"auto", "omitted"}, names)
	var count int64
	require.NoError(t, model.DB.Model(&model.TokenRouteProfile{}).Count(&count).Error)
	require.Zero(t, count)
}
