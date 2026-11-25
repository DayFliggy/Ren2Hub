package controller

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const maxAnnouncementRequestBodyBytes = 256 * 1024

// optionalScheduleBound distinguishes omitted JSON fields from explicit null/0 clear values.
type optionalScheduleBound struct {
	Set   bool
	Value *int64
}

func (o *optionalScheduleBound) UnmarshalJSON(data []byte) error {
	o.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		zero := int64(0)
		o.Value = &zero
		return nil
	}
	var parsed int64
	if err := common.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	o.Value = &parsed
	return nil
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

type createAnnouncementRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	NotifyMode string `json:"notify_mode"`
	Pinned     bool   `json:"pinned"`
	StartsAt   *int64 `json:"starts_at"`
	EndsAt     *int64 `json:"ends_at"`
}

type updateAnnouncementRequest struct {
	Title      *string               `json:"title"`
	Content    *string               `json:"content"`
	Type       *string               `json:"type"`
	Status     *string               `json:"status"`
	NotifyMode *string               `json:"notify_mode"`
	Pinned     *bool                 `json:"pinned"`
	StartsAt   optionalScheduleBound `json:"starts_at"`
	EndsAt     optionalScheduleBound `json:"ends_at"`
}

func parseAnnouncementID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeAnnouncementError(c, http.StatusBadRequest, "invalid_announcement_id", "Invalid announcement ID")
		return 0, false
	}
	return id, true
}

func writeAnnouncementError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": false,
		"code":    code,
		"message": message,
	})
}

func handleAnnouncementServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrAnnouncementNotFound):
		writeAnnouncementError(c, http.StatusNotFound, "announcement_not_found", "Announcement not found")
	case errors.Is(err, service.ErrAnnouncementInvalidTitle):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_title", "Announcement title must be 1 to 200 characters")
	case errors.Is(err, service.ErrAnnouncementInvalidContent):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_content", "Announcement content must be 1 to 20000 characters")
	case errors.Is(err, service.ErrAnnouncementInvalidType):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_type", "Invalid announcement type")
	case errors.Is(err, service.ErrAnnouncementInvalidStatus):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_status", "Invalid announcement status")
	case errors.Is(err, service.ErrAnnouncementInvalidNotifyMode):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_notify_mode", "Invalid announcement notify mode")
	case errors.Is(err, service.ErrAnnouncementInvalidSchedule):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_schedule", "End time must be later than start time")
	case errors.Is(err, service.ErrAnnouncementInvalidFilter):
		writeAnnouncementError(c, http.StatusBadRequest, "announcement_invalid_filter", "Invalid read status filter")
	default:
		writeAnnouncementError(c, http.StatusInternalServerError, "announcement_internal_error", "Announcement operation failed")
	}
}

func announcementPageQuery(c *gin.Context) (int, int) {
	pageInfo := common.GetPageQuery(c)
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" && strings.TrimSpace(c.Query("p")) == "" {
		if parsed, err := strconv.Atoi(rawPage); err == nil && parsed > 0 {
			pageInfo.Page = parsed
		}
	}
	page := pageInfo.GetPage()
	pageSize := pageInfo.GetPageSize()
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}
	return page, pageSize
}

func parseAnnouncementBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ListUserAnnouncements returns currently active announcements with read status for the signed-in user.
func ListUserAnnouncements(c *gin.Context) {
	userID := c.GetInt("id")
	unreadOnly := parseAnnouncementBoolQuery(c.Query("unread_only"))
	items, unreadCount, err := service.ListActiveAnnouncementsForUser(userID, unreadOnly)
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":        items,
			"unread_count": unreadCount,
		},
	})
}

// MarkUserAnnouncementRead marks one active announcement as read for the signed-in user.
func MarkUserAnnouncementRead(c *gin.Context) {
	id, ok := parseAnnouncementID(c)
	if !ok {
		return
	}
	userID := c.GetInt("id")
	readAt, err := service.MarkAnnouncementRead(id, userID)
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"id":      id,
			"read_at": readAt,
		},
	})
}

