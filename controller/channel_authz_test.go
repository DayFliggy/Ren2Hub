package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelHasSensitiveChanges(t *testing.T) {
	baseURL := "https://api.example.com"
	headerOverride := `{"Authorization":"Bearer {api_key}"}`
	origin := &model.Channel{
		Type:           1,
		Key:            "old-key",
		BaseURL:        &baseURL,
		HeaderOverride: &headerOverride,
		Models:         "gpt-4o",
		ChannelRatio:   "1",
	}

	t.Run("non-sensitive routing fields", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Models = "gpt-4o,gpt-4o-mini"
		updated.MaxConcurrency = 2

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
			"models":          updated.Models,
			"channel_ratio":   origin.ChannelRatio,
			"max_concurrency": updated.MaxConcurrency,
		}))
	})

	t.Run("channel ratio change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.ChannelRatio = "1.5"

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"channel_ratio": updated.ChannelRatio}))
	})

	t.Run("key change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Key = "new-key"

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"key": updated.Key}))
	})

	t.Run("base url change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		newBaseURL := "https://leak.example.com"
		updated.BaseURL = &newBaseURL

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"base_url": newBaseURL}))
	})

	t.Run("header override change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		newHeaderOverride := `{"X-Key":"{api_key}"}`
		updated.HeaderOverride = &newHeaderOverride

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"header_override": newHeaderOverride}))
	})

	t.Run("omitted sensitive fields do not use zero values", func(t *testing.T) {
		updated := PatchChannel{}
		updated.Id = origin.Id
		updated.Priority = origin.Priority

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"priority": 10}))
	})

	t.Run("unknown field fails closed", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"future_secret_field": "x"}))
	})

	t.Run("status is operational", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Status = common.ChannelStatusManuallyDisabled

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"status": updated.Status}))
	})

	t.Run("read-only fields are ignored by sensitivity check", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Balance = 99
		updated.UsedQuota = 100
		updated.ResponseTime = 200

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
			"balance":       updated.Balance,
			"used_quota":    updated.UsedQuota,
			"response_time": updated.ResponseTime,
		}))
	})
}

func TestClearChannelReadOnlyFields(t *testing.T) {
	channel := PatchChannel{Channel: model.Channel{
		CreatedTime:        11,
		TestTime:           22,
		ResponseTime:       33,
		Balance:            44.5,
		BalanceUpdatedTime: 55,
		UsedQuota:          66,
		InFlight:           7,
		Lab:                modellab.Classification{ID: "client-lab", Name: "client name", Source: "client"},
		Models:             "gpt-4o",
	}}

	clearChannelReadOnlyFields(&channel, map[string]any{
		"created_time":         channel.CreatedTime,
		"test_time":            channel.TestTime,
		"response_time":        channel.ResponseTime,
		"balance":              channel.Balance,
		"balance_updated_time": channel.BalanceUpdatedTime,
		"used_quota":           channel.UsedQuota,
		"in_flight":            channel.InFlight,
		"lab":                  channel.Lab,
		"models":               channel.Models,
	})

	assert.Zero(t, channel.CreatedTime)
	assert.Zero(t, channel.TestTime)
	assert.Zero(t, channel.ResponseTime)
	assert.Zero(t, channel.Balance)
	assert.Zero(t, channel.BalanceUpdatedTime)
	assert.Zero(t, channel.UsedQuota)
	assert.Zero(t, channel.InFlight)
	assert.Empty(t, channel.Lab)
	assert.Equal(t, "gpt-4o", channel.Models)
}

func TestChannelWriteUpdatesRoutingWithoutChangingServerFields(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.True(t, authz.Can(2, common.RoleAdminUser, authz.ChannelWrite))
	require.False(t, authz.Can(2, common.RoleAdminUser, authz.ChannelSensitiveWrite))
	channel := model.Channel{
		Type: 1, Name: "routing-permissions", Key: "server-key", Models: "gpt-test",
		Status: common.ChannelStatusEnabled, ChannelRatio: "1", MaxConcurrency: 0,
		CreatedTime: 11, TestTime: 22, ResponseTime: 33,
		Balance: 44.5, BalanceUpdatedTime: 55, UsedQuota: 66,
	}
	require.NoError(t, channel.Insert())
	payload := fmt.Sprintf(`{"id":%d,"channel_ratio":"1.0","max_concurrency":2,"in_flight":123,"lab":{"id":"client-lab","name":"client name","source":"client"},"created_time":111,"test_time":222,"response_time":333,"balance":444,"balance_updated_time":555,"used_quota":666}`, channel.Id)
	response := putUpdateChannel(t, 2, common.RoleAdminUser, payload)
	require.Contains(t, response.Body.String(), `"success":true`, "an unchanged ratio in canonical or equivalent form stays editable")
	var saved model.Channel
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "1", saved.ChannelRatio)
	assert.Equal(t, 2, saved.MaxConcurrency)
	assert.Equal(t, channel.CreatedTime, saved.CreatedTime)
	assert.Equal(t, channel.TestTime, saved.TestTime)
	assert.Equal(t, channel.ResponseTime, saved.ResponseTime)
	assert.Equal(t, channel.Balance, saved.Balance)
	assert.Equal(t, channel.BalanceUpdatedTime, saved.BalanceUpdatedTime)
	assert.Equal(t, channel.UsedQuota, saved.UsedQuota)
	assert.Zero(t, saved.InFlight)
	assert.Empty(t, saved.Lab)

	response = putUpdateChannel(t, 2, common.RoleAdminUser, fmt.Sprintf(`{"id":%d,"key":"changed-secret"}`, channel.Id))
	require.Contains(t, response.Body.String(), `"success":false`)
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "server-key", saved.Key)
}

