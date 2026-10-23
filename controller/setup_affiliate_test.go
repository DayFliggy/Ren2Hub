package controller

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupInitializesRootInvitationCode(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.Setup{}))
	oldSetup := constant.Setup
	oldSelfUse, oldDemo := operation_setting.SelfUseModeEnabled, operation_setting.DemoSiteEnabled
	common.OptionMapRWMutex.Lock()
	oldOptions := maps.Clone(common.OptionMap)
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	constant.Setup = false
	t.Cleanup(func() {
		constant.Setup = oldSetup
		operation_setting.SelfUseModeEnabled, operation_setting.DemoSiteEnabled = oldSelfUse, oldDemo
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	router := gin.New()
	router.POST("/api/setup", PostSetup)
	body, err := json.Marshal(SetupRequest{Username: "setup-root", Password: "SetupPassword2026!", ConfirmPassword: "SetupPassword2026!"})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var result struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	var root model.User
	require.NoError(t, db.Where("role = ?", common.RoleRootUser).First(&root).Error)
	assert.Regexp(t, `^[A-Z0-9_-]{12}$`, root.AffCode)
	assert.True(t, root.AffCodeEnabled)
	assert.Zero(t, root.InviterId)
	assert.Zero(t, root.AffCount)
}
