package ali

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliImageConvertedQuantityMatchesBudget(t *testing.T) {
	for _, test := range []struct {
		name, body string
		direct     bool
		count      float64
	}{
		{"standard", `{"model":"wanx-v1","prompt":"image","n":4,"size":"1024x1024"}`, false, 4},
		{"direct_dto", ``, true, 4},
		{"default", `{"model":"wanx-v1","prompt":"image"}`, false, 1},
		{"native_parameters", `{"model":"wanx-v1","prompt":"image","n":1,"parameters":{"n":3,"size":"1024*1024"}}`, false, 3},
		{"native_default", `{"model":"wanx-v1","prompt":"image","parameters":{}}`, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			request := dto.ImageRequest{}
			if test.direct {
				count := uint(4)
				request = dto.ImageRequest{Model: "wanx-v1", Prompt: "image", N: &count, Size: "1024x1024"}
			} else {
				require.NoError(t, common.Unmarshal([]byte(test.body), &request))
			}
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations, ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeAli, UpstreamModelName: request.Model}}
			converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			meta, err := helper.FinalImagePriceMeta(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			assert.Equal(t, test.count, meta.BillingRatios["n"])
			var wire AliImageRequest
			require.NoError(t, common.Unmarshal(body, &wire))
			assert.Equal(t, int(test.count), wire.Parameters.N)
			if request.Size != "" {
				assert.Equal(t, "1024*1024", wire.Parameters.Size)
			}
		})
	}
}

func TestAliImageRejectsInvalidNestedQuantities(t *testing.T) {
	for _, parameters := range []string{`null`, `{"n":-1}`, `{"n":129}`, `{"n":18446744073709551615}`} {
		request := dto.ImageRequest{Extra: map[string]json.RawMessage{"parameters": json.RawMessage(parameters)}}
		_, err := oaiImage2AliImageRequest(&relaycommon.RelayInfo{}, request, true)
		require.Error(t, err)
	}
}

func TestAliAsyncImagePollingRejectsFailures(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"invalid_json", `{"output":`, http.StatusOK},
		{"missing_status", `{"output":{}}`, http.StatusOK},
		{"failed", `{"output":{"task_status":"FAILED","code":"generation_failed"}}`, http.StatusOK},
		{"canceled", `{"output":{"task_status":"CANCELED"}}`, http.StatusOK},
		{"unknown", `{"output":{"task_status":"UNKNOWN"}}`, http.StatusOK},
		{"unexpected_status", `{"output":{"task_status":"INVALID"}}`, http.StatusOK},
		{"wrong_task", `{"output":{"task_id":"other","task_status":"SUCCEEDED"}}`, http.StatusOK},
		{"provider_error", `{"code":"ProviderFailure","message":"failed"}`, http.StatusOK},
		{"http_error", `{"error":{"message":"busy","type":"upstream_error"}}`, http.StatusServiceUnavailable},
		{"empty_http_success", ``, http.StatusNoContent},
		{"http_redirect", ``, http.StatusFound},
		{"truncated_body", `{"output":{"task_status":"SUCCEEDED"}}`, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls.Add(1)
				assert.Equal(t, "/api/v1/tasks/task-1", r.URL.Path)
				if test.name == "truncated_body" {
					w.Header().Set("Content-Length", "10000")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
			result, body, err := asyncTaskWait(c, info, "task-1", nil)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Nil(t, body)
			assert.Equal(t, int32(1), polls.Load(), "poll errors must terminate without re-submitting or looping indefinitely")
			if test.status != http.StatusOK {
				var apiErr *types.NewAPIError
				require.ErrorAs(t, err, &apiErr)
				expectedStatus := test.status
				if expectedStatus < http.StatusBadRequest {
					expectedStatus = http.StatusBadGateway
				}
				assert.Equal(t, expectedStatus, apiErr.StatusCode)
			}
		})
	}
}

func TestAliAsyncImagePollingCancels(t *testing.T) {
	service.InitHttpClient()
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "in_http_request", true: "between_polls"}[pending], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls.Add(1)
				if pending {
					_, _ = io.WriteString(w, `{"output":{"task_status":"PENDING"}}`)
					w.(http.Flusher).Flush()
				}
				close(started)
				if !pending {
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
			finished := make(chan error, 1)
			go func() {
				_, _, err := asyncTaskWait(c, info, "task-1", nil)
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("task poll was not sent")
			}
			cancel()
			select {
			case err := <-finished:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(3 * time.Second):
				t.Fatal("task polling did not stop after cancellation")
			}
			assert.Equal(t, int32(1), polls.Load())
		})
	}
}

