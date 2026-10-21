package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelManagementContractTest(t *testing.T) {
	t.Helper()
	// SearchChannels 依赖 InitDB 初始化的列名引用。
	initModelListColumnNames(t)
	setupTaskPluginBindChannelTest(t)
	gin.SetMode(gin.TestMode)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}, &model.Model{}, &model.Vendor{}))
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
}

func serveChannelAdmin(t *testing.T, handler gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Params = params
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func createContractChannel(t *testing.T, channel model.Channel) {
	t.Helper()
	if channel.Status == 0 {
		channel.Status = common.ChannelStatusEnabled
	}
	require.NoError(t, channel.Insert())
}

// C9：同时按 lab 和 type 筛选时，type_counts 只按 lab 计数，不能被 type 条件污染。
func TestGetAllChannelsTypeCountsIgnoreTypeFilterWithLabFilter(t *testing.T) {
	setupChannelManagementContractTest(t)
	createContractChannel(t, model.Channel{Id: 8101, Type: constant.ChannelTypeOpenAI, Name: "openai-a", Key: "k", Models: "gpt-4o"})
	createContractChannel(t, model.Channel{Id: 8102, Type: constant.ChannelTypeAzure, Name: "openai-azure", Key: "k", Models: "gpt-4o-mini"})
	createContractChannel(t, model.Channel{Id: 8103, Type: constant.ChannelTypeAnthropic, Name: "claude", Key: "k", Models: "claude-3-7-sonnet"})

	target := fmt.Sprintf("/api/channel/?lab=openai&type=%d&p=1&page_size=10", constant.ChannelTypeOpenAI)
	recorder := serveChannelAdmin(t, GetAllChannels, http.MethodGet, target, "", nil)

	var response struct {
		Success bool
		Data    struct {
			Items      []model.Channel
			Total      int64
			TypeCounts map[string]int64 `json:"type_counts"`
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 8101, response.Data.Items[0].Id)
	assert.EqualValues(t, 1, response.Data.Total)
	assert.Equal(t, map[string]int64{
		fmt.Sprint(constant.ChannelTypeOpenAI): 1,
		fmt.Sprint(constant.ChannelTypeAzure):  1,
	}, response.Data.TypeCounts)
}

// E2-4：in_flight 只在管理端响应里显式附加；普通查询不再隐式读取准入状态。
func TestAdminChannelResponsesAttachLiveInFlight(t *testing.T) {
	setupChannelManagementContractTest(t)
	createContractChannel(t, model.Channel{Id: 8201, Type: constant.ChannelTypeOpenAI, Name: "busy-channel", Key: "k", Models: "gpt-4o"})
	stored, err := model.GetChannelById(8201, false)
	require.NoError(t, err)
	permit, err := service.AcquireChannelConcurrency(context.Background(), stored)
	require.NoError(t, err)
	released := false
	t.Cleanup(func() {
		if !released {
			permit.Release()
		}
	})

	reloaded, err := model.GetChannelById(8201, false)
	require.NoError(t, err)
	assert.Zero(t, reloaded.InFlight, "a model query must not read admission state")

	inFlight := func(recorder *httptest.ResponseRecorder, detail bool) int {
		t.Helper()
		if detail {
			var response struct{ Data model.Channel }
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
			return response.Data.InFlight
		}
		var response struct {
			Data struct{ Items []model.Channel }
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		require.Len(t, response.Data.Items, 1, recorder.Body.String())
		return response.Data.Items[0].InFlight
	}
	list := func() *httptest.ResponseRecorder {
		return serveChannelAdmin(t, GetAllChannels, http.MethodGet, "/api/channel/?p=1&page_size=10", "", nil)
	}
	search := func() *httptest.ResponseRecorder {
		return serveChannelAdmin(t, SearchChannels, http.MethodGet, "/api/channel/search?keyword=busy-channel", "", nil)
	}
	detail := func() *httptest.ResponseRecorder {
		return serveChannelAdmin(t, GetChannel, http.MethodGet, "/api/channel/8201", "", gin.Params{{Key: "id", Value: "8201"}})
	}
	update := func() *httptest.ResponseRecorder {
		return serveChannelAdmin(t, UpdateChannel, http.MethodPut, "/api/channel/", `{"id":8201,"name":"busy-channel","in_flight":99}`, nil)
	}
	assert.Equal(t, 1, inFlight(list(), false))
	assert.Equal(t, 1, inFlight(search(), false))
	assert.Equal(t, 1, inFlight(detail(), true))
	assert.Equal(t, 1, inFlight(update(), true), "update responses keep the live projection, never the client value")

	permit.Release()
	released = true
	assert.Zero(t, inFlight(list(), false))
	assert.Zero(t, inFlight(detail(), true))
}

// E2-5：渠道倍率和并发上限只由模型层校验，非法值在写库前被拒绝。
func TestChannelRoutingFieldsAreRejectedBeforeWrite(t *testing.T) {
	setupChannelManagementContractTest(t)
	add := serveChannelAdmin(t, AddChannel, http.MethodPost, "/api/channel/", `{"mode":"single","channel":{"type":1,"name":"bad-ratio","key":"k","models":"gpt-4o","channel_ratio":"0"}}`, nil)
	assert.Contains(t, add.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "bad-ratio").Count(&count).Error)
	assert.Zero(t, count)

	createContractChannel(t, model.Channel{Id: 8501, Type: constant.ChannelTypeOpenAI, Name: "limits", Key: "k", Models: "gpt-4o", ChannelRatio: "1.5", MaxConcurrency: 2})
	update := serveChannelAdmin(t, UpdateChannel, http.MethodPut, "/api/channel/", `{"id":8501,"max_concurrency":-1}`, nil)
	assert.Contains(t, update.Body.String(), `"success":false`)
	stored, err := model.GetChannelById(8501, false)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.MaxConcurrency)
	assert.Equal(t, "1.5", stored.ChannelRatio)
}
