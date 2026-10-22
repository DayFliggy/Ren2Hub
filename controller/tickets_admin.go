package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	serviceauthz "github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminTicketUserDTO struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
}

type adminTicketDTO struct {
	ticketDTO
	AssigneeID   *int               `json:"assignee_id"`
	AssigneeName string             `json:"assignee_name,omitempty"`
	AssignedAt   int64              `json:"assigned_at"`
	User         adminTicketUserDTO `json:"user"`
	// UserDeleted marks a requester that was soft- or hard-deleted; the ticket
	// stays readable and closable.
	UserDeleted bool `json:"user_deleted"`
}

func buildAdminTicketSummaryDTO(summary *model.TicketSummary) adminTicketDTO {
	return adminTicketDTO{
		ticketDTO:    buildTicketSummaryDTO(summary),
		AssigneeID:   summary.AssigneeID,
		AssigneeName: summary.AssigneeName,
		AssignedAt:   summary.AssignedAt,
		User: adminTicketUserDTO{
			ID:          summary.UserID,
			Username:    summary.UserName,
			DisplayName: summary.UserDisplayName,
		},
		UserDeleted: summary.UserDeleted,
	}
}

// buildAdminTicketDTO accepts a nil owner for a hard-deleted requester.
func buildAdminTicketDTO(ticket *model.Ticket, messages []model.TicketMessage, owner *model.User, assigneeName string) adminTicketDTO {
	result := adminTicketDTO{
		ticketDTO:    buildTicketDTO(ticket, messages),
		AssigneeID:   ticket.AssigneeID,
		AssigneeName: assigneeName,
		AssignedAt:   ticket.AssignedAt,
		User:         adminTicketUserDTO{ID: ticket.UserID},
		UserDeleted:  model.OwnerDeleted(owner),
	}
	if owner != nil {
		result.User.Username = owner.Username
		result.User.DisplayName = owner.DisplayName
		result.User.Email = owner.Email
	}
	return result
}

func ListAdminTickets(c *gin.Context) {
	page := 1
	pageSize := 20
	if value, err := strconv.Atoi(c.Query("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(c.Query("page_size")); err == nil && value > 0 && value <= 100 {
		pageSize = value
	}
	status := strings.TrimSpace(c.Query("status"))
	category := strings.TrimSpace(c.Query("category"))
	priority := strings.TrimSpace(c.Query("priority"))
	if status != "" && !model.IsTicketStatus(status) {
		ticketBusinessError(c, common.NewMessage("Invalid ticket status"), "VALIDATION_ERROR")
		return
	}
	if category != "" && !validTicketCategory(category) {
		ticketBusinessError(c, common.NewMessage("Invalid ticket category"), "VALIDATION_ERROR")
		return
	}
	if priority != "" && !validTicketPriority(priority) {
		ticketBusinessError(c, common.NewMessage("Invalid ticket priority"), "VALIDATION_ERROR")
		return
	}
	filter := model.AdminTicketFilter{
		Keyword:  c.Query("keyword"),
		Status:   status,
		Category: category,
		Priority: priority,
		Page:     page,
		PageSize: pageSize,
	}
	assignee := strings.TrimSpace(c.Query("assignee"))
	if assignee != "" {
		assigneeID := 0
		switch assignee {
		case "unassigned":
		case "mine":
			assigneeID = c.GetInt("id")
		default:
			parsed, err := strconv.Atoi(assignee)
			if err != nil || parsed <= 0 {
				ticketBusinessError(c, common.NewMessage("Invalid ticket assignee"), "VALIDATION_ERROR")
				return
			}
			assigneeID = parsed
		}
		filter.AssigneeID = &assigneeID
	}
	items, total, err := model.ListAdminTickets(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]adminTicketDTO, 0, len(items))
	for index := range items {
		result = append(result, buildAdminTicketSummaryDTO(&items[index]))
	}
	common.ApiSuccess(c, gin.H{"items": result, "total": total, "page": page, "page_size": pageSize})
}

