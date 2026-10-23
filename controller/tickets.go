package controller

import (
	"errors"
	"fmt"
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
	form, err := readTicketForm(c)
	if err != nil {
		respondTicketFormError(c, err)
		return
	}
	defer form.discard()
	title := form.value("title")
	category := form.value("category")
	priority := form.value("priority")
	content := form.value("content")
	if err := validateTicketFields(title, category, priority, content); err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	modelID := form.value("model_id")
	requestID := form.value("request_id")
	if err := validateTicketReferences(modelID, requestID); err != nil {
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
	if err := form.commit(); err != nil {
		respondTicketFormError(c, err)
		return
	}
	if _, err := model.CreateTicket(ticket, content, userID, form.attachments()); err != nil {
		common.ApiError(c, err)
		return
	}
	form.keep()
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
	form, err := readTicketForm(c)
	if err != nil {
		respondTicketFormError(c, err)
		return
	}
	defer form.discard()
	content := form.value("content")
	if content == "" || len([]rune(content)) > 2000 {
		ticketBusinessError(c, common.NewMessage("Ticket content is required and must not exceed 2000 characters"), "VALIDATION_ERROR")
		return
	}
	if err := form.commit(); err != nil {
		respondTicketFormError(c, err)
		return
	}
	message, updatedTicket, err := model.AddTicketMessage(ticketID, userID, content, form.attachments())
	if errors.Is(err, model.ErrTicketClosed) {
		ticketBusinessError(c, common.NewMessage("Closed tickets cannot receive replies"), "TICKET_CLOSED")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	form.keep()
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
	serveTicketAttachment(c, attachment)
}
