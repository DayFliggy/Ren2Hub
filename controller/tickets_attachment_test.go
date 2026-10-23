package controller

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	return buffer.Bytes()
}

func ticketMultipart(t *testing.T, fields map[string]string, files ...[]byte) ([]byte, string) {
	t.Helper()
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	for _, content := range files {
		part, err := writer.CreateFormFile("attachments", "image.png")
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return payload.Bytes(), writer.FormDataContentType()
}

func ticketContext(body io.Reader, contentType string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tickets", body)
	ctx.Request.Header.Set("Content-Type", contentType)
	return ctx, recorder
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestTicketFormAbortsAtFirstLimitViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := tinyPNG(t)
	oversized := append(append([]byte{}, valid...), make([]byte, maxTicketAttachmentSize)...)
	tooMany := [][]byte{valid, valid, valid, valid, valid, append(append([]byte{}, valid...), make([]byte, 2<<20)...)}
	for _, tc := range []struct {
		name     string
		files    [][]byte
		code     string
		maxRead  int64
		minTotal int64
	}{
		{"sixth attachment stops before its body", tooMany, "ATTACHMENT_TOO_MANY", 1 << 20, 2 << 20},
		{"oversized attachment stops at the limit", [][]byte{valid, oversized}, "ATTACHMENT_TOO_LARGE", maxTicketAttachmentSize + 1<<20, maxTicketAttachmentSize},
		{"invalid later attachment removes earlier staged files", [][]byte{valid, []byte("not an image")}, "ATTACHMENT_TYPE_UNSUPPORTED", 1 << 20, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TICKET_UPLOAD_DIR", dir)
			body, contentType := ticketMultipart(t, map[string]string{"content": "hello"}, tc.files...)
			require.GreaterOrEqual(t, int64(len(body)), tc.minTotal)
			reader := &countingReader{reader: bytes.NewReader(body)}
			ctx, _ := ticketContext(reader, contentType)
			form, err := readTicketForm(ctx)
			require.Nil(t, form)
			var formErr *ticketFormError
			require.True(t, errors.As(err, &formErr), "%v", err)
			require.Equal(t, tc.code, formErr.code)
			require.Less(t, reader.read, tc.maxRead, "parser kept reading after the violation")
			requireEmptyDir(t, dir)
		})
	}
}

func useTicketTestDatabase(t *testing.T) {
	t.Helper()
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Ticket{}, &model.TicketMessage{}, &model.TicketAttachment{}))
}

func TestCreateTicketCompensatesFilesWhenDatabaseCommitFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	fields := map[string]string{"title": "t", "category": "api", "priority": "normal", "content": "c"}
	create := func() map[string]any {
		body, contentType := ticketMultipart(t, fields, tinyPNG(t), tinyPNG(t))
		ctx, recorder := ticketContext(bytes.NewReader(body), contentType)
		ctx.Set("id", 9)
		CreateTicket(ctx)
		var response map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response
	}
	require.Equal(t, true, create()["success"])
	stored, err := model.ListTicketAttachments(1)
	require.NoError(t, err)
	require.Len(t, stored, 2)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := []string{}
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), ".upload-"), "temporary file left behind")
		names = append(names, entry.Name())
	}
	require.ElementsMatch(t, []string{stored[0].StorageKey, stored[1].StorageKey}, names)

	// 事务失败时临时文件被清理，不留下孤儿文件。
	require.NoError(t, model.DB.Migrator().DropTable(&model.TicketAttachment{}))
	require.Equal(t, false, create()["success"])
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, after, 2)
}

func TestTicketFormCommitFailureRemovesPromotedAndTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	body, contentType := ticketMultipart(t, map[string]string{"content": "hello"}, tinyPNG(t), tinyPNG(t))
	ctx, _ := ticketContext(bytes.NewReader(body), contentType)
	form, err := readTicketForm(ctx)
	require.NoError(t, err)
	previous := renameTicketFile
	calls := 0
	renameTicketFile = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("forced rename failure")
		}
		return previous(oldPath, newPath)
	}
	t.Cleanup(func() { renameTicketFile = previous })
	require.Error(t, form.commit())
	requireEmptyDir(t, dir)
}

func TestTicketAttachmentFilenameCharacterLimit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("attachments", strings.Repeat("界", 256)+".png")
	require.NoError(t, err)
	_, err = part.Write(tinyPNG(t))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	ctx, _ := ticketContext(bytes.NewReader(body.Bytes()), writer.FormDataContentType())
	form, err := readTicketForm(ctx)
	require.Nil(t, form)
	var formErr *ticketFormError
	require.ErrorAs(t, err, &formErr)
	require.Equal(t, "ATTACHMENT_NAME_TOO_LONG", formErr.code)
	requireEmptyDir(t, dir)
}

func TestTicketAttachmentDownloadForbidsSniffing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	ticket := &model.Ticket{UserID: 5, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 5, []model.TicketAttachment{{StorageKey: "abc.png", OriginalName: "a.png", MimeType: "image/png", Size: 1}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dir+"/abc.png", tinyPNG(t), 0600))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 5)
	ctx.Params = gin.Params{{Key: "attachment_id", Value: "1"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/tickets/attachments/1", nil)
	DownloadTicketAttachment(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
}
