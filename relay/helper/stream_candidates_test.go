package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamScannerRequiresAllCandidateTerminals(t *testing.T) {
	for _, test := range []struct {
		name, body string
		success    bool
	}{
		{"partial second choice EOF", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"},\"finish_reason\":\"stop\"},{\"index\":1,\"delta\":{\"content\":\"partial\"}}]}\n\n", false},
		{"partial second choice DONE", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"},{\"index\":1,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]\n\n", false},
		{"choices finish in separate frames", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"},{\"index\":1,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"choices\":[{\"index\":1,\"finish_reason\":\"length\"}]}\n\n", true},
		{"new candidate after first terminal", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"partial\"}}]}\n\n", false},
		{"single choice explicit terminal", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n", true},
		{"implicit and explicit zero index", "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\n", true},
		{"unknown finish is not success", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"new_provider_reason\"}]}\n\n", false},
		{"partial second Gemini candidate", "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\"},{\"index\":1,\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n", false},
		{"noncandidate explicit DONE", "data: {\"provider_message\":\"answer\"}\n\ndata: [DONE]\n\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, resp, info := setupStreamTest(t, strings.NewReader(test.body))
			StreamScannerHandler(c, resp, info, func(data string, result *StreamResult) { require.NoError(t, StringData(c, data)) })
			assert.Equal(t, test.success, info.StreamStatus.IsNormalEnd())
			if test.success {
				assert.Nil(t, StreamError(info))
			} else {
				require.NotNil(t, StreamError(info))
			}
		})
	}
}
