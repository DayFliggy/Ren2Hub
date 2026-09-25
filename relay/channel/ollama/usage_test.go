package ollama

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaUsagePresence(t *testing.T) {
	for _, test := range []struct {
		name, counts, source string
		prompt, completion   int
		complete             bool
	}{
		{"missing", "", "missing", 0, 0, false},
		{"null", `,"prompt_eval_count":null,"eval_count":null`, "invalid", 0, 0, false},
		{"missing_output", `,"prompt_eval_count":5`, "partial", 5, 0, false},
		{"missing_input", `,"eval_count":7`, "partial", 0, 7, false},
		{"null_output", `,"prompt_eval_count":5,"eval_count":null`, "invalid", 5, 0, false},
		{"zero", `,"prompt_eval_count":0,"eval_count":0`, "provider", 0, 0, true},
		{"complete", `,"prompt_eval_count":5,"eval_count":7`, "provider", 5, 7, true},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", test.name, stream), func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{IsStream: stream, RelayFormat: types.RelayFormatOpenAI,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}
				body := `{"model":"llama3.1","message":{"role":"assistant","content":"answer"},"done":true` + test.counts + "}\n"
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
				var usage *dto.Usage
				var apiErr *types.NewAPIError
				if stream {
					usage, apiErr = ollamaStreamHandler(c, info, resp)
				} else {
					usage, apiErr = ollamaChatHandler(c, info, resp)
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Contains(t, w.Body.String(), "answer", "unknown usage must not discard valid output")
				assert.Equal(t, test.source, usage.UsageSource)
				assert.Equal(t, test.prompt, usage.PromptTokens)
				assert.Equal(t, test.completion, usage.CompletionTokens)
				assert.Equal(t, test.prompt+test.completion, usage.TotalTokens)
				facts := service.NormalizedBillingUsage(usage, "openai")
				assert.Equal(t, test.complete, facts.Complete)
				assert.False(t, facts.Estimated, "absent provider counters must not be replaced with estimates")
			})
		}
	}
}

func TestOllamaUsageRejectsInvalidCounters(t *testing.T) {
	for _, counts := range []string{
		`"prompt_eval_count":-1,"eval_count":3`,
		`"prompt_eval_count":3,"eval_count":-1`,
		`"prompt_eval_count":"3","eval_count":1`,
		`"prompt_eval_count":1.5,"eval_count":1`,
		`"prompt_eval_count":9223372036854775807,"eval_count":1`,
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", counts, stream), func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{IsStream: stream, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}
				body := `{"message":{"role":"assistant","content":"invalid usage"},"done":true,` + counts + "}\n"
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
				_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
				require.NotNil(t, apiErr)
				assert.Empty(t, w.Body.String())
			})
		}
	}
}

func TestOllamaEmbeddingUsagePresence(t *testing.T) {
	for _, test := range []struct {
		name, count, source string
		prompt              int
		complete            bool
	}{
		{"missing", "", "missing", 0, false},
		{"null", `,"prompt_eval_count":null`, "invalid", 0, false},
		{"zero", `,"prompt_eval_count":0`, "provider", 0, true},
		{"complete", `,"prompt_eval_count":5`, "provider", 5, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "embed"}, Request: &dto.EmbeddingRequest{Input: "hello"}}
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"model":"embed","embeddings":[[0.1,0.2]]` + test.count + `}`))}
			usage, apiErr := ollamaEmbeddingHandler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Contains(t, w.Body.String(), `"embedding":[0.1,0.2]`)
			assert.Equal(t, test.source, usage.UsageSource)
			assert.Equal(t, test.prompt, usage.PromptTokens)
			assert.Zero(t, usage.CompletionTokens)
			assert.Equal(t, test.prompt, usage.TotalTokens)
			assert.Equal(t, test.complete, service.NormalizedBillingUsage(usage, "openai").Complete)
		})
	}
}
