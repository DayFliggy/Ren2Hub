package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelCapacityRatioBounds(t *testing.T) {
	base := func() *model.Channel {
		return &model.Channel{
			Name:   "bounds-test",
			Type:   constant.ChannelTypeOpenAI,
			Key:    "sk-test",
			Models: "gpt-4o",
		}
	}

	t.Run("add applies defaults for unset fields", func(t *testing.T) {
		channel := base()
		require.NoError(t, validateChannel(channel, true))
		assert.Equal(t, model.DefaultChannelCapacityTotal, channel.CapacityTotal)
		assert.Equal(t, model.DefaultChannelRatio, channel.ChannelRatio)
	})

	t.Run("update keeps zero as unset", func(t *testing.T) {
		channel := base()
		require.NoError(t, validateChannel(channel, false))
		assert.Zero(t, channel.CapacityTotal)
		assert.Zero(t, channel.ChannelRatio)
	})

	outOfRange := []struct {
		name   string
		mutate func(*model.Channel)
	}{
		{name: "negative capacity", mutate: func(c *model.Channel) { c.CapacityTotal = -1 }},
		{name: "capacity above max", mutate: func(c *model.Channel) { c.CapacityTotal = model.MaxChannelCapacityTotal + 1 }},
		{name: "negative channel ratio", mutate: func(c *model.Channel) { c.ChannelRatio = "-0.5" }},
		{name: "channel ratio above max", mutate: func(c *model.Channel) { c.ChannelRatio = "1001" }},
		{name: "zero channel ratio", mutate: func(c *model.Channel) { c.ChannelRatio = "0" }},
		{name: "non-finite channel ratio", mutate: func(c *model.Channel) { c.ChannelRatio = "NaN" }},
	}
	for _, test := range outOfRange {
		t.Run(test.name+" rejected on add", func(t *testing.T) {
			channel := base()
			test.mutate(channel)
			assert.Error(t, validateChannel(channel, true))
		})
		t.Run(test.name+" rejected on update", func(t *testing.T) {
			channel := base()
			test.mutate(channel)
			assert.Error(t, validateChannel(channel, false))
		})
	}
}

func TestValidateChannelNormalizesEmptyGroupOnAdd(t *testing.T) {
	base := func() *model.Channel {
		return &model.Channel{
			Name:   "group-test",
			Type:   constant.ChannelTypeOpenAI,
			Key:    "sk-test",
			Models: "gpt-4o",
		}
	}

	t.Run("empty group defaults on add", func(t *testing.T) {
		channel := base()
		require.NoError(t, validateChannel(channel, true))
		assert.Equal(t, "default", channel.Group)
	})

	t.Run("blank group defaults on add", func(t *testing.T) {
		channel := base()
		channel.Group = "  "
		require.NoError(t, validateChannel(channel, true))
		assert.Equal(t, "default", channel.Group)
	})

	t.Run("custom group preserved on add", func(t *testing.T) {
		channel := base()
		channel.Group = "vip"
		require.NoError(t, validateChannel(channel, true))
		assert.Equal(t, "vip", channel.Group)
	})

	t.Run("update keeps empty group as unset", func(t *testing.T) {
		channel := base()
		require.NoError(t, validateChannel(channel, false))
		assert.Empty(t, channel.Group)
	})
}

