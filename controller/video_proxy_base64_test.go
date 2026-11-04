package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskMediaBase64ValidationBeforeResponse(t *testing.T) {
	for _, tc := range []struct {
		name, encoded, want string
		invalid             bool
	}{
		{name: "standard", encoded: "+/8=", want: "\xfb\xff"},
		{name: "raw standard", encoded: "+/8", want: "\xfb\xff"},
		{name: "url", encoded: "-_8=", want: "\xfb\xff"},
		{name: "raw url", encoded: "-_8", want: "\xfb\xff"},
		{name: "line breaks", encoded: "aGVs\r\nbG8=", want: "hello"},
		{name: "trailing invalid byte", encoded: strings.Repeat("YQ==", 2048) + "!", invalid: true},
		{name: "short final quantum", encoded: "a", invalid: true},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(method, "/content", nil)
				err := writeTaskMediaBase64(c, "video/webm", tc.encoded)
				if tc.invalid {
					require.Error(t, err)
					require.False(t, c.Writer.Written())
					require.Empty(t, recorder.Body.String())
					require.Empty(t, recorder.Header().Get("Content-Type"))
					return
				}
				require.NoError(t, err)
				require.Equal(t, strconv.Itoa(len(tc.want)), recorder.Header().Get("Content-Length"))
				if method == http.MethodHead {
					require.Empty(t, recorder.Body.String())
				} else {
					require.Equal(t, tc.want, recorder.Body.String())
				}
			})
		}
	}
}
