package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrontendContractModelCatalogHealth generates the canonical wire fixture
// consumed by frontend parser and visual tests for user-facing channel health
// projections on GET /api/user/model-catalog.
func TestFrontendContractModelCatalogHealth(t *testing.T) {
	initModelListColumnNames(t)
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}, &model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 17, Username: "catalog-health-user", AffCode: "CHU17"}).Error)

	require.NoError(t, model.DB.Create(&model.Option{Key: "ModelRatio", Value: `{"gpt-4o":1.25,"claude-sonnet-4-5":1.5}`}).Error)
	require.NoError(t, model.DB.Create(&model.Option{Key: "CompletionRatio", Value: `{"gpt-4o":4,"claude-sonnet-4-5":5}`}).Error)

	secretTarget := "internal-mapped-gpt-4o-target"
	mapping := `{"gpt-4o":"` + secretTarget + `"}`
	ch1 := model.Channel{
		Id: 11, Type: constant.ChannelTypeOpenAI, Key: "sk-contract-secret-11", Name: "Primary Pool",
		Status: common.ChannelStatusEnabled, Models: "gpt-4o,claude-sonnet-4-5", ModelMapping: &mapping,
		ChannelRatio: "1", MaxConcurrency: 10,
	}
	ch2 := model.Channel{
		Id: 22, Type: constant.ChannelTypeAnthropic, Key: "sk-contract-secret-22", Name: "Backup Pool",
		Status: common.ChannelStatusEnabled, Models: "claude-sonnet-4-5",
		ChannelRatio: "1.5", MaxConcurrency: 5,
	}
	require.NoError(t, model.DB.Create(&ch1).Error)
	require.NoError(t, model.DB.Create(&ch2).Error)
	require.NoError(t, ch1.AddAbilities(nil))
	require.NoError(t, ch2.AddAbilities(nil))
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		service.ReleaseDeletedChannels(context.Background(), []int{11, 22})
		model.InvalidatePricingCache()
	})
	service.ReleaseDeletedChannels(context.Background(), []int{11, 22})

	// Drive real production route-attempt lifecycle:
	// 1) Open breaker on channel 11 mapped gpt-4o -> paused (claude-sonnet-4-5 stays unrated).
	for range 5 {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		service.BeginRouteObservation(ctx)
		common.SetContextKey(ctx, constant.ContextKeyChannelId, 11)
		ctx.Set("model_mapping", mapping)
		info := &relaycommon.RelayInfo{
			RelayMode:       relayconstant.RelayModeChatCompletions,
			OriginModelName: "gpt-4o",
			StartTime:       time.Now(),
		}
		service.MarkRouteAttemptSent(ctx, info)
		apiErr := types.NewOpenAIError(errors.New("secret upstream failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
		service.FinishRouteAttempt(ctx, info, apiErr)
	}

	// 2) Trigger 429 cooldown on channel 22 -> limited.
	{
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		service.BeginRouteObservation(ctx)
		common.SetContextKey(ctx, constant.ContextKeyChannelId, 22)
		info := &relaycommon.RelayInfo{
			RelayMode:       relayconstant.RelayModeChatCompletions,
			OriginModelName: "claude-sonnet-4-5",
			StartTime:       time.Now(),
		}
		service.MarkRouteAttemptSent(ctx, info)
		resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"30"}}}
		service.ObserveRouteResponse(ctx, resp)
		apiErr := types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
		service.FinishRouteAttempt(ctx, info, apiErr)
	}

	engine := contractEngine(17, common.RoleCommonUser)
	engine.GET("/api/user/model-catalog", GetModelCatalog)
	w := contractRequest(t, engine, http.MethodGet, "/api/user/model-catalog", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	raw := w.Body.String()
	for _, forbidden := range []string{secretTarget, "sk-contract-secret", "secret upstream failure", "in_flight", "probe_in_flight", "open_until", "last_error_code", "retry_after", `"trigger"`} {
		require.NotContains(t, raw, forbidden)
	}

	body := decodeContractBody(t, w)
	require.Equal(t, true, body["success"])
	data := body["data"].(map[string]any)
	assert.Greater(t, data["generated_at"].(float64), float64(0))
	data["generated_at"] = 1792281600
	data["server_address"] = "https://api.example.test"
	for _, entry := range data["models"].([]any) {
		delete(entry.(map[string]any), "pricing_version")
	}

	frontendFixture(t, "model-catalog-health.get.json", body)
}

func TestGetModelCatalogReturns503CatalogBusy(t *testing.T) {
	prev := buildModelCatalog
	buildModelCatalog = func(context.Context, int, int) (service.ModelCatalog, error) {
		return service.ModelCatalog{}, service.ErrCatalogConfigChanging
	}
	t.Cleanup(func() { buildModelCatalog = prev })

	engine := contractEngine(17, common.RoleCommonUser)
	engine.GET("/api/user/model-catalog", GetModelCatalog)
	w := contractRequest(t, engine, http.MethodGet, "/api/user/model-catalog", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	body := decodeContractBody(t, w)
	require.Equal(t, false, body["success"])
	require.Equal(t, "CATALOG_BUSY", body["code"])
}
