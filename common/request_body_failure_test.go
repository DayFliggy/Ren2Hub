package common

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingBody struct{ reads, closes int }

func (b *failingBody) Read([]byte) (int, error) { b.reads++; return 0, io.ErrUnexpectedEOF }
func (b *failingBody) Close() error             { b.closes++; return nil }

func TestRequestBodyFailureIsStableAcrossReaders(t *testing.T) {
	old := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	for _, oversized := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		body := &failingBody{}
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Request.Body = body
		if oversized {
			c.Request.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1)))
			c.Request.ContentLength = -1
		}
		_, first := GetBodyStorage(c)
		require.Error(t, first)
		_, second := GetRequestBody(c)
		require.Same(t, first, second)
		var request map[string]any
		third := UnmarshalBodyReusable(c, &request)
		require.ErrorIs(t, third, first)
		require.Equal(t, oversized, IsRequestBodyTooLargeError(first))
		if !oversized {
			require.True(t, errors.Is(first, io.ErrUnexpectedEOF))
			require.Equal(t, 1, body.reads)
			require.Equal(t, 1, body.closes)
		}
	}
}
