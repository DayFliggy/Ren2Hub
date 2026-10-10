package channel

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// wssURLAdaptor supplies only the two hooks DoWssRequest calls.
type wssURLAdaptor struct {
	Adaptor
	url string
}

func (a wssURLAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a wssURLAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func TestDoWssRequestHonorsChannelTLSInsecureSkipVerify(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(upstream.Close)
	adaptor := wssURLAdaptor{url: "wss://" + strings.TrimPrefix(upstream.URL, "https://")}

	for _, skip := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{TLSInsecureSkipVerify: skip},
		}}

		conn, err := DoWssRequest(adaptor, c, info, nil)
		if !skip {
			var verifyErr *tls.CertificateVerificationError
			require.ErrorAs(t, err, &verifyErr, "a verifying channel must reject the self-signed upstream")
			continue
		}
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}
}
