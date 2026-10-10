package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateSetupCredentialsUsesUnicodeCharacterCounts(t *testing.T) {
	tests := []struct {
		name    string
		request SetupRequest
		wantKey string
	}{
		{
			name: "accepts twelve unicode username characters",
			request: SetupRequest{
				Username:        "一二三四五六七八九十甲乙",
				Password:        "密码一二三四五六",
				ConfirmPassword: "密码一二三四五六",
			},
		},
		{
			name: "rejects thirteen unicode username characters",
			request: SetupRequest{
				Username:        "一二三四五六七八九十甲乙丙",
				Password:        "password123",
				ConfirmPassword: "password123",
			},
			wantKey: "Username must be {{max}} characters or fewer",
		},
		{
			name: "accepts eight unicode password characters",
			request: SetupRequest{
				Username:        "admin",
				Password:        "密码一二三四五六",
				ConfirmPassword: "密码一二三四五六",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSetupCredentials(test.request)
			if test.wantKey == "" {
				require.NoError(t, err)
				return
			}
			var message *common.Message
			require.ErrorAs(t, err, &message)
			assert.Equal(t, test.wantKey, message.Key)
		})
	}
}

func TestPostSetupCreatesActiveRootAffiliateSeed(t *testing.T) {
	previousDB := model.DB
	previousDatabaseType := common.MainDatabaseType()
	previousSetup := constant.Setup
	previousOptionMap := common.OptionMap
	previousSelfUseMode := operation_setting.SelfUseModeEnabled
	previousDemoSite := operation_setting.DemoSiteEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.Setup{}, &model.BillingEvent{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.OptionMap = map[string]string{}
	constant.Setup = false

	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousDatabaseType)
		common.OptionMap = previousOptionMap
		constant.Setup = previousSetup
		operation_setting.SelfUseModeEnabled = previousSelfUseMode
		operation_setting.DemoSiteEnabled = previousDemoSite
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{
		"username":"rootadmin",
		"password":"password123",
		"confirmPassword":"password123",
		"SelfUseModeEnabled":false,
		"DemoSiteEnabled":false
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	PostSetup(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var root model.User
	require.NoError(t, db.Where("role = ?", common.RoleRootUser).First(&root).Error)
	assert.Len(t, root.AffCode, 12)
	assert.True(t, root.AffCodeEnabled)
	var events []model.BillingEvent
	require.NoError(t, db.Where("user_id = ?", root.Id).Find(&events).Error)
	require.Len(t, events, 1)
	assert.EqualValues(t, 100000000, root.Quota)
	assert.Equal(t, root.Quota, events[0].AvailableDelta)
	assert.Equal(t, "bootstrap", events[0].SourceType)
	hasSeed, err := model.HasActiveAffiliateSeed()
	require.NoError(t, err)
	assert.True(t, hasSeed)
}
