package relay

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/execution"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyAdmissionRunsAfterValidationBeforePricing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		mode     int
		run      func(*gin.Context, *relaycommon.RelayInfo) *taskdto.MidjourneyResponse
		admitted bool
	}{
		{"invalid imagine", `{}`, relayconstant.RelayModeMidjourneyImagine, RelayMidjourneySubmit, false},
		{"valid imagine", `{"prompt":"fixture scene"}`, relayconstant.RelayModeMidjourneyImagine, RelayMidjourneySubmit, true},
		{"invalid swap", `{"sourceBase64":"fixture"}`, relayconstant.RelayModeSwapFace, RelaySwapFace, false},
		{"valid swap", `{"sourceBase64":"fixture","targetBase64":"fixture"}`, relayconstant.RelayModeSwapFace, RelaySwapFace, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			admitted := false
			c.Set(channel.BasicValidatedContextKey, func() error { admitted = true; return errors.New("fixture admission rejected") })
			response := tc.run(c, &relaycommon.RelayInfo{RelayMode: tc.mode})
			require.NotNil(t, response)
			require.Equal(t, tc.admitted, admitted)
			if tc.admitted {
				require.Equal(t, "fixture admission rejected", response.Description)
			}
		})
	}
}

func TestMidjourneyAndHTTPShareOneApplicationCallBudget(t *testing.T) {
	service.InitHttpClient()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":1,"result":"fixture-task"}`)
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1}}
	state := channel.RequestExecution(c, execution.Submission)
	startCall := func() (func(*http.Response, error), error) { return channel.BeginUpstreamCall(c, info) }
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{`))
	_, _, err := service.DoMidjourneyHttpRequest(c, time.Second, upstream.URL, startCall)
	require.Error(t, err)
	require.Zero(t, state.CallCount())
	for index := 0; index < 3; index++ {
		c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"fixture"}`))
		if index == 0 {
			req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader(`{}`))
			require.NoError(t, err)
			response, err := channel.DoRequest(c, req, info)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			continue
		}
		_, _, err := service.DoMidjourneyHttpRequest(c, time.Second, upstream.URL, startCall)
		require.NoError(t, err)
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"fixture"}`))
	_, _, err = service.DoMidjourneyHttpRequest(c, time.Second, upstream.URL, startCall)
	require.ErrorIs(t, err, execution.ErrCallBudget)
	require.EqualValues(t, 3, calls.Load())
}
