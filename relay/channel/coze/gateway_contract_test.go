package coze

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCozePreservesAssistantHistoryAndTypedText(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("bot_id", "test-bot")
	request := &dto.GeneralOpenAIRequest{Model: "coze-test", Messages: []dto.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: []dto.MediaContent{{Type: "text", Text: "next"}, {Type: "text", Text: " question"}}},
	}}
	value, err := (&Adaptor{}).ConvertOpenAIRequest(c, nil, request)
	require.NoError(t, err)
	result := value.(*CozeChatRequest)
	assert.Equal(t, "test-bot", result.BotId)
	assert.Equal(t, []CozeEnterMessage{
		{Role: "user", Type: "question", Content: "first", ContentType: "text"},
		{Role: "assistant", Type: "answer", Content: "answer", ContentType: "text"},
		{Role: "user", Type: "question", Content: "next question", ContentType: "text"},
	}, result.AdditionalMessages)
	var userID string
	require.NoError(t, common.Unmarshal(result.UserId, &userID))
	assert.NotEmpty(t, userID)
	request.User = []byte(`{"invalid":"user"}`)
	_, err = (&Adaptor{}).ConvertOpenAIRequest(c, nil, request)
	require.ErrorContains(t, err, "non-empty string")
}

func TestCozeCancellationStopsAcceptedChatPolling(t *testing.T) {
	service.InitHttpClient()
	pollStarted := make(chan struct{})
	pollCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v3/chat" {
			_, _ = io.WriteString(w, `{"data":{"id":"chat&a","conversation_id":"conversation?b"}}`)
			return
		}
		assert.Equal(t, "chat&a", request.URL.Query().Get("chat_id"))
		assert.Equal(t, "conversation?b", request.URL.Query().Get("conversation_id"))
		close(pollStarted)
		<-request.Context().Done()
		close(pollCancelled)
	}))
	t.Cleanup(upstream.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`)).WithContext(ctx)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: upstream.URL}}
	finished := make(chan error, 1)
	go func() {
		_, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{}`))
		finished <- err
	}()
	select {
	case <-pollStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("accepted chat did not enter polling")
	}
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("polling did not release the request after cancellation")
	}
	select {
	case <-pollCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("polling transport did not observe cancellation")
	}
	assert.True(t, info.UpstreamAccepted)
	assert.True(t, info.UpstreamSideEffecting)
}

func TestCozeNonStreamValidatesAllAnswersAndUsage(t *testing.T) {
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"multiple_answers", `{"data":[{"type":"answer","content":"one"},{"type":"follow_up","content":"ignore"},{"type":"answer","content":"two"}]}`, true},
		{"missing_answer", `{"data":[]}`, false},
		{"invalid_answer", `{"data":[{"type":"answer","content":{}}]}`, false},
		{"unsupported_answer", `{"data":[{"type":"answer","content_type":"card","content":"data"}]}`, false},
		{"provider_error", `{"code":42,"data":[{"type":"answer","content":"data"}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("coze_input_count", 10)
			c.Set("coze_output_count", 2)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-test"}}
			usage, err := cozeChatHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body))})
			if !test.valid {
				require.NotNil(t, err)
				assert.Equal(t, http.StatusBadGateway, err.StatusCode)
				assert.False(t, c.Writer.Written())
				return
			}
			require.Nil(t, err)
			assert.Equal(t, 12, usage.TotalTokens)
			assert.Equal(t, "provider", usage.UsageSource)
			assert.True(t, info.HasValidOutput())
			var result dto.OpenAITextResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
			assert.Equal(t, "chat.completion", result.Object)
			require.Len(t, result.Choices, 1)
			assert.Equal(t, "onetwo", result.Choices[0].Message.StringContent())
		})
	}
}
