package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestGatewayOfficialSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("GATEWAY_SDK_PYTHON and GATEWAY_SDK_NODE must point to installed SDK runtimes")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify.py"}, {node, "verify.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			url, key, foreignKey := setupGatewaySDK(t)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+url, "GATEWAY_SDK_KEY="+key, "GATEWAY_SDK_FOREIGN_KEY="+foreignKey)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
		})
	}
}

func setupGatewaySDK(t *testing.T) (string, string, string) {
	t.Helper()
	return setupGatewaySDKWithUpstream(t, gatewaySDKUpstream)
}

func setupGatewaySDKWithUpstream(t *testing.T, handler http.HandlerFunc) (string, string, string) {
	t.Helper()
	setupRelayRouterTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.UserRouteGroup{}, &model.UserRouteEntry{}, &model.RoutePolicy{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{}, &model.BillingRecovery{}, &model.BillingRecoveryAdjustment{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.Log{}, &model.QuotaData{}))
	oldRedis, oldMemory := common.RDB, common.MemoryCacheEnabled
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	common.RDB, common.RedisEnabled, common.MemoryCacheEnabled = client, true, false
	oldPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		common.RDB, common.MemoryCacheEnabled = oldRedis, oldMemory
		_ = client.Close()
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-4o-mini":0.001,"claude-3-5-sonnet":0.001,"gemini-2.5-flash":0.001,"llama3.1":0.001}`))
	service.InitHttpClient()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	user := &model.User{Username: "sdk-isolated", Status: common.UserStatusEnabled, Quota: 10000000, Group: "default"}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "sdkisolatedtestonly", Name: "SDK", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, ConversionPolicy: "safe"}
	require.NoError(t, db.Create(token).Error)
	foreignToken := &model.Token{UserId: user.Id, Key: "sdkisolatedothertoken", Name: "other SDK token", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, ConversionPolicy: "safe"}
	require.NoError(t, db.Create(foreignToken).Error)
	for _, channel := range []model.Channel{
		{Type: constant.ChannelTypeOpenAI, Models: "gpt-4o-mini"},
		{Type: constant.ChannelTypeAnthropic, Models: "claude-3-5-sonnet"},
		{Type: constant.ChannelTypeGemini, Models: "gemini-2.5-flash"},
	} {
		channel.Name, channel.Key, channel.Status, channel.BaseURL = "SDK fixture", "local-test-key", common.ChannelStatusEnabled, &upstream.URL
		channel.Group, channel.CapacityTotal, channel.ChannelRatio = "default", 10, 1
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, channel.AddAbilities(db))
	}
	fallbackPriority := int64(-1)
	fallback := &model.Channel{Type: constant.ChannelTypeOpenAI, Models: "gpt-4o-mini", Name: "SDK fallback", Key: "local-fallback-key", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL, Group: "default", CapacityTotal: 10, ChannelRatio: 1, Priority: &fallbackPriority}
	require.NoError(t, db.Create(fallback).Error)
	require.NoError(t, fallback.AddAbilities(db))
	ollama := &model.Channel{Type: constant.ChannelTypeOllama, Models: "llama3.1", Name: "SDK Ollama", Key: "local-ollama-key", Status: common.ChannelStatusEnabled,
		BaseURL: &upstream.URL, Group: "default", CapacityTotal: 10, ChannelRatio: 1}
	require.NoError(t, db.Create(ollama).Error)
	require.NoError(t, ollama.AddAbilities(db))
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	return gateway.URL, token.Key, foreignToken.Key
}

