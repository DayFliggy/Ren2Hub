package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayAWSClientNeverRetriesInternally(t *testing.T) {
	for _, key := range []string{"test-token|us-east-1", "test-ak|test-sk|us-east-1"} {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: key}}
		client, err := newAwsClient(nil, info)
		require.NoError(t, err)
		options := client.Options()
		attempts := 0
		options.HTTPClient = awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			assert.NotEmpty(t, request.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"unavailable"}`)), Request: request}, nil
		})
		_, err = bedrockruntime.New(options).InvokeModel(context.Background(), newAwsInvokeModelInput())
		require.Error(t, err)
		assert.Equal(t, 1, attempts, "one gateway attempt must perform at most one SDK send")
	}
}

func TestNovaRequestPreservesExplicitZeroAndMissing(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{`{"messages":[],"temperature":0,"top_p":0,"top_k":0,"max_completion_tokens":100}`, `{"maxTokens":100,"temperature":0,"topP":0,"topK":0}`},
		{`{"messages":[]}`, `null`},
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.Unmarshal([]byte(tc.input), &req))
		body, err := common.Marshal(convertToNovaRequest(&req).InferenceConfig)
		require.NoError(t, err)
		assert.JSONEq(t, tc.expected, string(body))
	}
}

func TestNovaSystemAndTextPartsPreserveConversation(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "nova-lite", Messages: []dto.Message{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: []dto.MediaContent{{Type: "text", Text: "first"}, {Type: "text", Text: "second"}}},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "next"},
	}}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, request)
	require.NoError(t, err)
	result := converted.(*NovaRequest)
	assert.Equal(t, []NovaContent{{Text: "instructions"}}, result.System)
	assert.Equal(t, []NovaMessage{
		{Role: "user", Content: []NovaContent{{Text: "first"}, {Text: "second"}}},
		{Role: "assistant", Content: []NovaContent{{Text: "answer"}}},
		{Role: "user", Content: []NovaContent{{Text: "next"}}},
	}, result.Messages)
}

func TestNovaResponseRejectsMissingOutputAndNegativeUsage(t *testing.T) {
	for _, body := range []string{`{"output":{"message":{"content":[]}}}`, `{"output":{"message":{"content":[{"text":"hello"}]}},"usage":{"inputTokens":-1}}`} {
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		info := newAwsTestRelayInfo()
		info.IsStream = false
		adapter := &Adaptor{AwsReq: newAwsInvokeModelInput(), AwsClient: newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}))}
		err, usage := handleNovaRequest(c, info, adapter)
		require.NotNil(t, err)
		assert.Equal(t, http.StatusBadGateway, err.StatusCode)
		assert.Nil(t, usage)
		assert.False(t, c.Writer.Written())
	}
}
