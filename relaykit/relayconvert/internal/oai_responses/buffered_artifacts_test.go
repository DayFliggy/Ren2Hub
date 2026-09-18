package oairesponses

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedArtifactsIgnoreUnknownIndexedEvents(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	index := 9
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.unknown_telemetry", OutputIndex: &index})
	require.NoError(t, acc.Err())
	assert.Empty(t, acc.BuildOutput())
}

func TestBufferedArtifactsRejectUnidentifiedCitation(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.output_text.annotation.added", Annotation: []byte(`{"type":"url_citation","url":"https://example.test"}`)})
	require.Error(t, acc.Err(), "an annotation with no output identity must not silently disappear")
}

func TestBufferedArtifactsMergePartialTerminalByIdentity(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	frames := []string{
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"search","type":"web_search_call","status":"completed","action":{"type":"search","query":"test"}}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"message","type":"message","role":"assistant"}}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":0,"item_id":"message","delta":"answer"}`,
		`{"type":"response.output_text.annotation.added","output_index":1,"content_index":0,"item_id":"message","annotation":{"type":"url_citation","url":"https://example.com","start_index":0,"end_index":6}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"call","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"test\"}"}}`,
	}
	for _, frame := range frames {
		var event dto.ResponsesStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &event))
		acc.ProcessEvent(&event)
	}
	terminal := &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{{ID: "call", Type: "function_call", Status: "completed"}, {ID: "message", Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "answer"}}}}}
	acc.SupplementResponseOutput(terminal)
	require.NoError(t, acc.Err())
	require.Len(t, terminal.Output, 3)
	assert.Equal(t, []string{"search", "message", "call"}, []string{terminal.Output[0].ID, terminal.Output[1].ID, terminal.Output[2].ID})
	assert.JSONEq(t, `{"type":"search","query":"test"}`, string(terminal.Output[0].Action))
	require.Len(t, terminal.Output[1].Content[0].Annotations, 1)
	assert.Equal(t, "lookup", terminal.Output[2].Name)
	assert.Equal(t, "call_1", terminal.Output[2].CallId)
	assert.Equal(t, `{"q":"test"}`, terminal.Output[2].ArgumentsString())
	acc.SupplementResponseOutput(terminal)
	require.Len(t, terminal.Output, 3)
	require.Len(t, terminal.Output[1].Content[0].Annotations, 1)
}

func TestBufferedArtifactsNeverDropUnindexedHostedItems(t *testing.T) {
	for _, id := range []string{"search", ""} {
		acc := NewResponsesBufferedAccumulator()
		acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.output_item.done", Item: &dto.ResponsesOutput{ID: id, Type: "web_search_call", Status: "completed", Action: []byte(`{"type":"search","query":"test"}`)}})
		if id == "" {
			require.Error(t, acc.Err(), "missing identity must fail explicitly")
			continue
		}
		require.NoError(t, acc.Err())
		output := acc.BuildOutput()
		require.Len(t, output, 1)
		assert.Equal(t, id, output[0].ID)
	}
}

func TestBufferedArtifactsLimitRetainsKnownUsage(t *testing.T) {
	acc := NewResponsesBufferedAccumulator(1000)
	usage := &dto.Usage{InputTokens: 9, OutputTokens: 3, TotalTokens: 12}
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.in_progress", Response: &dto.OpenAIResponsesResponse{Usage: usage}})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: strings.Repeat("x", 1001)})
	require.ErrorContains(t, acc.Err(), "response_conversion_limit")
	response := &dto.OpenAIResponsesResponse{}
	acc.SupplementResponseOutput(response)
	assert.Same(t, usage, response.Usage)
	assert.Empty(t, response.Output)
}

func TestBufferedArtifactsFinalSnapshotDoesNotOverwriteEarlierItem(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	index := 0
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.output_item.done", OutputIndex: &index, Item: &dto.ResponsesOutput{ID: "search", Type: "web_search_call", Status: "completed", Action: []byte(`{"type":"search","query":"test"}`)}})
	response := &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{{ID: "late-message", Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "answer"}}}}}
	acc.SupplementResponseOutput(response)
	require.NoError(t, acc.Err())
	require.Len(t, response.Output, 2)
	assert.Equal(t, "search", response.Output[0].ID)
	assert.Equal(t, "late-message", response.Output[1].ID)
}

func TestBufferedArtifactsReasoningAndAnnotationSnapshotsDoNotLoseFields(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	frames := []string{
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason","delta":"reasoning"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"reason","type":"reasoning","status":"completed"}}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":0,"item_id":"msg","delta":"answer"}`,
		`{"type":"response.output_text.annotation.added","output_index":1,"content_index":0,"item_id":"msg","annotation":{"type":"url_citation","url":"https://example.com"}}`,
		`{"type":"response.content_part.done","output_index":1,"content_index":0,"item_id":"msg","part":{"type":"output_text","text":"answer","annotations":[]}}`,
	}
	for _, frame := range frames {
		var event dto.ResponsesStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &event))
		acc.ProcessEvent(&event)
	}
	response := &dto.OpenAIResponsesResponse{}
	acc.SupplementResponseOutput(response)
	require.NoError(t, acc.Err())
	require.Len(t, response.Output, 2)
	assert.Equal(t, "reasoning", response.Output[0].Summary[0].Text)
	require.Len(t, response.Output[1].Content[0].Annotations, 1)
}

func TestBufferedArtifactsUnresolvedArgumentsFailInsteadOfEmptyItem(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	index := 0
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.function_call_arguments.delta", OutputIndex: &index, Delta: `{}`})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`)}})
	require.ErrorContains(t, acc.Err(), "missing its type")
	assert.Empty(t, acc.BuildOutput())
	require.ErrorContains(t, acc.Err(), "missing its type")
}

func TestBufferedArtifactsKeepEarlierSnapshotItems(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.in_progress", Response: &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{{ID: "search", Type: "web_search_call", Action: []byte(`{"type":"search","query":"test"}`)}}}})
	terminal := &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{{ID: "msg", Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "answer"}}}}}
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.completed", Response: terminal})
	acc.SupplementResponseOutput(terminal)
	require.NoError(t, acc.Err())
	require.Len(t, terminal.Output, 2)
	assert.Equal(t, "search", terminal.Output[0].ID)
	assert.Equal(t, "msg", terminal.Output[1].ID)
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.completed", Response: terminal})
	acc.SupplementResponseOutput(terminal)
	require.Len(t, terminal.Output, 2)
}
