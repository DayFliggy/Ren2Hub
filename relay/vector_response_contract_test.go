package relay

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type vectorAdapterFixture struct {
	name, wire string
	channel    int
}

func TestVectorAdaptersValidateBeforeDelivery(t *testing.T) {
	for _, adapter := range []vectorAdapterFixture{
		{"openai_embedding", "embedding", constant.ChannelTypeOpenAI},
		{"azure_embedding", "embedding", constant.ChannelTypeAzure},
		{"ali_embedding", "embedding", constant.ChannelTypeAli},
		{"siliconflow_embedding", "embedding", constant.ChannelTypeSiliconFlow},
		{"moonshot_embedding", "embedding", constant.ChannelTypeMoonshot},
		{"minimax_embedding", "embedding", constant.ChannelTypeMiniMax},
		{"volcengine_embedding", "embedding", constant.ChannelTypeVolcEngine},
		{"zhipu_v4_embedding", "embedding", constant.ChannelTypeZhipu_v4},
		{"openrouter_embedding", "embedding", constant.ChannelTypeOpenRouter},
		{"newapi_embedding", "embedding", constant.ChannelTypeNewAPI},
		{"sub2api_embedding", "embedding", constant.ChannelTypeSub2API},
		{"cloudflare_embedding", "embedding", constant.ChannelCloudflare},
		{"jina_embedding", "embedding", constant.ChannelTypeJina},
		{"baidu_embedding", "embedding", constant.ChannelTypeBaidu},
		{"moka_embedding", "embedding", constant.ChannelTypeMokaAI},
		{"ollama_embedding", "ollama", constant.ChannelTypeOllama},
		{"gemini_embedding", "gemini", constant.ChannelTypeGemini},
		{"gemini_native_single", "gemini_single", constant.ChannelTypeGemini},
		{"gemini_native_batch", "gemini_batch", constant.ChannelTypeGemini},
		{"openai_rerank", "rerank", constant.ChannelTypeOpenAI},
		{"jina_rerank", "rerank", constant.ChannelTypeJina},
		{"moonshot_rerank", "rerank", constant.ChannelTypeMoonshot},
		{"xinference_rerank", "xinference", constant.ChannelTypeXinference},
		{"ali_rerank", "ali", constant.ChannelTypeAli},
		{"cohere_rerank", "cohere", constant.ChannelTypeCohere},
		{"siliconflow_rerank", "siliconflow", constant.ChannelTypeSiliconFlow},
	} {
		valid, empty, malformed, negative, source := vectorProviderResponses(adapter.wire)
		for _, scenario := range []struct {
			name, body string
			valid      bool
		}{
			{"valid", valid, true}, {"empty", empty, false}, {"invalid_index_or_count", malformed, false}, {"negative_usage", negative, false},
		} {
			if scenario.body == "" {
				continue
			}
			t.Run(adapter.name+"/"+scenario.name, func(t *testing.T) {
				c, info, recorder := newVectorAdapterContext(t, adapter)
				adaptor := GetAdaptor(info.ApiType)
				adaptor.Init(info)
				usage, apiErr := adaptor.DoResponse(c, vectorHTTPResponse(scenario.body), info)
				if !scenario.valid {
					require.NotNil(t, apiErr)
					assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
					assert.False(t, c.Writer.Written(), "invalid upstream data must not commit a client response")
					assert.False(t, info.HasValidOutput())
					assert.Empty(t, recorder.Body.String())
					return
				}
				require.Nil(t, apiErr)
				require.IsType(t, &dto.Usage{}, usage)
				observed := usage.(*dto.Usage)
				assert.Equal(t, source, observed.UsageSource)
				expectedTokens := 10
				if source == "estimated" {
					expectedTokens = 7
				}
				assert.Equal(t, expectedTokens, observed.PromptTokens)
				assert.Equal(t, expectedTokens, observed.TotalTokens)
				assert.True(t, info.HasValidOutput())
				assert.True(t, c.Writer.Written())
				assert.Equal(t, http.StatusOK, recorder.Code)
				var output map[string]any
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &output))
				switch adapter.wire {
				case "gemini_single":
					assert.NotEmpty(t, output["embedding"])
				case "gemini_batch":
					assert.Len(t, output["embeddings"], 2)
				case "embedding", "ollama", "gemini":
					assert.Len(t, output["data"], 2)
				default:
					assert.Len(t, output["results"], 1)
				}
			})
		}
	}
}