// MarkAllUserAnnouncementsRead marks every currently active unread announcement as read for the signed-in user.
func MarkAllUserAnnouncementsRead(c *gin.Context) {
	userID := c.GetInt("id")
	updated, err := service.MarkAllAnnouncementsRead(userID)
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"updated": updated,
		},
	})
}

// ListAdminAnnouncements lists announcements for administrators with filtering and pagination.
func ListAdminAnnouncements(c *gin.Context) {
	page, pageSize := announcementPageQuery(c)
	keyword := truncateRunes(strings.TrimSpace(c.Query("keyword")), 200)
	items, total, err := service.ListAdminAnnouncements(model.AdminAnnouncementFilter{
		Keyword:  keyword,
		Status:   strings.TrimSpace(c.Query("status")),
		Type:     strings.TrimSpace(c.Query("type")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     items,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		},
	})
}

// GetAdminAnnouncement fetches a single announcement with its read count.
func GetAdminAnnouncement(c *gin.Context) {
	id, ok := parseAnnouncementID(c)
	if !ok {
		return
	}
	item, err := service.GetAdminAnnouncement(id)
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    item,
	})
}

// CreateAdminAnnouncement creates a new announcement.
func CreateAdminAnnouncement(c *gin.Context) {
	var req createAnnouncementRequest
	limitedBody := http.MaxBytesReader(c.Writer, c.Request.Body, maxAnnouncementRequestBodyBytes)
	if err := common.DecodeJsonBody(limitedBody, &req); err != nil {
		writeAnnouncementError(c, http.StatusBadRequest, "invalid_request", "Invalid request payload")
		return
	}
	created, err := service.CreateAnnouncement(service.CreateAnnouncementInput{
		Title:      req.Title,
		Content:    req.Content,
		Type:       req.Type,
		Status:     req.Status,
		NotifyMode: req.NotifyMode,
		Pinned:     req.Pinned,
		StartsAt:   req.StartsAt,
		EndsAt:     req.EndsAt,
		ActorID:    c.GetInt("id"),
	})
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    created,
	})
}

// UpdateAdminAnnouncement updates an existing announcement.
func UpdateAdminAnnouncement(c *gin.Context) {
	id, ok := parseAnnouncementID(c)
	if !ok {
		return
	}
	var req updateAnnouncementRequest
	limitedBody := http.MaxBytesReader(c.Writer, c.Request.Body, maxAnnouncementRequestBodyBytes)
	if err := common.DecodeJsonBody(limitedBody, &req); err != nil {
		writeAnnouncementError(c, http.StatusBadRequest, "invalid_request", "Invalid request payload")
		return
	}
	var startsAt *int64
	if req.StartsAt.Set {
		startsAt = req.StartsAt.Value
	}
	var endsAt *int64
	if req.EndsAt.Set {
		endsAt = req.EndsAt.Value
	}
	updated, err := service.UpdateAnnouncement(id, service.UpdateAnnouncementInput{
		Title:      req.Title,
		Content:    req.Content,
		Type:       req.Type,
		Status:     req.Status,
		NotifyMode: req.NotifyMode,
		Pinned:     req.Pinned,
		StartsAt:   startsAt,
		EndsAt:     endsAt,
		ActorID:    c.GetInt("id"),
	})
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    updated,
	})
}

// DeleteAdminAnnouncement deletes an announcement and its read receipts.
func DeleteAdminAnnouncement(c *gin.Context) {
	id, ok := parseAnnouncementID(c)
	if !ok {
		return
	}
	if err := service.DeleteAnnouncement(id); err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"deleted": true,
		},
	})
}

// ListAdminAnnouncementReadStatus lists users and their read receipt timestamp for an announcement.
func ListAdminAnnouncementReadStatus(c *gin.Context) {
	id, ok := parseAnnouncementID(c)
	if !ok {
		return
	}
	page, pageSize := announcementPageQuery(c)
	keyword := truncateRunes(strings.TrimSpace(c.Query("keyword")), 200)
	filter := strings.TrimSpace(c.Query("filter"))
	items, total, totalUsers, readCount, err := service.ListAnnouncementReadStatus(id, keyword, filter, page, pageSize)
	if err != nil {
		handleAnnouncementServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       items,
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_users": totalUsers,
			"read_count":  readCount,
		},
	})
}
