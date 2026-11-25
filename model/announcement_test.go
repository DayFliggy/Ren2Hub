package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementIsActiveAt(t *testing.T) {
	now := int64(1800000000)
	start := now
	future := now + 10
	end := now
	later := now + 10

	assert.False(t, (*Announcement)(nil).IsActiveAt(now))
	assert.False(t, (&Announcement{Status: AnnouncementStatusDraft}).IsActiveAt(now))
	assert.False(t, (&Announcement{Status: AnnouncementStatusArchived}).IsActiveAt(now))
	assert.True(t, (&Announcement{Status: AnnouncementStatusActive}).IsActiveAt(now))
	assert.True(t, (&Announcement{Status: AnnouncementStatusActive, StartsAt: &start, EndsAt: &later}).IsActiveAt(now))
	assert.False(t, (&Announcement{Status: AnnouncementStatusActive, StartsAt: &future}).IsActiveAt(now))
	assert.False(t, (&Announcement{Status: AnnouncementStatusActive, EndsAt: &end}).IsActiveAt(now))
}

func TestAnnouncementDatabaseMatrix(t *testing.T) {
	useScheduledChannelDatabase(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Announcement{}, &AnnouncementRead{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Migrator().DropTable(&AnnouncementRead{}, &Announcement{}))
	})

	now := int64(1800000000)
	past := now - 3600
	future := now + 3600

	u1 := &User{Username: "ann_alice", DisplayName: "Alice", Email: "alice@example.invalid", AffCode: "ANNA", Role: 1, Status: 1}
	u2 := &User{Username: "ann_bob_100%", DisplayName: "Bob", Email: "bob@example.invalid", AffCode: "ANNB", Role: 1, Status: 1}
	uDeleted := &User{Username: "ann_deleted", DisplayName: "Gone", Email: "gone@example.invalid", AffCode: "ANNC", Role: 1, Status: 1}
	require.NoError(t, DB.Create(u1).Error)
	require.NoError(t, DB.Create(u2).Error)
	require.NoError(t, DB.Create(uDeleted).Error)
	require.NoError(t, DB.Delete(uDeleted).Error)

	activePinned := &Announcement{
		Title:      "维护通知 100%_!important",
		Content:    "系统将于今晚升级",
		Type:       AnnouncementTypeWarning,
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		Pinned:     true,
		StartsAt:   &past,
		EndsAt:     &future,
		CreatedAt:  now - 100,
		UpdatedAt:  now - 100,
	}
	activeNormal := &Announcement{
		Title:      "新模型上线",
		Content:    "支持更多模型",
		Type:       AnnouncementTypeSuccess,
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModeSilent,
		Pinned:     false,
		CreatedAt:  now - 50,
		UpdatedAt:  now - 50,
	}
	draftAnn := &Announcement{
		Title:      "草稿公告",
		Content:    "未发布内容",
		Type:       AnnouncementTypeDefault,
		Status:     AnnouncementStatusDraft,
		NotifyMode: AnnouncementNotifyModeSilent,
		CreatedAt:  now - 10,
		UpdatedAt:  now - 10,
	}
	expiredAnn := &Announcement{
		Title:      "过期公告",
		Content:    "已过期",
		Type:       AnnouncementTypeDefault,
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModeSilent,
		EndsAt:     &past,
		CreatedAt:  now - 200,
		UpdatedAt:  now - 200,
	}
	scheduledAnn := &Announcement{
		Title:      "定时公告",
		Content:    "尚未生效",
		Type:       AnnouncementTypeOngoing,
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		StartsAt:   &future,
		CreatedAt:  now - 5,
		UpdatedAt:  now - 5,
	}

	for _, item := range []*Announcement{activePinned, activeNormal, draftAnn, expiredAnn, scheduledAnn} {
		require.NoError(t, CreateAnnouncement(item))
	}

	// Admin list with LIKE wildcard escape (% _ !).
	matched, total, err := ListAdminAnnouncements(AdminAnnouncementFilter{
		Keyword:  "100%_!imp",
		Status:   AnnouncementStatusActive,
		Type:     AnnouncementTypeWarning,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, matched, 1)
	assert.Equal(t, activePinned.ID, matched[0].ID)

	// User active list only sees active announcements inside the time window; pinned first.
	userItems, unreadCount, err := ListActiveAnnouncementsForUser(u1.Id, now, false, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(2), unreadCount)
	require.Len(t, userItems, 2)
	assert.Equal(t, activePinned.ID, userItems[0].ID)
	assert.Equal(t, activeNormal.ID, userItems[1].ID)
	assert.Nil(t, userItems[0].ReadAt)

	// Marking draft / expired / future announcements returns ErrAnnouncementNotFound.
	for _, hiddenID := range []int{draftAnn.ID, expiredAnn.ID, scheduledAnn.ID} {
		_, err := MarkAnnouncementRead(hiddenID, u1.Id, now)
		assert.ErrorIs(t, err, ErrAnnouncementNotFound)
	}

	// Idempotent mark read preserves first read timestamp and updates ordering (unread before read).
	readAt1, err := MarkAnnouncementRead(activePinned.ID, u1.Id, now)
	require.NoError(t, err)
	assert.Equal(t, now, readAt1)
	readAt2, err := MarkAnnouncementRead(activePinned.ID, u1.Id, now+60)
	require.NoError(t, err)
	assert.Equal(t, now, readAt2)

	userItems, unreadCount, err = ListActiveAnnouncementsForUser(u1.Id, now+60, false, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unreadCount)
	require.Len(t, userItems, 2)
	assert.Equal(t, activeNormal.ID, userItems[0].ID)
	assert.Nil(t, userItems[0].ReadAt)
	assert.Equal(t, activePinned.ID, userItems[1].ID)
	require.NotNil(t, userItems[1].ReadAt)
	assert.Equal(t, now, *userItems[1].ReadAt)

	// Read status query excludes soft-deleted users and filters by read/unread/keyword.
	statuses, filteredTotal, totalUsers, readCount, err := ListAnnouncementReadStatus(activePinned.ID, "", "", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), totalUsers)
	assert.Equal(t, int64(1), readCount)
	assert.Equal(t, int64(2), filteredTotal)
	require.Len(t, statuses, 2)
	assert.Equal(t, u1.Id, statuses[0].UserID)
	require.NotNil(t, statuses[0].ReadAt)
	assert.Equal(t, u2.Id, statuses[1].UserID)
	assert.Nil(t, statuses[1].ReadAt)

	unreadOnly, unreadTotal, _, _, err := ListAnnouncementReadStatus(activePinned.ID, "bob_100%", "unread", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), unreadTotal)
	require.Len(t, unreadOnly, 1)
	assert.Equal(t, u2.Id, unreadOnly[0].UserID)

	// MarkAllAnnouncementsRead marks remaining unread items once.
	updated, err := MarkAllAnnouncementsRead(u1.Id, now+120)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)
	updatedAgain, err := MarkAllAnnouncementsRead(u1.Id, now+180)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updatedAgain)

	summary, err := GetAnnouncementByID(activePinned.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.ReadCount)

	// Unchanged same-second UpdateAnnouncement must not falsely report ErrAnnouncementNotFound on MySQL.
	unchanged := summary.Announcement
	require.NoError(t, UpdateAnnouncement(&unchanged))
	updatedSummary, err := UpdateAnnouncementLocked(activePinned.ID, func(record *Announcement) error {
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedSummary.ReadCount)

	// Content exceeding MySQL TEXT's 65,535-byte limit (20,000 3-byte Chinese chars = 60KB + 4-byte emoji > 68KB) persists cleanly across all three databases.
	largeContent := strings.Repeat("测", 15000) + strings.Repeat("🚀", 5000)
	largeAnn := &Announcement{
		Title:      "大内容测试",
		Content:    largeContent,
		Type:       AnnouncementTypeDefault,
		Status:     AnnouncementStatusDraft,
		NotifyMode: AnnouncementNotifyModeSilent,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, CreateAnnouncement(largeAnn))
	fetchedLarge, err := GetAnnouncementByID(largeAnn.ID)
	require.NoError(t, err)
	assert.Equal(t, largeContent, fetchedLarge.Content)
	require.NoError(t, DeleteAnnouncement(largeAnn.ID))

	// Soft-deleting a user who read activePinned keeps GetAnnouncementByID, ListAdminAnnouncements,
	// and ListAnnouncementReadStatus read_count identical.
	uSoft := &User{Username: "ann_soft_reader", DisplayName: "Soft", Email: "soft@example.invalid", AffCode: "ANNS", Role: 1, Status: 1}
	require.NoError(t, DB.Create(uSoft).Error)
	_, err = MarkAnnouncementRead(activePinned.ID, uSoft.Id, now+200)
	require.NoError(t, err)
	beforeSoft, err := GetAnnouncementByID(activePinned.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), beforeSoft.ReadCount)
	require.NoError(t, DB.Delete(uSoft).Error)

	afterSoftDetail, err := GetAnnouncementByID(activePinned.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), afterSoftDetail.ReadCount)
	afterSoftList, _, err := ListAdminAnnouncements(AdminAnnouncementFilter{Keyword: "100%_!imp", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, afterSoftList, 1)
	assert.Equal(t, int64(1), afterSoftList[0].ReadCount)
	_, _, _, afterSoftStatusReads, err := ListAnnouncementReadStatus(activePinned.ID, "", "", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), afterSoftStatusReads)

	// Deleting announcement cascades its read records.
	require.NoError(t, DeleteAnnouncement(activePinned.ID))
	var remainingReads int64
	require.NoError(t, DB.Model(&AnnouncementRead{}).Where("announcement_id = ?", activePinned.ID).Count(&remainingReads).Error)
	assert.Equal(t, int64(0), remainingReads)
	_, err = GetAnnouncementByID(activePinned.ID)
	assert.ErrorIs(t, err, ErrAnnouncementNotFound)
}