func GetAdminTicketSummary(c *gin.Context) {
	summary, err := model.GetTicketQueueSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func ListAdminTicketAgents(c *gin.Context) {
	users, err := model.ListEnabledTicketAgents()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	agents := make([]adminTicketUserDTO, 0, len(users))
	for index := range users {
		user := &users[index]
		if !serviceauthz.Can(user.Id, user.Role, serviceauthz.TicketReply) {
			continue
		}
		agents = append(agents, adminTicketUserDTO{
			ID:          user.Id,
			Username:    user.Username,
			DisplayName: user.DisplayName,
		})
	}
	common.ApiSuccess(c, gin.H{"items": agents})
}

func GetAdminTicket(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	ticket, err := model.GetTicket(ticketID)
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
	owner, err := model.GetOwnerIncludingDeleted(ticket.UserID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	assigneeName := ""
	if ticket.AssigneeID != nil {
		if assignee, getErr := model.GetUserById(*ticket.AssigneeID, false); getErr == nil {
			assigneeName = assignee.Username
		}
	}
	common.ApiSuccess(c, gin.H{
		"ticket":   buildAdminTicketDTO(ticket, messages, owner, assigneeName),
		"messages": buildTicketMessages(messages, attachments, "/api/admin/tickets/attachments"),
	})
}

func AddAdminTicketMessage(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	before, err := model.GetTicket(ticketID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
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
	message, updatedTicket, err := model.AddSupportTicketMessage(ticketID, c.GetInt("id"), content, form.attachments())
	if err != nil {
		if errors.Is(err, model.ErrTicketClosed) {
			ticketBusinessError(c, common.NewMessage("Closed tickets cannot receive replies"), "TICKET_CLOSED")
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ticketNotFound(c)
			return
		}
		common.ApiError(c, err)
		return
	}
	storeErr := form.commit()
	recordManageAudit(c, "ticket.reply", map[string]interface{}{
		"ticket_id":     ticketID,
		"auto_assigned": before.AssigneeID == nil,
	})
	service.NotifyTicketSupportReply(*updatedTicket)
	if storeErr != nil {
		respondTicketStorageFailure(c, ticketID, storeErr)
		return
	}
	storedAttachments, err := model.ListTicketAttachments(ticketID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := buildTicketMessages([]model.TicketMessage{*message}, storedAttachments, "/api/admin/tickets/attachments")
	common.ApiSuccess(c, gin.H{"message": items[0], "ticket": gin.H{
		"status":      updatedTicket.Status,
		"assignee_id": updatedTicket.AssigneeID,
		"assigned_at": updatedTicket.AssignedAt,
		"updated":     updatedTicket.UpdatedAt,
	}})
}

func UpdateAdminTicketStatus(c *gin.Context) {
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
	ticket, err := model.UpdateAdminTicketStatus(ticketID, c.GetInt("id"), request.Status)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		ticketBusinessError(c, err, "VALIDATION_ERROR")
		return
	}
	recordManageAudit(c, "ticket.status_update", map[string]interface{}{"ticket_id": ticketID, "status": ticket.Status})
	common.ApiSuccess(c, gin.H{"ticket": gin.H{"id": ticket.ID, "status": ticket.Status, "updated": ticket.UpdatedAt}})
}

func AssignAdminTicket(c *gin.Context) {
	ticketID, ok := ticketID(c)
	if !ok {
		return
	}
	var request struct {
		AssigneeID *int `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		ticketBusinessError(c, common.NewMessage("Invalid ticket assignee"), "VALIDATION_ERROR")
		return
	}
	if request.AssigneeID != nil {
		if *request.AssigneeID <= 0 {
			ticketBusinessError(c, common.NewMessage("Invalid ticket assignee"), "VALIDATION_ERROR")
			return
		}
		assignee, err := model.GetUserById(*request.AssigneeID, false)
		if err != nil || assignee.Status != common.UserStatusEnabled || !serviceauthz.Can(assignee.Id, assignee.Role, serviceauthz.TicketReply) {
			ticketBusinessError(c, common.NewMessage("Ticket assignee is not available"), "VALIDATION_ERROR")
			return
		}
	}
	ticket, changed, err := model.AssignTicketWithChange(ticketID, request.AssigneeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticketNotFound(c)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params := map[string]interface{}{"ticket_id": ticketID}
	if request.AssigneeID != nil {
		params["assignee_id"] = *request.AssigneeID
	} else {
		params["assignee_id"] = nil
	}
	if changed {
		recordManageAudit(c, "ticket.assign", params)
	}
	if changed && request.AssigneeID != nil {
		service.NotifyTicketAssignment(*ticket, *request.AssigneeID)
	}
	common.ApiSuccess(c, gin.H{"ticket": gin.H{
		"id":          ticket.ID,
		"assignee_id": ticket.AssigneeID,
		"assigned_at": ticket.AssignedAt,
		"updated":     ticket.UpdatedAt,
	}})
}

func DownloadAdminTicketAttachment(c *gin.Context) {
	attachmentID, err := strconv.Atoi(c.Param("attachment_id"))
	if err != nil || attachmentID <= 0 {
		ticketBusinessError(c, common.NewMessage("Invalid attachment ID"), "VALIDATION_ERROR")
		return
	}
	attachment, err := model.GetTicketAttachment(attachmentID)
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
