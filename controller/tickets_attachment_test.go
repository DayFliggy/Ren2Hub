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
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
			form, err := readSupportTicketForm(ctx)
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

	// 事务失败时已提升和临时的文件都被清理，不留下孤儿文件。预算查询照常成功，
	// 失败发生在事务里写附件记录的那一步。
	failed := failTicketAttachmentInserts(t)
	require.Equal(t, false, create()["success"])
	require.Positive(t, failed.Load(), "the failure must happen inside the transaction")
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.ElementsMatch(t, names, entryNames(after))
	requireNoTicketReservations(t)
}

// failTicketAttachmentInserts makes every insert into ticket_attachments fail
// inside its transaction and counts the attempts.
func failTicketAttachmentInserts(t *testing.T) *atomic.Int32 {
	t.Helper()
	const name = "test:fail_ticket_attachment_insert"
	var attempts atomic.Int32
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "ticket_attachments" {
			attempts.Add(1)
			_ = tx.AddError(errors.New("forced ticket attachment insert failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove(name)) })
	return &attempts
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// Appending a message cleans up the same way: when the transaction fails after
// the attachments were promoted, no file of that request stays in the directory.
func TestAddTicketMessageCompensatesFilesWhenDatabaseCommitFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 9, nil)
	require.NoError(t, err)
	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID, tinyPNG(t)))
	kept := entryNames(mustReadDir(t, dir))
	require.Len(t, kept, 1)

	failed := failTicketAttachmentInserts(t)
	reply := userTicketReply(t, 9, ticket.ID, tinyPNG(t), tinyPNG(t))
	require.Contains(t, reply.Body.String(), `"success":false`)
	require.Positive(t, failed.Load(), "the failure must happen inside the transaction")
	require.ElementsMatch(t, kept, entryNames(mustReadDir(t, dir)))
	requireNoTicketReservations(t)
	messages, err := model.ListTicketMessages(ticket.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2, "the failed reply is not stored")
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	return entries
}

func TestTicketFormCommitFailureRemovesPromotedAndTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	body, contentType := ticketMultipart(t, map[string]string{"content": "hello"}, tinyPNG(t), tinyPNG(t))
	ctx, _ := ticketContext(bytes.NewReader(body), contentType)
	form, err := readSupportTicketForm(ctx)
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
	form, err := readSupportTicketForm(ctx)
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
