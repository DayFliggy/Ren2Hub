package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAnnouncementServiceValidationAndLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldType := model.DB, common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = oldDB
		common.SetMainDatabaseType(oldType)
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Announcement{}, &model.AnnouncementRead{}))

	// Reject invalid title, content, type, status, notify_mode, schedule.
	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "   ", Content: "body"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidTitle)

	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: strings.Repeat("a", MaxAnnouncementTitleRunes+1), Content: "body"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidTitle)

	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "  "})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidContent)

	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "body", Type: "invalid"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidType)

	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "body", Status: "published"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidStatus)

	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "body", NotifyMode: "banner"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidNotifyMode)

	neg := int64(-1)
	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "body", StartsAt: &neg})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)

	start := int64(2000)
	end := int64(2000)
	_, err = CreateAnnouncement(CreateAnnouncementInput{Title: "ok", Content: "body", StartsAt: &start, EndsAt: &end})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)

	// Valid create applies defaults (type=default, status=draft, notify_mode=silent).
	endValid := int64(3000)
	created, err := CreateAnnouncement(CreateAnnouncementInput{
		Title:    "  维护计划  ",
		Content:  "  **升级说明**  ",
		StartsAt: &start,
		EndsAt:   &endValid,
		ActorID:  7,
	})
	require.NoError(t, err)
	assert.Equal(t, "维护计划", created.Title)
	assert.Equal(t, "**升级说明**", created.Content)
	assert.Equal(t, model.AnnouncementTypeDefault, created.Type)
	assert.Equal(t, model.AnnouncementStatusDraft, created.Status)
	assert.Equal(t, model.AnnouncementNotifyModeSilent, created.NotifyMode)
	assert.Equal(t, 7, created.CreatedBy)

	// Partial update changes only supplied fields; 0 clears schedule bounds.
	newStatus := model.AnnouncementStatusActive
	newNotify := model.AnnouncementNotifyModePopup
	newPinned := true
	zero := int64(0)
	updated, err := UpdateAnnouncement(created.ID, UpdateAnnouncementInput{
		Status:     &newStatus,
		NotifyMode: &newNotify,
		Pinned:     &newPinned,
		EndsAt:     &zero,
		ActorID:    9,
	})
	require.NoError(t, err)
	assert.Equal(t, "维护计划", updated.Title)
	assert.Equal(t, model.AnnouncementStatusActive, updated.Status)
	assert.Equal(t, model.AnnouncementNotifyModePopup, updated.NotifyMode)
	assert.True(t, updated.Pinned)
	require.NotNil(t, updated.StartsAt)
	assert.Equal(t, start, *updated.StartsAt)
	assert.Nil(t, updated.EndsAt)
	assert.Equal(t, 9, updated.UpdatedBy)

	// Invalid list filter rejected.
	_, _, err = ListAdminAnnouncements(model.AdminAnnouncementFilter{Status: "unknown"})
	assert.ErrorIs(t, err, ErrAnnouncementInvalidStatus)
	_, _, _, _, err = ListAnnouncementReadStatus(created.ID, "", "invalid", 1, 10)
	assert.ErrorIs(t, err, ErrAnnouncementInvalidFilter)
}