func TestAliAsyncImageDelivery(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		name, results string
		wantError     bool
	}{
		{"success", `[{"url":"https://images.example/1.png"},{"url":"https://images.example/2.png"}]`, false},
		{"empty", `[]`, true},
		{"failed_result", `[{"url":"https://images.example/1.png"},{"code":"ImageFailed","message":"failed"}]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","results":`+test.results+`}}`)
			}))
			defer server.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
			info.PriceData.Usage = &billing.Usage{Quantities: map[string]string{"images": "3"}}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"output":{"task_id":"task-1"}}`))}
			apiErr, usage := aliImageHandler(&Adaptor{}, c, resp, info)
			if test.wantError {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Nil(t, usage)
				assert.Empty(t, recorder.Body.String())
				assert.False(t, info.HasValidOutput())
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, "2", info.PriceData.Usage.Quantities["images"])
			var result dto.ImageResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
			assert.Len(t, result.Data, 2)
		})
	}
}

func TestAliImageDownloadFailureDoesNotDeliverPartialSuccess(t *testing.T) {
	service.InitHttpClient()
	originalLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = originalLimit })
	fetch := system_setting.GetFetchSetting()
	original := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() { *fetch = original })
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.URL.Path == "/bad.png" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		if r.URL.Path == "/empty.png" {
			return
		}
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	for _, shape := range []string{"results", "choices", "empty"} {
		t.Run(shape, func(t *testing.T) {
			downloads.Store(0)
			body := `{"output":{"results":[{"url":"` + server.URL + `/good.png"},{"url":"` + server.URL + `/bad.png"}]}}`
			if shape == "choices" {
				body = `{"output":{"choices":[{"message":{"content":[{"image":"` + server.URL + `/good.png"},{"image":"` + server.URL + `/bad.png"}]}}]}}`
			}
			if shape == "empty" {
				body = `{"output":{"results":[{"url":"` + server.URL + `/empty.png"}]}}`
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			c.Set("response_format", "b64_json")
			info := &relaycommon.RelayInfo{StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{}}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			apiErr, usage := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, resp, info)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.Nil(t, usage)
			assert.Empty(t, recorder.Body.String())
			assert.False(t, info.HasValidOutput())
			expectedDownloads := int32(2)
			if shape == "empty" {
				expectedDownloads = 1
			}
			assert.Equal(t, expectedDownloads, downloads.Load())
		})
	}
}

func TestAliImageChoicesPreserveEveryImage(t *testing.T) {
	var response AliResponse
	require.NoError(t, common.Unmarshal([]byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://images.example/1.png"},{"text":"revised"},{"image":"https://images.example/2.png"}]}}]}}`), &response))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	images, err := response.Output.ChoicesToOpenAIImageDate(c, "url")
	require.NoError(t, err)
	require.Len(t, images, 2)
	assert.Equal(t, "https://images.example/1.png", images[0].Url)
	assert.Equal(t, "https://images.example/2.png", images[1].Url)
	assert.Equal(t, "revised", images[0].RevisedPrompt)
	assert.Equal(t, "revised", images[1].RevisedPrompt)

	require.NoError(t, common.Unmarshal([]byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://images.example/1.png"}]}},{"message":{"content":[{"text":"generation failed"}]}}]}}`), &response))
	images, err = response.Output.ChoicesToOpenAIImageDate(c, "url")
	require.Error(t, err)
	assert.Nil(t, images)
}

func TestAliNativeImageResponseUsesInitializedModel(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		model string
		mode  int
		async bool
	}{
		{"qwen-image-3.0-pro", relayconstant.RelayModeImagesGenerations, false},
		{"qwen-image-edit", relayconstant.RelayModeImagesEdits, false},
		{"z-image", relayconstant.RelayModeImagesGenerations, false},
		{"wan2.6-image", relayconstant.RelayModeImagesEdits, true},
	} {
		t.Run(test.model, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls.Add(1)
				_, _ = io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://images.example/1.png"}]}}`)
			}))
			defer server.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{RelayMode: test.mode, StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, UpstreamModelName: test.model}}
			body := `{"output":{"choices":[{"message":{"content":[{"image":"https://images.example/1.png"}]}}]}}`
			if test.async {
				body = `{"output":{"task_id":"task-1"}}`
			}
			adaptor := &Adaptor{}
			adaptor.Init(info)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			usage, apiErr := adaptor.DoResponse(c, resp, info)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, test.async, polls.Load() == 1)
		})
	}
}

func TestAliTaskPollKeepsSubmittedTransportIdentity(t *testing.T) {
	service.InitHttpClient()
	for _, useSubmitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "resolved_headers", true: "submitted_headers"}[useSubmitted], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "tenant.images.example", r.Host)
				assert.Equal(t, "tenant-1", r.Header.Get("X-Tenant"))
				assert.Equal(t, "Bearer selected-credential", r.Header.Get("Authorization"))
				assert.Empty(t, r.Header.Get("X-DashScope-Async"))
				_, _ = io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://images.example/1.png"}]}}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations, ChannelMeta: &relaycommon.ChannelMeta{
				ChannelBaseUrl: server.URL, ApiKey: "selected-credential", UpstreamModelName: "wanx-v1",
				HeadersOverride: map[string]interface{}{"Host": "tenant.images.example", "X-Tenant": "tenant-1"},
			}}
			var submitted *http.Request
			if useSubmitted {
				var err error
				submitted, err = channel.BuildApiRequest(&Adaptor{}, c, info, nil)
				require.NoError(t, err)
				info.HeadersOverride["X-Tenant"] = "later-change"
			}
			result, _, err := asyncTaskWait(c, info, "task-1", submitted)
			require.NoError(t, err)
			assert.Equal(t, "SUCCEEDED", result.Output.TaskStatus)
		})
	}
}
