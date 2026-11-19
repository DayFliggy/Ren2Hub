package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamOutputExcludesControlAndUsageFrames(t *testing.T) {
	for _, data := range []string{
		`invalid`, `[DONE]`, `{"type":"response.created","response":{"output":[]}}`,
		`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[],"usage":{"completion_tokens":12}}`,
		`{"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":10}}}`,
		`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
		`{"type":"ping"}`, `{"type":"response.completed","response":{"output":[]}}`,
		`{"error":{"message":"failed"},"choices":[{"delta":{"content":"ignored"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_metadata"}]}}]}`,
		`{"type":"session.created"}`, `{"type":"response.output_text.delta","delta":""}`,
		// Empty arrays and objects with whitespace are still empty.
		`{"type":"response.output_item.done","item":{"type":"web_search_call","action":{ },"results":[ ]}}`,
		`{"type":"content_block_start","content_block":{"type":"web_search_tool_result","content":[ ` + "\n" + ` ]}}`,
	} {
		require.False(t, streamEventHasOutput(data), data)
	}
	for _, data := range []string{
		`{"choices":[{"delta":{"content":"hello"}}],"error":null}`,
		`{"choices":[{"text":"hello"}]}`,
		`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"search"}}]}}]}`,
		`{"choices":[{"delta":{"function_call":{"arguments":"{}"}}}]}`,
		`{"type":"response.output_text.delta","delta":"answer"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
		`{"type":"response.function_call_arguments.delta","delta":"{}"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","name":"search"}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}}`,
		`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thinking"}}`,
		`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"content_block_start","content_block":{"type":"tool_use","name":"search"}}`,
		`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search"}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"audio"}}]}}]}`,
		`{"type":"response.audio.delta","delta":"audio"}`,
		`{"type":"response.text.delta","delta":"realtime answer"}`,
	} {
		require.True(t, streamEventHasOutput(data), data)
	}
}

func TestRouteLatencyWaitsForEffectiveOutputAndResetsPerAttempt(t *testing.T) {
	setupRouteTest(t)
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 912)
	info := &relaycommon.RelayInfo{OriginModelName: "delayed-model", RelayMode: 1, IsStream: true, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	value, _ := c.Get("route_attempt")
	attempt := value.(*routeAttempt)
	attempt.started = time.Now().Add(-15 * time.Second)
	info.SetFirstResponseTime()
	ObserveRouteStreamOutput(c, `{"choices":[{"delta":{"role":"assistant"}}]}`)
	require.Zero(t, attempt.firstOutput.Load())
	ObserveRouteStreamOutput(c, `{"type":"response.created","response":{"output":[]}}`)
	require.Zero(t, attempt.firstOutput.Load())
	ObserveRouteStreamOutput(c, `{"type":"response.output_text.delta","delta":"answer"}`)
	first := attempt.firstOutput.Load()
	require.Positive(t, first)
	ObserveRouteStreamOutput(c, `{"type":"response.output_text.delta","delta":"later"}`)
	require.Equal(t, first, attempt.firstOutput.Load())
	FinishRouteAttempt(c, info, nil)
	row := routeObservations.get(912, "delayed-model", 1, true)
	require.Equal(t, 1, row.LatencySamples)
	require.GreaterOrEqual(t, row.LatencyMS, float64(15_000))

	MarkRouteAttemptSent(c, info)
	value, _ = c.Get("route_attempt")
	require.Zero(t, value.(*routeAttempt).firstOutput.Load())
	ObserveRouteStreamOutput(c, `{"type":"response.created"}`)
	FinishRouteAttempt(c, info, nil)
	row = routeObservations.get(912, "delayed-model", 1, true)
	// A control-only stream without a completion is ignored (no_visible_output):
	// it writes neither a sample nor an invented latency.
	require.Equal(t, 1, row.Samples)
	require.Equal(t, 1, row.LatencySamples)
}
