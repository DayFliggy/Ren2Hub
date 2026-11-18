package xunfei

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRouteScanBeforeAndAfterXunfeiDial(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	var logged bytes.Buffer
	common.LogWriterMu.Lock()
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logged
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previous
		common.LogWriterMu.Unlock()
	})

	for _, dispatch := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		var selected service.PolicyEvent
		require.NoError(t, common.Unmarshal([]byte(`{"channel_id":11,"scan":{"candidates":1}}`), &selected))
		service.RequestPolicy(c).AddEvent(selected)
		if dispatch {
			_, _, err := xunfeiMakeRequest(c, dto.GeneralOpenAIRequest{}, "test", "ws"+strings.TrimPrefix(upstream.URL, "http"), "app")
			require.Error(t, err)
		} else {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "invalid"}}
			_, err := (&Adaptor{}).DoResponse(c, nil, info)
			require.NotNil(t, err)
		}
		require.False(t, service.RouteAttemptWasSent(c), "this adapter does not record channel health or usage attempts")
		service.LogRejectedRouteSelection(c, http.StatusBadGateway)
		require.Equal(t, 1, strings.Count(logged.String(), "route_scan "), "only the invalid key before dial should log a local rejection")
	}
}
