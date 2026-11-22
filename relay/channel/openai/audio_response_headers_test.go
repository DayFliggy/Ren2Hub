package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestOpenAITTSFiltersResponseCredentialsAndConnectionTokens(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/audio/speech", nil)
	body := []byte{0, 0, 1, 1}
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/pcm"}, "Set-Cookie": {"session=secret"}, "Authorization": {"Bearer secret"}, "Connection": {"X-Private"}, "X-Private": {"secret"}, "X-Trace": {"trace"}}, Body: io.NopCloser(bytes.NewReader(body))}
	OpenaiTTSHandler(c, resp, &relaycommon.RelayInfo{Request: &dto.AudioRequest{ResponseFormat: "pcm"}})
	for _, key := range []string{"Set-Cookie", "Authorization", "Connection", "X-Private"} {
		assert.Empty(t, w.Header().Get(key), key)
	}
	assert.Equal(t, "trace", w.Header().Get("X-Trace"))
	assert.Equal(t, body, w.Body.Bytes())
}
