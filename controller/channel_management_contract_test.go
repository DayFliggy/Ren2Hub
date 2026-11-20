package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
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

// B4：三条原生删除路径都清理 Key 路由条目和并发资源表项，且清扫之前遗留的孤立条目。
func TestChannelDeletionReleasesRouteEntriesAndConcurrency(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		handler gin.HandlerFunc
		method  string
		target  string
		body    string
		params  gin.Params
	}{
		{name: "single", status: common.ChannelStatusEnabled, handler: DeleteChannel, method: http.MethodDelete, target: "/api/channel/8301", params: gin.Params{{Key: "id", Value: "8301"}}},
		{name: "batch", status: common.ChannelStatusEnabled, handler: DeleteChannelBatch, method: http.MethodPost, target: "/api/channel/batch", body: `{"ids":[8301]}`},
		{name: "disabled", status: common.ChannelStatusManuallyDisabled, handler: DeleteDisabledChannel, method: http.MethodDelete, target: "/api/channel/disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupChannelManagementContractTest(t)
			createContractChannel(t, model.Channel{Id: 8301, Type: constant.ChannelTypeOpenAI, Name: "deleted", Key: "k", Models: "gpt-4o", Status: tc.status})
			createContractChannel(t, model.Channel{Id: 8302, Type: constant.ChannelTypeOpenAI, Name: "kept", Key: "k", Models: "gpt-4o"})
			token := &model.Token{UserId: 7, Key: "route-cleanup-" + tc.name, Name: "route"}
			require.NoError(t, token.Insert())
			require.NoError(t, model.DB.Create(&model.TokenRouteProfile{TokenID: token.Id, Mode: model.TokenRouteOrdered, SelectedOnly: true, Version: 1}).Error)
			for position, channelID := range []int{8301, 8302, 8399} {
				require.NoError(t, model.DB.Create(&model.TokenRouteEntry{TokenID: token.Id, ChannelID: channelID, Position: position, Enabled: true}).Error)
			}
			deleted, err := model.GetChannelById(8301, false)
			require.NoError(t, err)
			permit, err := service.AcquireChannelConcurrency(context.Background(), deleted)
			require.NoError(t, err)
			permit.Release()
			require.True(t, channelConcurrencyTracked(8301))

			recorder := serveChannelAdmin(t, tc.handler, tc.method, tc.target, tc.body, tc.params)
			require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())

			view, err := model.ReadTokenRouting(context.Background(), token.Id)
			require.NoError(t, err)
			require.Len(t, view.Entries, 1, "entries of deleted channels, including older leftovers, must be removed")
			assert.Equal(t, 8302, view.Entries[0].ChannelID)
			assert.False(t, channelConcurrencyTracked(8301))
		})
	}
}

func channelConcurrencyTracked(id int) bool {
	for _, row := range service.RoutingSnapshot().Channels {
		if row.ID == id {
			return true
		}
	}
	return false
}

// B10：优先级已经提交后，路由发布失败只记日志，不能把写入报告为失败。
func TestReorderChannelPriorityReportsCommittedWriteWhenPublicationFails(t *testing.T) {
	setupChannelManagementContractTest(t)
	for id, priority := range map[int]int64{8401: 100, 8402: 80, 8403: 0} {
		value := priority
		createContractChannel(t, model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Name: fmt.Sprint(id), Key: "k", Models: "gpt-4o", Priority: &value})
	}
	previous := model.ChannelRoutingChanged
	published := 0
	model.ChannelRoutingChanged = func(int) error { published++; return errors.New("forced publication failure") }
	t.Cleanup(func() { model.ChannelRoutingChanged = previous })
	order, err := model.GetChannelPriorityOrder()
	require.NoError(t, err)

	body := fmt.Sprintf(`{"revision":%q,"id":8403,"target_id":8402,"position":"before"}`, order.Revision)
	recorder := serveChannelAdmin(t, ReorderChannelPriority, http.MethodPost, "/api/channel/priority/reorder", body, nil)

	require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())
	assert.Positive(t, published)
	stored, err := model.GetChannelById(8403, false)
	require.NoError(t, err)
	assert.Equal(t, int64(90), stored.GetPriority())
}

