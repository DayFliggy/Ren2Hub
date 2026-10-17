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
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxTicketAttachmentSize  = 10 * 1024 * 1024
	maxTicketAttachmentCount = 5
	maxTicketRequestSize     = maxTicketAttachmentCount*maxTicketAttachmentSize + 1024*1024
)

var ticketAttachmentExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type ticketDTO struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Category      string `json:"category"`
	Priority      string `json:"priority"`
	Status        string `json:"status"`
	ReplyCount    int    `json:"reply_count"`
	MessageCount  int    `json:"message_count"`
	LastReplyRole string `json:"last_reply_role"`
	ModelID       string `json:"model_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	Created       int64  `json:"created"`
	Updated       int64  `json:"updated"`
}

type ticketMessageDTO struct {
	ID         int      `json:"id"`
	Role       string   `json:"role"`
	Department string   `json:"department,omitempty"`
	Content    string   `json:"content"`
	Images     []string `json:"images"`
	Created    int64    `json:"created"`
}

func ticketStorageDir() string {
	dir := strings.TrimSpace(os.Getenv("TICKET_UPLOAD_DIR"))
	if dir == "" {
		return filepath.Join("data", "ticket-attachments")
	}
	return dir
}

func ticketID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		ticketBusinessError(c, common.NewMessage("Invalid ticket ID"), "VALIDATION_ERROR")
		return 0, false
	}
	return id, true
}

// ticketBusinessError answers a business failure with a stable error code. Pass a
// *common.Message (common.NewMessage) for text the web console translates; a
// plain error is sent as written, without a message_key.
func ticketBusinessError(c *gin.Context, err error, code string) {
	common.ApiErrorStatus(c, http.StatusOK, err, gin.H{"code": code})
}

func ticketNotFound(c *gin.Context) {
	ticketBusinessError(c, common.NewMessage("Ticket not found"), "NOT_FOUND")
}

func ticketRole(message model.TicketMessage) string {
	if message.Role == model.TicketMessageRoleSupport {
		return model.TicketMessageRoleSupport
	}
	return model.TicketMessageRoleUser
}

func buildTicketDTO(ticket *model.Ticket, messages []model.TicketMessage) ticketDTO {
	lastRole := model.TicketMessageRoleUser
	if len(messages) > 0 {
		lastRole = ticketRole(messages[len(messages)-1])
	}
	return ticketDTO{
		ID:            ticket.ID,
		Title:         ticket.Title,
		Category:      ticket.Category,
		Priority:      ticket.Priority,
		Status:        ticket.Status,
		ReplyCount:    len(messages),
		MessageCount:  len(messages),
		LastReplyRole: lastRole,
		ModelID:       ticket.ModelID,
		RequestID:     ticket.RequestID,
		Created:       ticket.CreatedAt,
		Updated:       ticket.UpdatedAt,
	}
}

func buildTicketSummaryDTO(summary *model.TicketSummary) ticketDTO {
	return ticketDTO{
		ID:            summary.ID,
		Title:         summary.Title,
		Category:      summary.Category,
		Priority:      summary.Priority,
		Status:        summary.Status,
		ReplyCount:    summary.MessageCount,
		MessageCount:  summary.MessageCount,
		LastReplyRole: summary.LastReplyRole,
		ModelID:       summary.ModelID,
		RequestID:     summary.RequestID,
		Created:       summary.CreatedAt,
		Updated:       summary.UpdatedAt,
	}
}

func buildTicketMessages(messages []model.TicketMessage, attachments []model.TicketAttachment, attachmentBase string) []ticketMessageDTO {
	imagesByMessage := make(map[int][]string)
	for _, attachment := range attachments {
		imagesByMessage[attachment.MessageID] = append(imagesByMessage[attachment.MessageID], fmt.Sprintf("%s/%d", attachmentBase, attachment.ID))
	}
	items := make([]ticketMessageDTO, 0, len(messages))
	for _, message := range messages {
		role := ticketRole(message)
		item := ticketMessageDTO{
			ID:      message.ID,
			Role:    role,
			Content: message.Content,
			Images:  imagesByMessage[message.ID],
			Created: message.CreatedAt,
		}
		if item.Images == nil {
			item.Images = []string{}
		}
		if role == "support" {
			item.Department = "support"
		}
		items = append(items, item)
	}
	return items
}

func validateTicketFields(title, category, priority, content string) error {
	if title == "" || len([]rune(title)) > 100 {
		return common.NewMessage("Ticket title is required and must not exceed 100 characters")
	}
	if content == "" || len([]rune(content)) > 2000 {
		return common.NewMessage("Ticket content is required and must not exceed 2000 characters")
	}
	if !validTicketCategory(category) || !validTicketPriority(priority) {
		return common.NewMessage("Invalid ticket category or priority")
	}
	return nil
}

func validTicketCategory(value string) bool {
	return value == "billing" || value == "api" || value == "model" || value == "account" || value == "other"
}

func validTicketPriority(value string) bool {
	return value == "low" || value == "normal" || value == "high"
}

func validateTicketReferences(modelID, requestID string) error {
	if len([]rune(modelID)) > 128 {
		return common.NewMessage("Model ID must not exceed 128 characters")
	}
	if len([]rune(requestID)) > 128 {
		return common.NewMessage("Request ID must not exceed 128 characters")
	}
	return nil
}

func saveTicketAttachments(headers []*multipart.FileHeader) ([]model.TicketAttachment, []string, error) {
	if len(headers) > maxTicketAttachmentCount {
		return nil, nil, common.NewMessage("At most {{count}} attachments are allowed", map[string]any{"count": maxTicketAttachmentCount})
	}
	if len(headers) == 0 {
		return nil, nil, nil
	}
	dir := ticketStorageDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	attachments := make([]model.TicketAttachment, 0, len(headers))
	paths := make([]string, 0, len(headers))
	for _, header := range headers {
		if header.Size > maxTicketAttachmentSize {
			removeTicketFiles(paths)
			return nil, nil, common.NewMessage("Attachment {{name}} exceeds 10 MB", map[string]any{"name": filepath.Base(header.Filename)})
		}
		file, err := header.Open()
		if err != nil {
			removeTicketFiles(paths)
			return nil, nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxTicketAttachmentSize+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			removeTicketFiles(paths)
			if readErr != nil {
				return nil, nil, readErr
			}
			return nil, nil, closeErr
		}
		if len(content) > maxTicketAttachmentSize {
			removeTicketFiles(paths)
			return nil, nil, common.NewMessage("Attachment {{name}} exceeds 10 MB", map[string]any{"name": filepath.Base(header.Filename)})
		}
		mimeType := http.DetectContentType(content)
		extension, allowed := ticketAttachmentExtensions[mimeType]
		if !allowed {
			removeTicketFiles(paths)
			return nil, nil, common.NewMessage("Attachment {{name}} has an unsupported image type", map[string]any{"name": filepath.Base(header.Filename)})
		}
		keyBytes := make([]byte, 24)
		if _, err := rand.Read(keyBytes); err != nil {
			removeTicketFiles(paths)
			return nil, nil, err
		}
		storageKey := hex.EncodeToString(keyBytes) + extension
		path := filepath.Join(dir, storageKey)
		if err := os.WriteFile(path, content, 0600); err != nil {
			removeTicketFiles(paths)
			return nil, nil, err
		}
		digest := sha256.Sum256(content)
		paths = append(paths, path)
		attachments = append(attachments, model.TicketAttachment{
			StorageKey:   storageKey,
			OriginalName: filepath.Base(header.Filename),
			MimeType:     mimeType,
			Size:         int64(len(content)),
			SHA256:       hex.EncodeToString(digest[:]),
		})
	}
	return attachments, paths, nil
}

func removeTicketFiles(paths []string) {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			common.SysError(common.LogText("failed to remove ticket attachment: %s", err.Error()))
		}
	}
}

func ListTickets(c *gin.Context) {
	page := 1
	pageSize := 10
	if value, err := strconv.Atoi(c.Query("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(c.Query("page_size")); err == nil && value > 0 && value <= 100 {
		pageSize = value
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsTicketStatus(status) {
		ticketBusinessError(c, common.NewMessage("Invalid ticket status"), "VALIDATION_ERROR")
		return
	}
	tickets, total, err := model.ListUserTickets(c.GetInt("id"), c.Query("keyword"), status, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]ticketDTO, 0, len(tickets))
	for index := range tickets {
		items = append(items, buildTicketSummaryDTO(&tickets[index]))
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func CreateTicket(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTicketRequestSize)
	form, err := c.MultipartForm()
	if err != nil {
		ticketBusinessError(c, common.NewMessage("Invalid multipart request"), "VALIDATION_ERROR")
		return
	}
	defer form.RemoveAll()
	title := strings.TrimSpace(c.PostForm("title"))
	category := strings.TrimSpace(c.PostForm("category"))
	priority := strings.TrimSpace(c.PostForm("priority"))
	content := strings.TrimSpace(c.PostForm("content"))
	if err := validateTicketFields(title, category, priority, content); err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	modelID := strings.TrimSpace(c.PostForm("model_id"))
	requestID := strings.TrimSpace(c.PostForm("request_id"))
	if err := validateTicketReferences(modelID, requestID); err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	attachments, paths, err := saveTicketAttachments(form.File["attachments"])
	if err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	userID := c.GetInt("id")
	ticket := &model.Ticket{
		UserID:    userID,
		Title:     title,
		Category:  category,
		Priority:  priority,
		ModelID:   modelID,
		RequestID: requestID,
	}
	if _, err := model.CreateTicket(ticket, content, userID, attachments); err != nil {
		removeTicketFiles(paths)
		common.ApiError(c, err)
		return
	}
	service.NotifyNewTicket(*ticket)
	messages, err := model.ListTicketMessages(ticket.ID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"ticket": buildTicketDTO(ticket, messages)})
}

func GetTicket(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	ticket, err := model.GetUserTicket(c.GetInt("id"), ticketID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	messages, err := model.ListTicketMessages(ticket.ID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	attachments, err := model.ListTicketAttachments(ticket.ID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"ticket":   buildTicketDTO(ticket, messages),
		"messages": buildTicketMessages(messages, attachments, "/api/tickets/attachments"),
	})
}

func AddTicketMessage(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	userID := c.GetInt("id")
	ticket, err := model.GetUserTicket(userID, ticketID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if ticket.Status == model.TicketStatusClosed {
		ticketBusinessError(c, common.NewMessage("Closed tickets cannot receive replies"), "TICKET_CLOSED")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTicketRequestSize)
	form, err := c.MultipartForm()
	if err != nil {
		ticketBusinessError(c, common.NewMessage("Invalid multipart request"), "VALIDATION_ERROR")
		return
	}
	defer form.RemoveAll()
	content := strings.TrimSpace(c.PostForm("content"))
	if content == "" || len([]rune(content)) > 2000 {
		ticketBusinessError(c, common.NewMessage("Ticket content is required and must not exceed 2000 characters"), "VALIDATION_ERROR")
		return
	}
	attachments, paths, err := saveTicketAttachments(form.File["attachments"])
	if err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	message, updatedTicket, err := model.AddTicketMessage(ticketID, userID, content, attachments)
	if errors.Is(err, model.ErrTicketClosed) {
		removeTicketFiles(paths)
		ticketBusinessError(c, common.NewMessage("Closed tickets cannot receive replies"), "TICKET_CLOSED")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		removeTicketFiles(paths)
		ticketNotFound(c)
		return
	}
	if err != nil {
		removeTicketFiles(paths)
		common.ApiError(c, err)
		return
	}
	service.NotifyTicketUserReply(*updatedTicket)
	storedAttachments, err := model.ListTicketAttachments(ticketID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := buildTicketMessages([]model.TicketMessage{*message}, storedAttachments, "/api/tickets/attachments")
	common.ApiSuccess(c, gin.H{"message": items[0]})
}

func UpdateTicketStatus(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	var request struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		ticketBusinessError(c, common.NewMessage("Invalid ticket status"), "VALIDATION_ERROR")
		return
	}
	ticket, err := model.UpdateTicketStatus(ticketID, c.GetInt("id"), request.Status)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	messages, err := model.ListTicketMessages(ticket.ID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"ticket": buildTicketDTO(ticket, messages)})
}

func DownloadTicketAttachment(c *gin.Context) {
	attachmentID, err := strconv.Atoi(c.Param("attachment_id"))
	if err != nil || attachmentID <= 0 {
		ticketBusinessError(c, common.NewMessage("Invalid attachment ID"), "VALIDATION_ERROR")
		return
	}
	attachment, err := model.GetUserTicketAttachment(c.GetInt("id"), attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	path := filepath.Join(ticketStorageDir(), filepath.Base(attachment.StorageKey))
	c.Header("Content-Type", attachment.MimeType)
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", attachment.OriginalName))
	c.Header("Cache-Control", "private, max-age=300")
	c.File(path)
}
