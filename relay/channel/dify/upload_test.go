package dify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDifyUploadDecodesCompleteBase64AndCleansTemporaryFiles(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	service.InitHttpClient()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v1/files/upload", r.URL.Path)
		assert.Equal(t, "Bearer local-test-key", r.Header.Get("Authorization"))
		if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
		assert.Equal(t, "user-7", r.FormValue("user"))
		file, _, err := r.FormFile("file")
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		assert.NoError(t, err)
		assert.Equal(t, []byte("image bytes"), data)
		_, _ = w.Write([]byte(`{"id":"file-7"}`))
	}))
	defer upstream.Close()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: upstream.URL, ApiKey: "local-test-key"}}
	for _, tc := range []struct {
		name, encoded string
		valid         bool
	}{
		{name: "data url", encoded: "data:image/png;base64,aW1hZ2UgYnl0ZXM=", valid: true},
		{name: "plain base64", encoded: "aW1hZ2UgYnl0ZXM=", valid: true},
		{name: "incomplete base64", encoded: "aW1hZ2UgYnl0ZXM"},
		{name: "trailing invalid data", encoded: "aW1hZ2UgYnl0ZXM=!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			before := calls.Load()
			file := uploadDifyFile(c, info, "user-7", dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: tc.encoded, MimeType: "image/png"}})
			if tc.valid {
				require.NotNil(t, file)
				require.Equal(t, "file-7", file.UploadFileId)
				require.Equal(t, before+1, calls.Load())
			} else {
				require.Nil(t, file)
				require.Equal(t, before, calls.Load(), "partial input must not be uploaded")
			}
			entries, err := os.ReadDir(temp)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
