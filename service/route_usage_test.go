package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRouteCacheUsagePresence(t *testing.T) {
	tests := []struct {
		name, protocol      string
		frames              []string
		known               bool
		input, read, output int64
	}{
		{"OpenAI zero", "openai", []string{`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":0}}}`}, true, 100, 0, 20},
		{"omitted", "openai", []string{`{"usage":{"prompt_tokens":100,"completion_tokens":20}}`}, false, 100, 0, 20},
		{"Responses terminal", "openai", []string{`{"response":{"usage":{"input_tokens":60,"output_tokens":12,"input_tokens_details":{"cached_tokens":50}}}}`}, true, 60, 50, 12},
		{"Claude field merge", "claude", []string{`{"message":{"usage":{"input_tokens":30,"cache_read_input_tokens":20,"cache_creation_input_tokens":50}}}`, `{"usage":{"output_tokens":15}}`, `{"usage":{"output_tokens":15}}`}, true, 100, 20, 15},
		{"Gemini inclusive", "gemini", []string{`{"usageMetadata":{"promptTokenCount":80,"toolUsePromptTokenCount":20,"cachedContentTokenCount":30,"candidatesTokenCount":40,"thoughtsTokenCount":10}}`}, true, 100, 30, 50},
		{"invalid count", "openai", []string{`{"usage":{"prompt_tokens":4,"prompt_cache_hit_tokens":8}}`}, false, 4, 8, 0},
		{"conflicting aliases", "openai", []string{`{"usage":{"prompt_tokens":80,"prompt_tokens_details":{"cached_tokens":5},"prompt_cache_hit_tokens":9}}`}, false, 80, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a routeAttempt
			for _, frame := range tc.frames {
				a.observeUsage(tc.protocol, []byte(frame))
			}
			ev := a.evidence(time.Now(), "success", false)
			require.Equal(t, tc.known, ev.CacheKnown)
			require.Equal(t, tc.input, ev.InputTokens)
			require.Equal(t, tc.read, ev.CacheReadTokens)
			require.Equal(t, tc.output, ev.OutputTokens)
			ev = a.evidence(time.Now(), "failure", true)
			require.False(t, ev.CacheKnown)
		})
	}
}

func TestRouteSpeedRequiresRealCompleteStream(t *testing.T) {
	ev := RouteAttemptEvidence{Stream: true, Outcome: "success", OutputSource: "upstream", OutputTokens: 50, GenerationMS: 500}
	tokens, elapsed := ev.speedSample()
	require.EqualValues(t, 50, tokens)
	require.Equal(t, 500*time.Millisecond, elapsed)
	for _, change := range []func(*RouteAttemptEvidence){
		func(e *RouteAttemptEvidence) { e.Stream = false },
		func(e *RouteAttemptEvidence) { e.Outcome = "failure" },
		func(e *RouteAttemptEvidence) { e.CacheReason = "incomplete" },
		func(e *RouteAttemptEvidence) { e.GenerationMS = 499 },
		func(e *RouteAttemptEvidence) { e.OutputTokens = 49 },
		func(e *RouteAttemptEvidence) { e.OutputSource = "estimated" },
	} {
		excluded := ev
		change(&excluded)
		tokens, elapsed = excluded.speedSample()
		require.Zero(t, tokens)
		require.Zero(t, elapsed)
	}
}
