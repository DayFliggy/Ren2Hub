package model

import (
	"errors"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AnnouncementTypeDefault = "default"
	AnnouncementTypeOngoing = "ongoing"
	AnnouncementTypeSuccess = "success"
	AnnouncementTypeWarning = "warning"
	AnnouncementTypeError   = "error"

	AnnouncementStatusDraft    = "draft"
	AnnouncementStatusActive   = "active"
	AnnouncementStatusArchived = "archived"

	AnnouncementNotifyModeSilent = "silent"
	AnnouncementNotifyModePopup  = "popup"
)

var (
	ErrAnnouncementNotFound = errors.New("announcement not found")
)

type Announcement struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	Title      string `json:"title" gorm:"type:varchar(200);not null"`
	Content    string `json:"content" gorm:"size:131072;not null"`
	Type       string `json:"type" gorm:"type:varchar(24);not null;default:default;index"`
	Status     string `json:"status" gorm:"type:varchar(24);not null;default:draft;index"`
	NotifyMode string `json:"notify_mode" gorm:"type:varchar(24);not null;default:silent"`
	Pinned     bool   `json:"pinned" gorm:"not null;default:false;index"`
	StartsAt   *int64 `json:"starts_at" gorm:"index"`
	EndsAt     *int64 `json:"ends_at" gorm:"index"`
	CreatedBy  int    `json:"created_by" gorm:"not null;default:0"`
	UpdatedBy  int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt  int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt  int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type AnnouncementRead struct {
	ID             int   `json:"id" gorm:"primaryKey"`
	AnnouncementID int   `json:"announcement_id" gorm:"not null;uniqueIndex:idx_announcement_reads_ann_user,priority:1;index"`
	UserID         int   `json:"user_id" gorm:"not null;uniqueIndex:idx_announcement_reads_ann_user,priority:2;index"`
	ReadAt         int64 `json:"read_at" gorm:"not null;index"`
}

func announcementTable(tx *gorm.DB, name string) string {
	if tx != nil && tx.NamingStrategy != nil {
		return tx.NamingStrategy.TableName(name)
	}
	return name
}

type AnnouncementSummary struct {
	Announcement
	ReadCount int64 `json:"read_count" gorm:"column:read_count"`
}

type UserAnnouncementItem struct {
	Announcement
	ReadAt *int64 `json:"read_at" gorm:"column:read_at"`
}

type AdminAnnouncementFilter struct {
	Keyword  string
	Status   string
	Type     string
	Page     int
	PageSize int
}

type AnnouncementUserReadStatus struct {
	UserID      int    `json:"user_id" gorm:"column:user_id"`
	Username    string `json:"username" gorm:"column:username"`
	DisplayName string `json:"display_name" gorm:"column:display_name"`
	Email       string `json:"email" gorm:"column:email"`
	ReadAt      *int64 `json:"read_at" gorm:"column:read_at"`
}

func (a *Announcement) IsActiveAt(now int64) bool {
	if a == nil || a.Status != AnnouncementStatusActive {
		return false
	}
	if a.StartsAt != nil && now < *a.StartsAt {
		return false
	}
	if a.EndsAt != nil && now >= *a.EndsAt {
		return false
	}
	return true
}

func CreateAnnouncement(a *Announcement) error {
	return DB.Create(a).Error
}

func announcementActiveReadCountSQL(tx *gorm.DB) string {
	readTable := announcementTable(tx, "announcement_reads")
	userTable := announcementTable(tx, "users")
	return "(SELECT COUNT(*) FROM " + readTable + " ar INNER JOIN " + userTable + " u ON u.id = ar.user_id AND u.deleted_at IS NULL WHERE ar.announcement_id = a.id) AS read_count"
}