// B12：channel_ratio 决定所有用户的计费，只有 ChannelSensitiveWrite 能修改；
// max_concurrency 只影响运行，ChannelWrite 即可修改。
func TestChannelRatioRequiresSensitiveWrite(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	channel := model.Channel{Type: 1, Name: "ratio-permissions", Key: "server-key", Models: "gpt-test", Status: common.ChannelStatusEnabled, ChannelRatio: "1"}
	require.NoError(t, channel.Insert())

	denied := putUpdateChannel(t, 2, common.RoleAdminUser, fmt.Sprintf(`{"id":%d,"channel_ratio":"0.01","max_concurrency":3}`, channel.Id))
	require.Contains(t, denied.Body.String(), `"success":false`)
	var saved model.Channel
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "1", saved.ChannelRatio)
	assert.Zero(t, saved.MaxConcurrency, "a rejected request must not apply any field")

	allowed := putUpdateChannel(t, 2, common.RoleAdminUser, fmt.Sprintf(`{"id":%d,"max_concurrency":3}`, channel.Id))
	require.Contains(t, allowed.Body.String(), `"success":true`)
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.Equal(t, 3, saved.MaxConcurrency)

	require.True(t, authz.Can(1, common.RoleRootUser, authz.ChannelSensitiveWrite))
	root := putUpdateChannel(t, 1, common.RoleRootUser, fmt.Sprintf(`{"id":%d,"channel_ratio":"0.5"}`, channel.Id))
	require.Contains(t, root.Body.String(), `"success":true`)
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "0.5", saved.ChannelRatio)
}

func TestAddChannelIgnoresServerProjections(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	response := postAddChannel(t, 2, common.RoleAdminUser, `{"mode":"single","channel":{"type":1,"name":"routing-create","key":"server-key","models":"gpt-test","channel_ratio":"1.25","max_concurrency":3,"in_flight":123,"lab":{"id":"client-lab","source":"client"}}}`)
	require.Contains(t, response.Body.String(), `"success":true`)
	var saved model.Channel
	require.NoError(t, model.DB.Where("name = ?", "routing-create").First(&saved).Error)
	assert.Equal(t, "1.25", saved.ChannelRatio)
	assert.Equal(t, 3, saved.MaxConcurrency)
	assert.Zero(t, saved.InFlight)
	assert.Empty(t, saved.Lab)
}

func TestUpdateChannelRejectsStatusField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/channel/",
		bytes.NewBufferString(`{"id":1,"status":2}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
}

func TestChannelStatusValidation(t *testing.T) {
	assert.True(t, isManageableChannelStatus(common.ChannelStatusEnabled))
	assert.True(t, isManageableChannelStatus(common.ChannelStatusManuallyDisabled))
	assert.False(t, isManageableChannelStatus(common.ChannelStatusAutoDisabled))
	assert.False(t, isManageableChannelStatus(0))
}

// TestChannelFieldsAreClassified guards the fail-closed sensitivity check: every
// JSON field of PatchChannel (including the embedded model.Channel) must be listed
// in channelSensitiveFields, channelNonSensitiveFields, or
// channelOperationalFields. A newly added field that is left unclassified will
// fail this test, forcing a conscious permission decision instead of silently
// defaulting either way.
func TestChannelFieldsAreClassified(t *testing.T) {
	classified := func(name string) bool {
		if _, ok := channelSensitiveFields[name]; ok {
			return true
		}
		if _, ok := channelNonSensitiveFields[name]; ok {
			return true
		}
		if _, ok := channelOperationalFields[name]; ok {
			return true
		}
		_, ok := channelReadOnlyFields[name]
		return ok
	}

	var collect func(rt reflect.Type) []string
	collect = func(rt reflect.Type) []string {
		var names []string
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				names = append(names, collect(field.Type)...)
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			names = append(names, name)
		}
		return names
	}

	for _, name := range collect(reflect.TypeFor[PatchChannel]()) {
		assert.Truef(t, classified(name),
			"channel field %q is not classified; add it to channelSensitiveFields, channelNonSensitiveFields, channelOperationalFields, or channelReadOnlyFields in channel_authz.go", name)
	}
	assert.False(t, classified("group"), "retired business groups must not remain writable channel fields")
}
