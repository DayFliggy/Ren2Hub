package openai

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestAudioUsageRequiresExplicitDirectionalCounts(t *testing.T) {
	for _, raw := range []string{`{}`, `{"type":"tokens"}`, `{"input_tokens":2}`, `{"input_tokens":null,"output_tokens":0}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := decodeAudioUsage(json.RawMessage(raw), true)
			require.Error(t, err)
		})
	}
	usage, err := decodeAudioUsage(json.RawMessage(`{"input_tokens":0,"output_tokens":0}`), true)
	require.NoError(t, err)
	require.Equal(t, "provider", usage.UsageSource)
}

func TestRealtimePendingGenerationCannotFinalizePreviousUsage(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"first","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`)))
	readRealtimeMessage(t, client)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
	readRealtimeMessage(t, target)
	require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.usage)
	require.False(t, result.usage.Observation.Complete, "the next generation was sent but its outcome is unknown")
	require.Equal(t, 30, result.usage.TotalTokens, "retain the completed response as recovery evidence")
}

func TestRealtimeEmptyUsageCannotConfirmZeroCharge(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"empty","status":"completed","usage":{}}}`)))
	readRealtimeMessage(t, client)
	require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.usage)
	require.False(t, result.usage.Observation.Complete)
}

func TestRealtimeConflictingDuplicateUsageRemainsUnknown(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"same","status":"completed","usage":{"input_tokens":1,"output_tokens":2}}}`)))
	readRealtimeMessage(t, client)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"same","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.err)
	require.False(t, result.usage.Observation.Complete)
}
