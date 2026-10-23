package controller

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	maxTicketAttachmentSize  = 10 * 1024 * 1024
	maxTicketAttachmentCount = 5
	maxTicketRequestSize     = maxTicketAttachmentCount*maxTicketAttachmentSize + 1024*1024
	maxTicketFieldBytes      = 16 * 1024
	// 临时文件以点开头，与正式存储键（十六进制加扩展名）区分。
	ticketUploadTempPattern = ".upload-*"
)

var ticketAttachmentExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

var renameTicketFile = os.Rename

var ticketFormFields = map[string]bool{
	"title": true, "category": true, "priority": true, "content": true, "model_id": true, "request_id": true,
}

// ticketFormError 是客户端可以修正的表单错误，带稳定的错误码。
type ticketFormError struct {
	message *common.Message
	code    string
}

func (e *ticketFormError) Error() string { return e.message.Error() }

type ticketUpload struct {
	tempPath   string
	finalPath  string
	file       *os.File
	attachment model.TicketAttachment
}

type ticketForm struct {
	fields  map[string]string
	uploads []*ticketUpload
	kept    bool
}

// readTicketForm 流式解析工单表单：读到超出数量或大小的那一段时立即中止，
// 不先缓冲整个请求。附件先写临时文件，进入数据库事务前由 commit 提升。
func readTicketForm(c *gin.Context) (*ticketForm, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTicketRequestSize)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, invalidTicketForm()
	}
	form := &ticketForm{fields: map[string]string{}}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		// 不调用 part.Close：它会读完该段剩余内容；NextPart 已负责跳过，出错时直接停止读取。
		if err != nil {
			err = ticketReadError(err)
		} else {
			err = form.readPart(part)
		}
		if err != nil {
			form.discard()
			return nil, err
		}
	}
}

func (form *ticketForm) value(name string) string { return strings.TrimSpace(form.fields[name]) }

func (form *ticketForm) attachments() []model.TicketAttachment {
	items := make([]model.TicketAttachment, 0, len(form.uploads))
	for _, upload := range form.uploads {
		items = append(items, upload.attachment)
	}
	return items
}

// commit 在进入数据库事务前提升已暂存文件；失败则不创建记录，
// discard 会同时删除临时文件和已提升文件。
func (form *ticketForm) commit() error {
	for _, upload := range form.uploads {
		if err := renameTicketFile(upload.tempPath, upload.finalPath); err != nil {
			form.discard()
			return err
		}
		upload.tempPath = ""
	}
	return nil
}

func (form *ticketForm) keep() {
	form.kept = true
	for _, upload := range form.uploads {
		upload.closeFile()
		upload.tempPath = ""
		upload.finalPath = ""
	}
}

// discard 清理尚未提升的临时文件，以及事务失败时已提升的文件。
func (form *ticketForm) discard() {
	if form.kept {
		return
	}
	for _, upload := range form.uploads {
		removeTicketFile(upload.tempPath)
		removeTicketFile(upload.finalPath)
		upload.closeFile()
		upload.tempPath = ""
		upload.finalPath = ""
	}
}

func (upload *ticketUpload) closeFile() {
	if upload.file == nil {
		return
	}
	if err := upload.file.Close(); err != nil {
		common.SysError(common.LogText("failed to close ticket attachment: %s", err.Error()))
	}
	upload.file = nil
}

// 未知字段和非 attachments 的文件段由 NextPart 跳过，不进入内存。
func (form *ticketForm) readPart(part *multipart.Part) error {
	name := part.FormName()
	if part.FileName() == "" {
		return form.readField(name, part)
	}
	if name != "attachments" {
		return nil
	}
	if len(form.uploads) == maxTicketAttachmentCount {
		return &ticketFormError{common.NewMessage("At most {{count}} attachments are allowed", map[string]any{"count": maxTicketAttachmentCount}), "ATTACHMENT_TOO_MANY"}
	}
	upload, err := stageTicketUpload(part)
	if err != nil {
		return err
	}
	form.uploads = append(form.uploads, upload)
	return nil
}

// readField 与 PostForm 一致，同名字段只取第一个值。
func (form *ticketForm) readField(name string, part *multipart.Part) error {
	if !ticketFormFields[name] {
		return nil
	}
	value, err := io.ReadAll(io.LimitReader(part, maxTicketFieldBytes+1))
	if err != nil {
		return ticketReadError(err)
	}
	if len(value) > maxTicketFieldBytes {
		return &ticketFormError{common.NewMessage("Form field {{name}} is too long", map[string]any{"name": name}), "VALIDATION_ERROR"}
	}
	if _, exists := form.fields[name]; !exists {
		form.fields[name] = string(value)
	}
	return nil
}