func TestChannelStatusReportsCommittedChangeWhenPublicationFails(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache_%v", cached), func(t *testing.T) {
			setupChannelManagementContractTest(t)
			createContractChannel(t, model.Channel{Id: 8601, Type: constant.ChannelTypeOpenAI, Name: "status-publication", Key: "k", Models: "gpt-4o"})
			common.MemoryCacheEnabled = cached
			model.InitChannelCache()
			previous := model.ChannelRoutingChanged
			model.ChannelRoutingChanged = func(int) error { return errors.New("forced publication failure") }
			t.Cleanup(func() { model.ChannelRoutingChanged = previous })
			closed := false
			unregister := wsmanager.Register(8601, wsmanager.KindResponses, func(reason string) {
				closed = true
				assert.Equal(t, service.ChannelDisabledCloseReason, reason)
			})
			t.Cleanup(unregister)
			recorder := serveChannelAdmin(t, UpdateChannelStatus, http.MethodPut, "/api/channel/8601/status", `{"status":2}`, gin.Params{{Key: "id", Value: "8601"}})
			var response struct {
				Success            bool `json:"success"`
				Data               bool `json:"data"`
				PublicationPending bool `json:"publication_pending"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
			assert.True(t, response.Success, recorder.Body.String())
			assert.True(t, response.Data, "the database change committed")
			assert.True(t, response.PublicationPending)
			assert.True(t, closed, "committed disable must close existing connections despite publication failure")
			stored, err := model.GetChannelById(8601, false)
			require.NoError(t, err)
			assert.Equal(t, 2, stored.Status)
			cachedChannel, err := model.CacheGetChannel(8601)
			require.NoError(t, err)
			assert.Equal(t, 2, cachedChannel.Status)
			var audit model.AuditLog
			require.NoError(t, model.LOG_DB.Where("action = ?", "channel.status_update").First(&audit).Error)
			require.True(t, audit.Success)
			encoded, err := common.Marshal(audit.Other)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"changed":true`)
			assert.Contains(t, string(encoded), `"publication_pending":true`)
		})
	}
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

func TestDeletedSelectedChannelsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skipf("%s not configured", dialect.env)
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
			createContractChannel(t, model.Channel{Id: 8302, Type: constant.ChannelTypeOpenAI, Key: "other", Models: "route-model"})
			for _, tc := range []struct {
				name               string
				handler            gin.HandlerFunc
				method, path, body string
				params             gin.Params
				status             int
			}{
				{"single", DeleteChannel, http.MethodDelete, "/api/channel/8301", "", gin.Params{{Key: "id", Value: "8301"}}, 1},
				{"batch", DeleteChannelBatch, http.MethodPost, "/api/channel/batch", `{"ids":[8301]}`, nil, 1},
				{"disabled", DeleteDisabledChannel, http.MethodDelete, "/api/channel/disabled", "", nil, common.ChannelStatusManuallyDisabled},
			} {
				t.Run(tc.name, func(t *testing.T) {
					createContractChannel(t, model.Channel{Id: 8301, Type: constant.ChannelTypeOpenAI, Key: "selected", Models: "route-model", Status: tc.status})
					tokens := map[string]*model.Token{}
					for _, mode := range []string{"auto", "ordered"} {
						token := &model.Token{UserId: 7, Key: tc.name + "-" + mode}
						require.NoError(t, token.Insert())
						var profile model.TokenRouteProfile
						require.NoError(t, common.Unmarshal([]byte(fmt.Sprintf(`{"token_id":%d,"mode":%q,"selected_only":true,"version":1}`, token.Id, mode)), &profile))
						require.NoError(t, db.Create(&profile).Error)
						require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: token.Id, ChannelID: 8301, Enabled: true}).Error)
						tokens[mode] = token
					}
					response := serveChannelAdmin(t, tc.handler, tc.method, tc.path, tc.body, tc.params)
					require.Contains(t, response.Body.String(), `"success":true`)
					for mode, token := range tokens {
						t.Run(mode, func(t *testing.T) {
							view, err := model.ReadTokenRouting(t.Context(), token.Id)
							require.NoError(t, err)
							require.Empty(t, view.Entries)
							require.Equal(t, mode, view.Mode)
							c, _ := newAuthenticatedContext(t, http.MethodPost, "/v1/chat/completions", nil, 7)
							common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
							channel, selErr := service.SelectChannelForRequest(c, "route-model", nil)
							defer service.ReleaseRouteChannel(c)
							require.Nil(t, channel)
							require.NotNil(t, selErr)
							require.Equal(t, 503, selErr.StatusCode)
							require.EqualValues(t, "model_not_found", selErr.Code)
						})
					}
				})
			}
		})
	}
}