func gatewaySDKUpstream(w http.ResponseWriter, r *http.Request) {
	var request map[string]any
	if err := common.DecodeJson(r.Body, &request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if request["input"] == "continue" && request["previous_response_id"] != "resp_fixture" {
		http.Error(w, "native continuation reference was not preserved", http.StatusBadRequest)
		return
	}
	stream, _ := request["stream"].(bool)
	stream = stream || strings.Contains(r.URL.Path, "streamGenerateContent")
	modelName, _ := request["model"].(string)
	encodedRequest, _ := common.Marshal(request)
	if strings.Contains(string(encodedRequest), "retry_prelude") && r.Header.Get("Authorization") == "Bearer local-test-key" {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 2 {
			_, _ = fmt.Fprint(w, "data: {\"id\":\"prelude\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
		}
		return
	}
	interrupt := strings.Contains(string(encodedRequest), "interrupt")
	if strings.Contains(string(encodedRequest), "use_tool") && !strings.Contains(string(encodedRequest), "tool_result") && !strings.Contains(string(encodedRequest), "function_call_output") && !strings.Contains(string(encodedRequest), `"role":"tool"`) && !strings.Contains(string(encodedRequest), "functionResponse") {
		gatewaySDKToolResponse(w, r, modelName, stream)
		return
	}
	writeJSON := func(value any) { data, _ := common.Marshal(value); _, _ = w.Write(data) }
	writeEvent := func(value any) {
		data, _ := common.Marshal(value)
		if interrupt {
			var event map[string]any
			_ = common.Unmarshal(data, &event)
			if event["type"] == "message_delta" || event["type"] == "message_stop" || event["type"] == "response.completed" {
				return
			}
			if choices, ok := event["choices"].([]any); ok {
				for _, choice := range choices {
					if choice.(map[string]any)["finish_reason"] != nil {
						return
					}
				}
			}
			if candidates, ok := event["candidates"].([]any); ok {
				for _, candidate := range candidates {
					delete(candidate.(map[string]any), "finishReason")
				}
			}
			data, _ = common.Marshal(event)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	w.Header().Set("Content-Type", "application/json")
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	switch {
	case r.URL.Path == "/api/chat":
		message := map[string]any{"role": "assistant", "content": "hello"}
		if !stream {
			writeJSON(map[string]any{"model": modelName, "message": message, "done": true, "done_reason": "stop", "prompt_eval_count": 10, "eval_count": 2})
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		writeJSON(map[string]any{"model": modelName, "message": message, "done": false})
		_, _ = fmt.Fprint(w, "\n")
		if !interrupt {
			writeJSON(map[string]any{"model": modelName, "done": true, "done_reason": "stop", "prompt_eval_count": 10, "eval_count": 2})
			_, _ = fmt.Fprint(w, "\n")
		}
	case strings.Contains(r.URL.Path, "generateContent") || strings.Contains(r.URL.Path, "streamGenerateContent"):
		response := map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "hello"}}}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 2, "totalTokenCount": 12}}
		if stream {
			writeEvent(response)
		} else {
			writeJSON(response)
		}
	case strings.Contains(r.URL.Path, "messages"):
		usage := map[string]any{"input_tokens": 10, "output_tokens": 2}
		response := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": modelName, "content": []any{map[string]any{"type": "text", "text": "hello"}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": usage}
		if !stream {
			writeJSON(response)
			return
		}
		start := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": modelName, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 0}}
		for _, event := range []map[string]any{
			{"type": "message_start", "message": start}, {"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "hello"}}, {"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": usage}, {"type": "message_stop"},
		} {
			_, _ = fmt.Fprintf(w, "event: %s\n", event["type"])
			writeEvent(event)
		}
	case strings.Contains(r.URL.Path, "responses"):
		part := map[string]any{"type": "output_text", "text": "hello", "annotations": []any{}}
		item := map[string]any{"id": "msg_response_fixture", "type": "message", "status": "completed", "role": "assistant", "content": []any{part}}
		response := map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1, "status": "completed", "error": nil, "incomplete_details": nil, "model": modelName, "output": []any{item}, "parallel_tool_calls": true, "tools": []any{}, "tool_choice": "auto", "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
		if !stream {
			writeJSON(response)
			return
		}
		start := map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1, "status": "in_progress", "model": modelName, "output": []any{}}
		for sequence, event := range []map[string]any{
			{"type": "response.created", "response": start},
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "msg_response_fixture", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
			{"type": "response.content_part.added", "item_id": "msg_response_fixture", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
			{"type": "response.output_text.delta", "item_id": "msg_response_fixture", "output_index": 0, "content_index": 0, "delta": "hello"},
			{"type": "response.output_text.done", "item_id": "msg_response_fixture", "output_index": 0, "content_index": 0, "text": "hello"},
			{"type": "response.content_part.done", "item_id": "msg_response_fixture", "output_index": 0, "content_index": 0, "part": part},
			{"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response},
		} {
			event["sequence_number"] = sequence
			_, _ = fmt.Fprintf(w, "event: %s\n", event["type"])
			writeEvent(event)
		}
	default:
		usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
		if !stream {
			writeJSON(map[string]any{"id": "chat_fixture", "object": "chat.completion", "created": 1, "model": modelName, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}}, "usage": usage})
			return
		}
		for _, delta := range []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}} {
			writeEvent(map[string]any{"id": "chat_fixture", "object": "chat.completion.chunk", "created": 1, "model": modelName, "choices": []any{delta}, "usage": usage})
		}
		if !interrupt {
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}
}
