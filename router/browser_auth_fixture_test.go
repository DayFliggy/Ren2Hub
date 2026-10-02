package router

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// createRouterBrowserSession exercises the same JWT/session validation used by
// the dashboard while keeping resource-authorization tests independent of PATs.
func createRouterBrowserSession(t *testing.T, userID int) string {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.UserSession{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("auth_version", 1).Error)
	bundle, err := service.CreateLoginSession(userID, "password", "127.0.0.1", "router-test")
	require.NoError(t, err)
	return bundle.AccessToken
}