func getAnnouncementSummaryTx(tx *gorm.DB, id int) (*AnnouncementSummary, error) {
	if id <= 0 {
		return nil, ErrAnnouncementNotFound
	}
	annTable := announcementTable(tx, "announcements")
	var item AnnouncementSummary
	err := tx.Table(annTable+" AS a").
		Select("a.*, "+announcementActiveReadCountSQL(tx)).
		Where("a.id = ?", id).
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAnnouncementNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func GetAnnouncementByID(id int) (*AnnouncementSummary, error) {
	return getAnnouncementSummaryTx(DB, id)
}

func ListAdminAnnouncements(filter AdminAnnouncementFilter) ([]AnnouncementSummary, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if maxPage := math.MaxInt / pageSize; page > maxPage {
		page = maxPage
	}

	annTable := announcementTable(DB, "announcements")
	query := DB.Table(annTable + " AS a")
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("a.status = ?", status)
	}
	if annType := strings.TrimSpace(filter.Type); annType != "" {
		query = query.Where("a.type = ?", annType)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + escapeAnnouncementLike(strings.ToLower(keyword)) + "%"
		query = query.Where("LOWER(a.title) LIKE ? ESCAPE '!' OR LOWER(a.content) LIKE ? ESCAPE '!'", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]AnnouncementSummary, 0)
	err := query.
		Select("a.*, " + announcementActiveReadCountSQL(DB)).
		Order("a.pinned DESC, a.created_at DESC, a.id DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateAnnouncementLocked locks the target row inside a transaction, applies mutate,
// persists all mutable columns, and returns the updated summary. Locking prevents
// lost updates and avoids MySQL returning RowsAffected == 0 on unchanged values.
func UpdateAnnouncementLocked(id int, mutate func(*Announcement) error) (*AnnouncementSummary, error) {
	if id <= 0 {
		return nil, ErrAnnouncementNotFound
	}
	var updated *AnnouncementSummary
	err := DB.Transaction(func(tx *gorm.DB) error {
		var record Announcement
		if err := lockForUpdate(tx.Model(&Announcement{})).Where("id = ?", id).Take(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAnnouncementNotFound
			}
			return err
		}
		if err := mutate(&record); err != nil {
			return err
		}
		if err := tx.Model(&Announcement{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"title":       record.Title,
				"content":     record.Content,
				"type":        record.Type,
				"status":      record.Status,
				"notify_mode": record.NotifyMode,
				"pinned":      record.Pinned,
				"starts_at":   record.StartsAt,
				"ends_at":     record.EndsAt,
				"updated_by":  record.UpdatedBy,
				"updated_at":  record.UpdatedAt,
			}).Error; err != nil {
			return err
		}
		var err error
		updated, err = getAnnouncementSummaryTx(tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func UpdateAnnouncement(a *Announcement) error {
	if a == nil || a.ID <= 0 {
		return ErrAnnouncementNotFound
	}
	_, err := UpdateAnnouncementLocked(a.ID, func(record *Announcement) error {
		record.Title = a.Title
		record.Content = a.Content
		record.Type = a.Type
		record.Status = a.Status
		record.NotifyMode = a.NotifyMode
		record.Pinned = a.Pinned
		record.StartsAt = a.StartsAt
		record.EndsAt = a.EndsAt
		record.UpdatedBy = a.UpdatedBy
		record.UpdatedAt = a.UpdatedAt
		return nil
	})
	return err
}

func DeleteAnnouncement(id int) error {
	if id <= 0 {
		return ErrAnnouncementNotFound
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing Announcement
		if err := lockForUpdate(tx.Model(&Announcement{}).Select("id")).Where("id = ?", id).Take(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAnnouncementNotFound
			}
			return err
		}
		if err := tx.Delete(&Announcement{}, id).Error; err != nil {
			return err
		}
		return tx.Where("announcement_id = ?", id).Delete(&AnnouncementRead{}).Error
	})
}

func activeAnnouncementsScope(tx *gorm.DB, now int64) *gorm.DB {
	annTable := announcementTable(tx, "announcements")
	return tx.Table(annTable+" AS a").
		Where("a.status = ?", AnnouncementStatusActive).
		Where("(a.starts_at IS NULL OR a.starts_at <= ?)", now).
		Where("(a.ends_at IS NULL OR a.ends_at > ?)", now)
}

func ListActiveAnnouncementsForUser(userID int, now int64, unreadOnly bool, limit int) ([]UserAnnouncementItem, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	readTable := announcementTable(DB, "announcement_reads")

	var unreadCount int64
	if err := activeAnnouncementsScope(DB, now).
		Joins("LEFT JOIN "+readTable+" AS ar ON ar.announcement_id = a.id AND ar.user_id = ?", userID).
		Where("ar.read_at IS NULL").
		Count(&unreadCount).Error; err != nil {
		return nil, 0, err
	}

	query := activeAnnouncementsScope(DB, now).
		Select("a.*, ar.read_at AS read_at").
		Joins("LEFT JOIN "+readTable+" AS ar ON ar.announcement_id = a.id AND ar.user_id = ?", userID)
	if unreadOnly {
		query = query.Where("ar.read_at IS NULL")
	}

	items := make([]UserAnnouncementItem, 0)
	err := query.
		Order("CASE WHEN ar.read_at IS NULL THEN 0 ELSE 1 END ASC, a.pinned DESC, a.created_at DESC, a.id DESC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, unreadCount, nil
}

func MarkAnnouncementRead(announcementID, userID int, now int64) (int64, error) {
	if announcementID <= 0 || userID <= 0 {
		return 0, ErrAnnouncementNotFound
	}
	var announcement Announcement
	err := DB.First(&announcement, announcementID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrAnnouncementNotFound
	}
	if err != nil {
		return 0, err
	}
	if !announcement.IsActiveAt(now) {
		return 0, ErrAnnouncementNotFound
	}

	record := AnnouncementRead{
		AnnouncementID: announcementID,
		UserID:         userID,
		ReadAt:         now,
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "announcement_id"}, {Name: "user_id"}},
		DoNothing: true,
	}).Create(&record).Error; err != nil {
		return 0, err
	}

	var stored AnnouncementRead
	if err := DB.Where("announcement_id = ? AND user_id = ?", announcementID, userID).Take(&stored).Error; err != nil {
		return now, err
	}
	return stored.ReadAt, nil
}

func MarkAllAnnouncementsRead(userID int, now int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	readTable := announcementTable(DB, "announcement_reads")
	var unreadIDs []int
	err := activeAnnouncementsScope(DB, now).
		Joins("LEFT JOIN "+readTable+" AS ar ON ar.announcement_id = a.id AND ar.user_id = ?", userID).
		Where("ar.read_at IS NULL").
		Pluck("a.id", &unreadIDs).Error
	if err != nil {
		return 0, err
	}
	if len(unreadIDs) == 0 {
		return 0, nil
	}

	records := make([]AnnouncementRead, 0, len(unreadIDs))
	for _, id := range unreadIDs {
		records = append(records, AnnouncementRead{
			AnnouncementID: id,
			UserID:         userID,
			ReadAt:         now,
		})
	}
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "announcement_id"}, {Name: "user_id"}},
		DoNothing: true,
	}).Create(&records)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func ListAnnouncementReadStatus(announcementID int, keyword, filter string, page, pageSize int) ([]AnnouncementUserReadStatus, int64, int64, int64, error) {
	if announcementID <= 0 {
		return nil, 0, 0, 0, ErrAnnouncementNotFound
	}
	var exists Announcement
	if err := DB.Select("id").First(&exists, announcementID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, 0, 0, ErrAnnouncementNotFound
		}
		return nil, 0, 0, 0, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if maxPage := math.MaxInt / pageSize; page > maxPage {
		page = maxPage
	}

	userTable := announcementTable(DB, "users")
	readTable := announcementTable(DB, "announcement_reads")

	var totalUsers int64
	if err := DB.Table(userTable).Where("deleted_at IS NULL").Count(&totalUsers).Error; err != nil {
		return nil, 0, 0, 0, err
	}
	var readCount int64
	if err := DB.Table(readTable+" AS ar").
		Joins("INNER JOIN "+userTable+" AS u ON u.id = ar.user_id AND u.deleted_at IS NULL").
		Where("ar.announcement_id = ?", announcementID).
		Count(&readCount).Error; err != nil {
		return nil, 0, 0, 0, err
	}

	query := DB.Table(userTable+" AS u").
		Joins("LEFT JOIN "+readTable+" AS ar ON ar.user_id = u.id AND ar.announcement_id = ?", announcementID).
		Where("u.deleted_at IS NULL")

	switch strings.TrimSpace(filter) {
	case "read":
		query = query.Where("ar.read_at IS NOT NULL")
	case "unread":
		query = query.Where("ar.read_at IS NULL")
	}

	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + escapeAnnouncementLike(strings.ToLower(kw)) + "%"
		query = query.Where(
			"LOWER(u.username) LIKE ? ESCAPE '!' OR LOWER(u.display_name) LIKE ? ESCAPE '!' OR LOWER(u.email) LIKE ? ESCAPE '!'",
			like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, 0, 0, err
	}

	items := make([]AnnouncementUserReadStatus, 0)
	err := query.
		Select("u.id AS user_id, u.username AS username, u.display_name AS display_name, u.email AS email, ar.read_at AS read_at").
		Order("CASE WHEN ar.read_at IS NOT NULL THEN 0 ELSE 1 END ASC, ar.read_at DESC, u.id ASC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&items).Error
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, totalUsers, readCount, nil
}

func escapeAnnouncementLike(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

func deleteUserAnnouncementReads(tx *gorm.DB, userID int) error {
	if userID <= 0 || !tx.Migrator().HasTable(&AnnouncementRead{}) {
		return nil
	}
	return tx.Where("user_id = ?", userID).Delete(&AnnouncementRead{}).Error
}
