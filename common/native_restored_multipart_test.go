//go:build known_upstream_restored

package common

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoredNativeTaskTextBudget(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	previousBodyLimit, previousFileLimit := constant.MaxRequestBodyMB, constant.MaxFileDownloadMB
	constant.MaxRequestBodyMB, constant.MaxFileDownloadMB = 16, 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB, constant.MaxFileDownloadMB = previousBodyLimit, previousFileLimit
	})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field := strings.Repeat("a", 12<<20)
	require.NoError(t, writer.WriteField("request", field))
	fileBytes := bytes.Repeat([]byte("b"), 768<<10)
	for _, filename := range []string{"first.bin", "second.bin"} {
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(fileBytes)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/body", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { CleanupBodyStorage(c) })

	// Prior middleware may have consumed the cached body completely.
	storage, err := GetBodyStorage(c)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, storage)
	require.NoError(t, err)
	form, err := parseRestoredTaskMultipart(c)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, form.RemoveAll()) })
	assert.Equal(t, []string{field}, form.Value["request"])
	require.Len(t, form.File["file"], 2)
	for index, header := range form.File["file"] {
		opened, openErr := header.Open()
		require.NoError(t, openErr)
		_, onDisk := opened.(*os.File)
		assert.Equal(t, index == 1, onDisk, "the file memory budget is shared across all parts")
		actual, readErr := io.ReadAll(opened)
		require.NoError(t, readErr)
		require.NoError(t, opened.Close())
		assert.Equal(t, fileBytes, actual)
	}
	replayed, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, body.Bytes(), replayed)
	require.NoError(t, form.RemoveAll())
	_, err = form.File["file"][1].Open()
	require.ErrorIs(t, err, os.ErrNotExist)
}

var parseRestoredTaskMultipart = ParseMultipartFormReusable