// The next frontend rejects success envelopes without a data key, so channel
// creation must echo the created count as data.
func TestAddChannelSuccessEnvelopeIncludesData(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))

	requestBody, err := common.Marshal(AddChannelRequest{
		Mode: "single",
		Channel: &model.Channel{
			Name:   "envelope-test",
			Type:   constant.ChannelTypeOpenAI,
			Key:    "sk-test",
			Models: "gpt-4o",
		},
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/next/admin/channels", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AddChannel(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    *int `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.NotNil(t, response.Data, "success envelope must include data")
	assert.Equal(t, 1, *response.Data)
}

func TestAddChannelCanImmediatelyDisableWithMemoryCache(t *testing.T) {
	for _, mode := range []string{"single", "batch", "multi_to_single"} {
		t.Run(mode, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			previousMemoryCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = true
			model.InitChannelCache()
			t.Cleanup(func() {
				require.NoError(t, db.Exec("DELETE FROM channels").Error)
				model.InitChannelCache()
				common.MemoryCacheEnabled = previousMemoryCache
			})
			key := "test-key-one"
			if mode != "single" {
				key += "\ntest-key-two"
			}
			body, err := common.Marshal(AddChannelRequest{
				Mode: mode, MultiKeyMode: constant.MultiKeyModePolling,
				Channel: &model.Channel{Name: "immediate-disable", Type: constant.ChannelTypeOpenAI,
					Key: key, Models: "gpt-4o", Status: common.ChannelStatusEnabled},
			})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/next/admin/channels", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			AddChannel(ctx)
			var created struct {
				Success bool `json:"success"`
				Data    int  `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &created))
			require.True(t, created.Success)
			var channels []model.Channel
			require.NoError(t, db.Find(&channels).Error)
			require.Len(t, channels, created.Data)
			require.NotEmpty(t, channels)
			for _, channel := range channels {
				body, err = common.Marshal(ChannelStatusRequest{Status: common.ChannelStatusManuallyDisabled})
				require.NoError(t, err)
				recorder = httptest.NewRecorder()
				ctx, _ = gin.CreateTestContext(recorder)
				ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}
				ctx.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/next/admin/channels/%d/status", channel.Id), bytes.NewReader(body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				UpdateChannelStatus(ctx)
				var updated struct {
					Success bool `json:"success"`
					Data    bool `json:"data"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updated))
				require.True(t, updated.Success)
				require.True(t, updated.Data, "a newly created channel must be immediately manageable")
				require.NoError(t, db.First(&channel, channel.Id).Error)
				assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
				cached, err := model.CacheGetChannel(channel.Id)
				require.NoError(t, err)
				assert.Equal(t, channel.Status, cached.Status)
				var ability model.Ability
				require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
				assert.False(t, ability.Enabled)
			}
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactAPITypeSupport(t *testing.T) {
	tests := []struct {
		name    string
		apiType int
		want    bool
	}{
		{name: "OpenAI", apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", apiType: constant.APITypeSub2API, want: true},
		{name: "New API", apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.IsResponsesCompactAPIType(test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"data":{"id":999999}`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	require.NotNil(t, auditLog.Other.Op)
	encoded, err := common.Marshal(auditLog.Other.Op.Params["count"])
	require.NoError(t, err)
	assert.JSONEq(t, "1", string(encoded))
}

func TestSettleTestQuotaUsesFrozenSafeAttributes(t *testing.T) {
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeExpression,
		Support:    &billing.PricingSupport{Dimensions: []string{"p"}, ServiceTiers: []string{"default", "fast"}},
		Expression: `attr("service_tier") == "fast" ? p * 3 : p * 2`}, "1", map[string]interface{}{"service_tier": "fast"}, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{Quote: quote}, ChannelMeta: &relaycommon.ChannelMeta{}}
	info.MarkValidOutput()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	quota, err := service.PrepareFinalTextCharge(ctx, info, &dto.Usage{PromptTokens: 1000, UsageSource: "provider"})
	require.NoError(t, err)
	assert.Equal(t, 1500, quota)
	require.NotNil(t, info.PriceData.Charge)
	assert.Equal(t, common.Quota(1500), info.PriceData.Charge.FinalQuota)
}

func TestChannelTestLogPreservesFrozenPricingEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	quote, err := billing.NewQuote(billing.PriceVersion{ID: 7, ScopeKind: billing.PriceScopeChannel, ChannelID: 9, Model: "test", ContractVersion: billing.ContractVersion, CalculationVersion: billing.CalculationVersion, ResultUnit: billing.ResultMicroUSD, Mode: billing.ModeExpression, Expression: `tier("base", p * 2)`}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{Quote: quote}, ChannelMeta: &relaycommon.ChannelMeta{}}
	usage := &dto.Usage{
		PromptTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	info.MarkValidOutput()
	_, err = service.PrepareFinalTextCharge(ctx, info, usage)
	require.NoError(t, err)
	other := service.GenerateTextOtherInfo(ctx, info, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, billing.PriceScopeChannel, other["price_scope"])
	assert.Equal(t, int64(7), other["price_version_id"])
	assert.Equal(t, quote.Hash, other["quote_hash"])
	assert.Equal(t, quote.PriceVersion.Hash, other["price_version_hash"])
	assert.Equal(t, info.PriceData.Charge, other["charge_result"])
	assert.NotContains(t, other, "expr_b64")
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
