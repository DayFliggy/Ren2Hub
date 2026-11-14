package common

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"mime/multipart"
	"os"
	"testing"
)

func TestReadTaskMultipartFormRemovesFilesAfterParseError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "file.bin")
	require.NoError(t, err)
	_, err = io.WriteString(part, "file payload")
	require.NoError(t, err)
	_, err = writer.CreateFormField("broken")
	require.NoError(t, err)
	// Leave the second part unfinished, after the first file was written to disk.
	_, err = multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary()).ReadForm(0)
	require.Error(t, err)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, files)
}
