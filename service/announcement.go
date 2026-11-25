package service

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/model"
)

const (
	MaxAnnouncementTitleRunes   = 200
	MaxAnnouncementContentRunes = 20000
)

var (
	ErrAnnouncementInvalidTitle      = errors.New("announcement_invalid_title")
	ErrAnnouncementInvalidContent    = errors.New("announcement_invalid_content")
	ErrAnnouncementInvalidType       = errors.New("announcement_invalid_type")
	ErrAnnouncementInvalidStatus     = errors.New("announcement_invalid_status")
	ErrAnnouncementInvalidNotifyMode = errors.New("announcement_invalid_notify_mode")
	ErrAnnouncementInvalidSchedule   = errors.New("announcement_invalid_schedule")
	ErrAnnouncementInvalidFilter     = errors.New("announcement_invalid_filter")
)

type CreateAnnouncementInput struct {
	Title      string
	Content    string
	Type       string
	Status     string
	NotifyMode string
	Pinned     bool
	StartsAt   *int64
	EndsAt     *int64
	ActorID    int
}

type UpdateAnnouncementInput struct {
	Title      *string
	Content    *string
	Type       *string
	Status     *string
	NotifyMode *string
	Pinned     *bool
	StartsAt   *int64 // nil = unchanged, 0 = clear, >0 = set
	EndsAt     *int64 // nil = unchanged, 0 = clear, >0 = set
	ActorID    int
}

func IsValidAnnouncementType(value string) bool {
	switch value {
	case model.AnnouncementTypeDefault,
		model.AnnouncementTypeOngoing,
		model.AnnouncementTypeSuccess,
		model.AnnouncementTypeWarning,
		model.AnnouncementTypeError:
		return true
	default:
		return false
	}
}

func IsValidAnnouncementStatus(value string) bool {
	switch value {
	case model.AnnouncementStatusDraft,
		model.AnnouncementStatusActive,
		model.AnnouncementStatusArchived:
		return true
	default:
		return false
	}
}

func IsValidAnnouncementNotifyMode(value string) bool {
	switch value {
	case model.AnnouncementNotifyModeSilent,
		model.AnnouncementNotifyModePopup:
		return true
	default:
		return false
	}
}

func normalizeScheduleBound(v *int64) (*int64, error) {
	if v == nil || *v == 0 {
		return nil, nil
	}
	if *v < 0 {
		return nil, ErrAnnouncementInvalidSchedule
	}
	ts := *v
	return &ts, nil
}

func validateSchedule(startsAt, endsAt *int64) error {
	if startsAt != nil && endsAt != nil && *startsAt >= *endsAt {
		return ErrAnnouncementInvalidSchedule
	}
	return nil
}