func stageTicketUpload(part *multipart.Part) (*ticketUpload, error) {
	name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
	if len([]rune(name)) > 255 {
		return nil, &ticketFormError{common.NewMessage("Attachment filename must not exceed {{count}} characters", map[string]any{"count": 255}), "ATTACHMENT_NAME_TOO_LONG"}
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, ticketReadError(err)
	}
	head = head[:n]
	mimeType := http.DetectContentType(head)
	extension, allowed := ticketAttachmentExtensions[mimeType]
	if !allowed {
		return nil, &ticketFormError{common.NewMessage("Attachment {{name}} has an unsupported image type", map[string]any{"name": name}), "ATTACHMENT_TYPE_UNSUPPORTED"}
	}
	key, err := newTicketStorageKey(extension)
	if err != nil {
		return nil, err
	}
	dir := ticketStorageDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, ticketUploadTempPattern)
	if err != nil {
		return nil, err
	}
	upload := &ticketUpload{tempPath: file.Name(), finalPath: filepath.Join(dir, key), file: file}
	staged := false
	defer func() {
		if !staged {
			removeTicketFile(upload.tempPath)
			upload.closeFile()
		}
	}()
	if err := lockTicketUpload(file); err != nil {
		return nil, err
	}
	size, digest, err := writeTicketUpload(file, head, part)
	if err == nil && size > maxTicketAttachmentSize {
		err = &ticketFormError{common.NewMessage("Attachment {{name}} exceeds 10 MB", map[string]any{"name": name}), "ATTACHMENT_TOO_LARGE"}
	}
	if err != nil {
		return nil, err
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	// Linux retains the inode lock through rename and the DB commit. Other
	// platforms keep the existing closed-file promotion behavior; cleanup is
	// explicitly unavailable there rather than deleting without a lock.
	if !ticketAttachmentCleanupSupported {
		err = file.Close()
		upload.file = nil
		if err != nil {
			return nil, err
		}
	}
	upload.attachment = model.TicketAttachment{StorageKey: key, OriginalName: name, MimeType: mimeType, Size: size, SHA256: digest}
	staged = true
	return upload, nil
}

// writeTicketUpload 最多多读 1 字节，超限即停止读取请求体。
func writeTicketUpload(file *os.File, head []byte, part io.Reader) (int64, string, error) {
	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	if _, err := writer.Write(head); err != nil {
		return 0, "", err
	}
	limit := int64(maxTicketAttachmentSize-len(head)) + 1
	copied, err := io.Copy(writer, io.LimitReader(ticketClientReader{part}, limit))
	if err != nil {
		return 0, "", err
	}
	return int64(len(head)) + copied, hex.EncodeToString(hasher.Sum(nil)), nil
}

// ticketClientReader 把读取请求体的错误标为客户端错误，与写磁盘的错误区分开。
type ticketClientReader struct{ reader io.Reader }

func (r ticketClientReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = ticketReadError(err)
	}
	return n, err
}

func ticketReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &ticketFormError{common.NewMessage("Ticket request exceeds {{size}} MB", map[string]any{"size": maxTicketRequestSize >> 20}), "REQUEST_TOO_LARGE"}
	}
	return invalidTicketForm()
}

func invalidTicketForm() error {
	return &ticketFormError{common.NewMessage("Invalid multipart request"), "VALIDATION_ERROR"}
}

func newTicketStorageKey(extension string) (string, error) {
	keyBytes := make([]byte, 24)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(keyBytes) + extension, nil
}

func removeTicketFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		common.SysError(common.LogText("failed to remove ticket attachment: %s", err.Error()))
	}
}

// respondTicketFormError 区分客户端可修正的表单错误和服务端存储错误。
func respondTicketFormError(c *gin.Context, err error) {
	var formErr *ticketFormError
	if errors.As(err, &formErr) {
		ticketBusinessError(c, formErr.message, formErr.code)
		return
	}
	common.ApiError(c, err)
}

// serveTicketAttachment 按上传时探测的类型返回附件，并禁止浏览器另行嗅探。
func serveTicketAttachment(c *gin.Context, attachment *model.TicketAttachment) {
	path := filepath.Join(ticketStorageDir(), filepath.Base(attachment.StorageKey))
	c.Header("Content-Type", attachment.MimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", attachment.OriginalName))
	c.Header("Cache-Control", "private, max-age=300")
	c.File(path)
}
