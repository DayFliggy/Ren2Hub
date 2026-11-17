package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func channelObservations(channelID int) []service.RouteObservationSnapshot {
	var rows []service.RouteObservationSnapshot
	for _, row := range service.RoutingSnapshot().Observations {
		if row.ChannelID == channelID {
			rows = append(rows, row)
		}
	}
	return rows
}

// An upstream count error is answered with the protocol's own fields only. The
// message is the upstream error.message string, always masked with the
// channel's upstream addresses, so shapes the masker recognizes (the upstream
// address, api_key:value) are masked whatever the upstream error.code says;
// error.metadata and every other field outside the protocol whitelist never
// reach the caller, and a message that is not a string is replaced by a fixed
// one. The status and Retry-After are kept, and repeated count failures leave
// the channel's route observations unchanged.
func TestNativeCountUpstreamErrorsAreMaskedAndSkipRouteHealth(t *testing.T) {
	const leakyMessage = `"message":"backend UPSTREAM/internal/pool rejected api_key:sk-secretfragment"`
	shapes := []struct {
		name, fields string
		fixedMessage bool
	}{
		{"plain", `"code":"leak-code",` + leakyMessage, false},
		{"metadata", `"code":"leak-code","metadata":{"debug":"leak-metadata"},` + leakyMessage, false},
		{"count_token_failed code", `"code":"count_token_failed",` + leakyMessage, false},
		{"object message", `"code":"leak-code","message":{"text":"leak-object sk-secretfragment"}`, true},
	}
	for _, tc := range []struct {
		name, path, body string
		provider         int
	}{
		{"Claude", "/v1/messages/count_tokens", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`, constant.ChannelTypeAnthropic},
		{"Gemini", "/v1beta/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, constant.ChannelTypeGemini},
	} {
		for _, shape := range shapes {
			t.Run(tc.name+"/"+shape.name, func(t *testing.T) {
				var upstreamURL string
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					status := http.StatusServiceUnavailable
					if calls.Add(1) == 1 {
						status = http.StatusTooManyRequests
					}
					w.Header().Set("Retry-After", "30")
					w.WriteHeader(status)
					fields := strings.ReplaceAll(shape.fields, "UPSTREAM", upstreamURL)
					_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","status":"UNAVAILABLE","param":"leak-param","details":[{"reason":"leak-detail"}],` + fields + `},"debug":"leak-debug"}`))
				}))
				defer upstream.Close()
				upstreamURL = upstream.URL
				engine, _, token, ch := nativeCountFixture(t, tc.provider, upstream.URL)
				before := channelObservations(ch.Id)
				for i := range 7 {
					recorder := nativeCountRequest(engine, tc.path, tc.body, token.Key)
					body := recorder.Body.String()
					want := http.StatusServiceUnavailable
					if i == 0 {
						want = http.StatusTooManyRequests
					}
					require.Equal(t, want, recorder.Code, body)
					require.Equal(t, "30", recorder.Header().Get("Retry-After"))
					require.NotContains(t, body, "leak-")
					require.NotContains(t, body, "sk-secretfragment")
					require.NotContains(t, body, strings.TrimPrefix(upstream.URL, "http://"))
					var payload map[string]any
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload), body)
					errorObject, ok := payload["error"].(map[string]any)
					require.True(t, ok, body)
					message, _ := errorObject["message"].(string)
					if shape.fixedMessage {
						require.Equal(t, "native token count upstream returned an error", message)
					} else {
						require.True(t, strings.HasPrefix(message, "backend http://"), message)
						require.True(t, strings.HasSuffix(message, " rejected api_key:***"), message)
					}
					if tc.provider == constant.ChannelTypeAnthropic {
						require.Equal(t, map[string]any{"type": "error", "error": map[string]any{"type": "overloaded_error", "message": message}}, payload)
					} else {
						require.Equal(t, map[string]any{"error": map[string]any{"code": float64(want), "status": "UNAVAILABLE", "message": message}}, payload)
					}
					require.Zero(t, service.ChannelInFlight(ch.Id))
				}
				require.Equal(t, before, channelObservations(ch.Id))
			})
		}
	}
}