func CreateAnnouncement(input CreateAnnouncementInput) (*model.AnnouncementSummary, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || utf8.RuneCountInString(title) > MaxAnnouncementTitleRunes {
		return nil, ErrAnnouncementInvalidTitle
	}
	content := strings.TrimSpace(input.Content)
	if content == "" || utf8.RuneCountInString(content) > MaxAnnouncementContentRunes {
		return nil, ErrAnnouncementInvalidContent
	}

	annType := strings.TrimSpace(input.Type)
	if annType == "" {
		annType = model.AnnouncementTypeDefault
	}
	if !IsValidAnnouncementType(annType) {
		return nil, ErrAnnouncementInvalidType
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = model.AnnouncementStatusDraft
	}
	if !IsValidAnnouncementStatus(status) {
		return nil, ErrAnnouncementInvalidStatus
	}

	notifyMode := strings.TrimSpace(input.NotifyMode)
	if notifyMode == "" {
		notifyMode = model.AnnouncementNotifyModeSilent
	}
	if !IsValidAnnouncementNotifyMode(notifyMode) {
		return nil, ErrAnnouncementInvalidNotifyMode
	}

	startsAt, err := normalizeScheduleBound(input.StartsAt)
	if err != nil {
		return nil, err
	}
	endsAt, err := normalizeScheduleBound(input.EndsAt)
	if err != nil {
		return nil, err
	}
	if err := validateSchedule(startsAt, endsAt); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	record := &model.Announcement{
		Title:      title,
		Content:    content,
		Type:       annType,
		Status:     status,
		NotifyMode: notifyMode,
		Pinned:     input.Pinned,
		StartsAt:   startsAt,
		EndsAt:     endsAt,
		CreatedBy:  input.ActorID,
		UpdatedBy:  input.ActorID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := model.CreateAnnouncement(record); err != nil {
		return nil, err
	}
	return &model.AnnouncementSummary{Announcement: *record, ReadCount: 0}, nil
}

func UpdateAnnouncement(id int, input UpdateAnnouncementInput) (*model.AnnouncementSummary, error) {
	return model.UpdateAnnouncementLocked(id, func(record *model.Announcement) error {
		if input.Title != nil {
			title := strings.TrimSpace(*input.Title)
			if title == "" || utf8.RuneCountInString(title) > MaxAnnouncementTitleRunes {
				return ErrAnnouncementInvalidTitle
			}
			record.Title = title
		}
		if input.Content != nil {
			content := strings.TrimSpace(*input.Content)
			if content == "" || utf8.RuneCountInString(content) > MaxAnnouncementContentRunes {
				return ErrAnnouncementInvalidContent
			}
			record.Content = content
		}
		if input.Type != nil {
			annType := strings.TrimSpace(*input.Type)
			if !IsValidAnnouncementType(annType) {
				return ErrAnnouncementInvalidType
			}
			record.Type = annType
		}
		if input.Status != nil {
			status := strings.TrimSpace(*input.Status)
			if !IsValidAnnouncementStatus(status) {
				return ErrAnnouncementInvalidStatus
			}
			record.Status = status
		}
		if input.NotifyMode != nil {
			notifyMode := strings.TrimSpace(*input.NotifyMode)
			if !IsValidAnnouncementNotifyMode(notifyMode) {
				return ErrAnnouncementInvalidNotifyMode
			}
			record.NotifyMode = notifyMode
		}
		if input.Pinned != nil {
			record.Pinned = *input.Pinned
		}
		if input.StartsAt != nil {
			startsAt, err := normalizeScheduleBound(input.StartsAt)
			if err != nil {
				return err
			}
			record.StartsAt = startsAt
		}
		if input.EndsAt != nil {
			endsAt, err := normalizeScheduleBound(input.EndsAt)
			if err != nil {
				return err
			}
			record.EndsAt = endsAt
		}
		if err := validateSchedule(record.StartsAt, record.EndsAt); err != nil {
			return err
		}

		record.UpdatedBy = input.ActorID
		record.UpdatedAt = time.Now().Unix()
		return nil
	})
}

func ListAdminAnnouncements(filter model.AdminAnnouncementFilter) ([]model.AnnouncementSummary, int64, error) {
	if s := strings.TrimSpace(filter.Status); s != "" && !IsValidAnnouncementStatus(s) {
		return nil, 0, ErrAnnouncementInvalidStatus
	}
	if t := strings.TrimSpace(filter.Type); t != "" && !IsValidAnnouncementType(t) {
		return nil, 0, ErrAnnouncementInvalidType
	}
	return model.ListAdminAnnouncements(filter)
}

func GetAdminAnnouncement(id int) (*model.AnnouncementSummary, error) {
	return model.GetAnnouncementByID(id)
}

func DeleteAnnouncement(id int) error {
	return model.DeleteAnnouncement(id)
}

func ListAnnouncementReadStatus(id int, keyword, filter string, page, pageSize int) ([]model.AnnouncementUserReadStatus, int64, int64, int64, error) {
	cleanFilter := strings.TrimSpace(filter)
	if cleanFilter != "" && cleanFilter != "read" && cleanFilter != "unread" {
		return nil, 0, 0, 0, ErrAnnouncementInvalidFilter
	}
	return model.ListAnnouncementReadStatus(id, keyword, cleanFilter, page, pageSize)
}

func ListActiveAnnouncementsForUser(userID int, unreadOnly bool) ([]model.UserAnnouncementItem, int64, error) {
	return model.ListActiveAnnouncementsForUser(userID, time.Now().Unix(), unreadOnly, 50)
}

func MarkAnnouncementRead(announcementID, userID int) (int64, error) {
	return model.MarkAnnouncementRead(announcementID, userID, time.Now().Unix())
}

func MarkAllAnnouncementsRead(userID int) (int64, error) {
	return model.MarkAllAnnouncementsRead(userID, time.Now().Unix())
}
