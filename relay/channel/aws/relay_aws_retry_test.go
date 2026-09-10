package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type awsRetryTestTransport func(*http.Request) (*http.Response, error)

func (transport awsRetryTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestAWSAdapterDoesNotRetryGenerationInsideSDK(t *testing.T) {
	service.InitHttpClient()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "fixture-token|us-east-1"}}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	require.NoError(t, err)
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	var calls atomic.Int32
	client.Transport = awsRetryTestTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Amzn-Errortype": {"ServiceUnavailableException"}},
			Body:       io.NopCloser(strings.NewReader(`{"message":"fixture failure"}`)), Request: request,
		}, nil
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	awsClient, err := newAwsClient(c, info)
	require.NoError(t, err)
	modelID := "anthropic.claude-test"
	_, err = awsClient.InvokeModel(context.Background(), &bedrockruntime.InvokeModelInput{ModelId: &modelID, Body: []byte(`{}`)})
	require.Error(t, err)
	require.EqualValues(t, 1, calls.Load())
}
