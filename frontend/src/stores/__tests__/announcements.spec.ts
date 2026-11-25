import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { announcementsApi, type UserAnnouncement } from '@/api/announcements'
import { useAnnouncementStore } from '@/stores/announcements'
import { useAuthStore } from '@/stores/auth'

function sampleAnnouncement(
  overrides: Partial<UserAnnouncement> = {}
): UserAnnouncement {
  return {
    id: 1,
    title: 'Maintenance Notice',
    content: 'Scheduled update tonight.',
    type: 'warning',
    status: 'active',
    notify_mode: 'silent',
    pinned: false,
    starts_at: null,
    ends_at: null,
    created_by: 1,
    updated_by: 1,
    created_at: 1785120000,
    updated_at: 1785120000,
    read_at: null,
    ...overrides,
  }
}

describe('useAnnouncementStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useAuthStore().persist({
      id: 42,
      username: 'member',
      display_name: 'Member',
      email: 'member@example.com',
      role: 1,
      status: 1,
      quota: 0,
      used_quota: 0,
      request_count: 0,
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('queues unread popup announcements once per session and advances on dismiss', async () => {
    const popupA = sampleAnnouncement({ id: 10, notify_mode: 'popup' })
    const popupB = sampleAnnouncement({ id: 11, notify_mode: 'popup' })
    const silent = sampleAnnouncement({ id: 12, notify_mode: 'silent' })
    vi.spyOn(announcementsApi, 'listForUser').mockResolvedValue({
      items: [popupA, popupB, silent],
      unread_count: 3,
    })
    const markSpy = vi
      .spyOn(announcementsApi, 'markRead')
      .mockResolvedValue({ id: 10, read_at: 1785123456 })

    const store = useAnnouncementStore()
    await store.fetchAnnouncements(true)

    expect(store.unreadCount).toBe(3)
    expect(store.currentPopup?.id).toBe(10)

    await store.dismissPopup()
    expect(markSpy).toHaveBeenCalledWith(10)
    expect(store.unreadCount).toBe(2)
    expect(store.currentPopup?.id).toBe(11)

    // Refetching in the same session does not re-popup already shown items.
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(11)
  })

  it('marks all announcements read in one request and clears active popups', async () => {
    const popup = sampleAnnouncement({ id: 20, notify_mode: 'popup' })
    const silent = sampleAnnouncement({ id: 21, notify_mode: 'silent' })
    vi.spyOn(announcementsApi, 'listForUser').mockResolvedValue({
      items: [popup, silent],
      unread_count: 2,
    })
    const markAllSpy = vi
      .spyOn(announcementsApi, 'markAllRead')
      .mockResolvedValue({ updated: 2 })

    const store = useAnnouncementStore()
    await store.fetchAnnouncements(true)
    expect(store.unreadCount).toBe(2)

    const ok = await store.markAllAsRead()
    expect(ok).toBe(true)
    expect(markAllSpy).toHaveBeenCalledTimes(1)
    expect(store.unreadCount).toBe(0)
    expect(store.currentPopup).toBeNull()
    expect(store.items.every((item) => item.read_at !== null)).toBe(true)
  })

  it('resets announcements and popup state when the signed-in user changes', async () => {
    const popup = sampleAnnouncement({ id: 30, notify_mode: 'popup' })
    vi.spyOn(announcementsApi, 'listForUser').mockResolvedValue({
      items: [popup],
      unread_count: 1,
    })
    const store = useAnnouncementStore()
    await store.fetchAnnouncements(true)
    expect(store.items).toHaveLength(1)
    expect(store.currentPopup?.id).toBe(30)

    useAuthStore().persist({
      id: 99,
      username: 'other',
      display_name: 'Other',
      email: 'other@example.com',
      role: 1,
      status: 1,
      quota: 0,
      used_quota: 0,
      request_count: 0,
    })
    await Promise.resolve()

    expect(store.items).toHaveLength(0)
    expect(store.unreadCount).toBe(0)
    expect(store.currentPopup).toBeNull()
  })

  it('keeps popup open when dismiss markRead fails and discards stale markRead after reset', async () => {
    const popupA = sampleAnnouncement({ id: 40, notify_mode: 'popup' })
    const popupB = sampleAnnouncement({ id: 41, notify_mode: 'popup' })
    vi.spyOn(announcementsApi, 'listForUser').mockResolvedValue({
      items: [popupA, popupB],
      unread_count: 2,
    })
    vi.spyOn(announcementsApi, 'markRead').mockRejectedValueOnce(
      new Error('network error')
    )

    const store = useAnnouncementStore()
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(40)

    const dismissed = await store.dismissPopup()
    expect(dismissed).toBe(false)
    expect(store.currentPopup?.id).toBe(40)
    expect(store.unreadCount).toBe(2)

    let resolveMark!: (val: { id: number; read_at: number }) => void
    vi.spyOn(announcementsApi, 'markRead').mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveMark = resolve
        })
    )
    const inflight = store.markAsRead(40)
    store.reset()
    resolveMark({ id: 40, read_at: 1785129999 })
    expect(await inflight).toBe(false)
    expect(store.items).toHaveLength(0)
    expect(store.unreadCount).toBe(0)
  })
})
