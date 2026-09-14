package palm

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedPaLMStreamRequiresValidResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.Reader
		ok   bool
	}{
		{"complete", strings.NewReader(`{"candidates":[{"content":"complete answer"}]}`), true},
		{"malformed", strings.NewReader(`{"candidates":`), false},
		{"provider_error", strings.NewReader(`{"error":{"code":500,"message":"provider failed"}}`), false},
		{"read_error", iotest.ErrReader(errors.New("provider disconnected")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			apiErr, text := palmStreamHandler(c, info, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(tc.body)})
			require.NotNil(t, info.StreamStatus)
			assert.Equal(t, tc.ok, info.StreamStatus.IsNormalEnd())
			if tc.ok {
				require.Nil(t, apiErr)
				assert.Equal(t, "complete answer", text)
				assert.Contains(t, recorder.Body.String(), `"role":"assistant"`)
				assert.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
				assert.Contains(t, recorder.Body.String(), "[DONE]")
			} else {
				require.NotNil(t, apiErr)
				assert.Empty(t, text)
				assert.False(t, c.Writer.Written())
			}
		})
	}
}