func vectorProviderResponses(wire string) (valid, empty, malformed, negative, source string) {
	const results = `"results":[{"index":1,"relevance_score":0,"document":{"text":"second"}}]`
	source = "provider"
	switch wire {
	case "embedding":
		valid = `{"object":"list","model":"text-embedding-3-small","data":[{"object":"embedding","index":0,"embedding":[0,1]},{"object":"embedding","index":1,"embedding":[1,0]}],"usage":{"prompt_tokens":10,"total_tokens":10}}`
		empty = `{"data":[]}`
		malformed = strings.Replace(valid, `"index":1`, `"index":2`, 1)
		negative = strings.ReplaceAll(valid, ":10", ":-1")
	case "ollama":
		valid = `{"model":"nomic-embed-text","embeddings":[[0,1],[1,0]],"prompt_eval_count":10}`
		empty = `{"embeddings":[]}`
		malformed = `{"embeddings":[[0,1]],"prompt_eval_count":10}`
		negative = strings.Replace(valid, ":10", ":-1", 1)
	case "gemini", "gemini_batch":
		valid = `{"embeddings":[{"values":[0,1]},{"values":[1,0]}]}`
		empty = `{"embeddings":[null]}`
		malformed = `{"embeddings":[{"values":[0,1]}]}`
		source = "estimated"
	case "gemini_single":
		valid = `{"embedding":{"values":[0,1]}}`
		empty = `{"embedding":null}`
		malformed = `{"embedding":{"values":[null,1]}}`
		source = "estimated"
	case "rerank", "xinference":
		valid = `{` + results + `,"usage":{"total_tokens":10}}`
		empty = `{"results":[]}`
		malformed = strings.Replace(valid, `"index":1`, `"index":2`, 1)
		negative = strings.Replace(valid, ":10", ":-1", 1)
		if wire == "xinference" {
			source, negative = "estimated", ""
		}
	case "ali":
		valid = `{"output":{` + results + `},"usage":{"total_tokens":10},"request_id":"vector-local"}`
		empty = `{"output":{"results":[]}}`
		malformed = strings.Replace(valid, `"index":1`, `"index":2`, 1)
		negative = strings.Replace(valid, ":10", ":-1", 1)
	case "cohere":
		valid = `{` + results + `,"meta":{"billed_units":{"input_tokens":10,"output_tokens":0}}}`
		empty = `{"results":[]}`
		malformed = strings.Replace(valid, `"index":1`, `"index":2`, 1)
		negative = `{` + results + `,"meta":{"billed_units":{"input_tokens":0,"output_tokens":-1}}}`
	case "siliconflow":
		valid = `{` + results + `,"meta":{"tokens":{"input_tokens":10,"output_tokens":0}}}`
		empty = `{"results":[]}`
		malformed = strings.Replace(valid, `"index":1`, `"index":2`, 1)
		negative = strings.Replace(valid, ":10", ":-1", 1)
	}
	return
}

func newVectorAdapterContext(t *testing.T, fixture vectorAdapterFixture) (*gin.Context, *relaycommon.RelayInfo, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	apiType, registered := common.ChannelType2APIType(fixture.channel)
	require.True(t, registered)
	require.NotNil(t, GetAdaptor(apiType))
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, ChannelType: fixture.channel, UpstreamModelName: "text-embedding-3-small"},
		RelayMode: relayconstant.RelayModeEmbeddings, RelayFormat: types.RelayFormatEmbedding,
		Request:      &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: []any{"hello", "world"}},
		RerankerInfo: &relaycommon.RerankerInfo{Documents: []any{"first", "second"}, ReturnDocuments: true}}
	info.SetEstimatePromptTokens(7)
	switch fixture.wire {
	case "gemini_single", "gemini_batch":
		info.RelayMode, info.RelayFormat = relayconstant.RelayModeGemini, types.RelayFormatGemini
		info.RequestURLPath = "/v1beta/models/gemini-embedding-001:embedContent"
		info.Request = &dto.GeminiEmbeddingRequest{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "hello"}}}}
		if fixture.wire == "gemini_batch" {
			info.RequestURLPath = "/v1beta/models/gemini-embedding-001:batchEmbedContents"
			info.IsGeminiBatchEmbedding = true
			info.Request = &dto.GeminiBatchEmbeddingRequest{Requests: []*dto.GeminiEmbeddingRequest{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "hello"}}}}, {Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "world"}}}}}}
		}
	case "embedding", "ollama", "gemini":
	default:
		info.RelayMode, info.RelayFormat = relayconstant.RelayModeRerank, types.RelayFormatRerank
		info.Request = &dto.RerankRequest{Model: "rerank-test", Query: "hello", Documents: info.Documents}
	}
	return c, info, recorder
}

func vectorHTTPResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

type vectorDisconnectedWriter struct{ gin.ResponseWriter }

func (w vectorDisconnectedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestVectorAdaptersDoNotTreatDisconnectedWriterAsDelivery(t *testing.T) {
	for _, fixture := range []vectorAdapterFixture{{"embedding", "embedding", constant.ChannelTypeOpenAI}, {"rerank", "rerank", constant.ChannelTypeOpenAI}} {
		t.Run(fixture.name, func(t *testing.T) {
			c, info, recorder := newVectorAdapterContext(t, fixture)
			c.Writer = vectorDisconnectedWriter{c.Writer}
			valid, _, _, _, _ := vectorProviderResponses(fixture.wire)
			_, apiErr := GetAdaptor(info.ApiType).DoResponse(c, vectorHTTPResponse(valid), info)
			require.NotNil(t, apiErr)
			require.NotNil(t, info.StreamStatus)
			assert.True(t, errors.Is(info.StreamStatus.EndError, io.ErrClosedPipe))
			assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
			assert.False(t, info.HasValidOutput())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestVectorGeminiDispatchUsesEmbeddingOperation(t *testing.T) {
	fixture := vectorAdapterFixture{"mapped_gemini_embedding", "gemini", constant.ChannelTypeGemini}
	c, info, recorder := newVectorAdapterContext(t, fixture)
	info.UpstreamModelName, info.ChannelBaseUrl, info.IsGeminiBatchEmbedding = "private-vector-deployment", "https://example.invalid", true
	adaptor := GetAdaptor(info.ApiType)
	adaptor.Init(info)
	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Contains(t, url, "private-vector-deployment:batchEmbedContents")
	valid, _, _, _, _ := vectorProviderResponses(fixture.wire)
	_, apiErr := adaptor.DoResponse(c, vectorHTTPResponse(valid), info)
	require.Nil(t, apiErr)
	assert.True(t, info.HasValidOutput())
	var output map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &output))
	assert.Len(t, output["data"], 2)
}
