package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSEnvelopeSeparatesControlsAndPreservesZero(t *testing.T) {
	body := []byte(`{"type":"response.create","stream_id":"one","generate":false,"response":{"model":"test","temperature":0,"top_p":0,"stream_id":"shadow","background":true,"stream":true,"stream_options":{"include_usage":true}}}`)
	event, id, err := parseResponsesWSEnvelope(body)
	require.NoError(t, err)
	create, err := normalizeResponsesWSCreateEvent(body, event, id)
	require.NoError(t, err)
	require.NotNil(t, create.Request.Temperature)
	assert.Zero(t, *create.Request.Temperature)
	assert.NotContains(t, string(create.Body), "stream_id")
	assert.NotContains(t, string(create.Body), "background")
	assert.NotContains(t, string(create.Body), "stream")
	payload, err := buildResponsesWSCreateEvent(create.Body, create.Generate, id)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, common.Unmarshal(payload, &wire))
	assert.Equal(t, "one", wire["stream_id"])
	assert.Equal(t, false, wire["generate"])
	for _, raw := range []string{`{"type":"response.create","stream_id":42}`, `{"type":"response.create","stream_id":"x/y"}`, `{"type":"response.create","stream_id":""}`} {
		_, _, err := parseResponsesWSEnvelope([]byte(raw))
		require.Error(t, err)
	}
	event.Generate = []byte(`"false"`)
	_, err = normalizeResponsesWSCreateEvent(body, event, id)
	require.ErrorContains(t, err, "boolean")
}

func TestResponsesWSCancelErrorDoesNotEndGeneration(t *testing.T) {
	control := []byte(`{"type":"response.cancel","event_id":"cancel-one","stream_id":"stream-one","response_id":"resp-one"}`)
	event := responsesWSErrorEvent{Type: "error", EventID: "cancel-one", StreamID: "stream-one", Error: &types.OpenAIError{Type: "invalid_request_error"}}
	terminal, ambiguous, controlError := responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.False(t, terminal)
	assert.False(t, ambiguous)
	assert.True(t, controlError)
	event.EventID = ""
	event.StreamID = "other"
	terminal, _, _ = responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.False(t, terminal)
	event.StreamID = "stream-one"
	terminal, ambiguous, _ = responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.True(t, terminal)
	assert.True(t, ambiguous)
}
