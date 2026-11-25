import { describe, expect, it, vi } from 'vitest'

import {
  adminAnnouncementsApi,
  parseAdminAnnouncement,
  parseAdminAnnouncementPage,
  parseAnnouncementReadStatusPage,
  parseUserAnnouncementList,
} from '@/api/announcements'
import { api } from '@/api/client'
import fixture from '../../../tests/fixtures/native/announcements.get.json'

describe('announcements API contracts', () => {
  it('parses the backend-generated user list, admin list, detail, and read-status fixtures', () => {
    const userList = parseUserAnnouncementList(fixture.user_list.data)
    expect(userList.unread_count).toBe(2)
    expect(userList.items).toHaveLength(2)
    expect(userList.items[0]?.id).toBe(11)
    expect(userList.items[0]?.pinned).toBe(true)
    expect(userList.items[0]?.read_at).toBeNull()
    expect(userList.items[1]?.notify_mode).toBe('popup')

    const adminPage = parseAdminAnnouncementPage(fixture.admin_list.data)
    expect(adminPage.total).toBe(3)
    expect(adminPage.items[0]?.read_count).toBe(1)
    expect(adminPage.items[2]?.status).toBe('draft')

    const detail = parseAdminAnnouncement(fixture.admin_detail.data)
    expect(detail.id).toBe(11)
    expect(detail.type).toBe('ongoing')

    const readStatus = parseAnnouncementReadStatusPage(fixture.read_status.data)
    expect(readStatus.total_users).toBe(2)
    expect(readStatus.read_count).toBe(1)
    expect(readStatus.items[0]?.username).toBe('member.demo')
    expect(readStatus.items[0]?.read_at).toBe(1785123600)
    expect(readStatus.items[1]?.read_at).toBeNull()
  })

  it('rejects unknown types, statuses, notify modes, or negative read counts', () => {
    const valid = fixture.admin_detail.data
    expect(() =>
      parseAdminAnnouncement({ ...valid, type: 'invalid' })
    ).toThrowError()
    expect(() =>
      parseAdminAnnouncement({ ...valid, status: 'published' })
    ).toThrowError()
    expect(() =>
      parseAdminAnnouncement({ ...valid, notify_mode: 'banner' })
    ).toThrowError()
    expect(() =>
      parseAdminAnnouncement({ ...valid, read_count: -1 })
    ).toThrowError()
  })

  it('encodes null schedule fields as 0 when clearing schedule on update', async () => {
    const spy = vi
      .spyOn(api, 'put')
      .mockResolvedValue(fixture.admin_detail.data)
    await adminAnnouncementsApi.update(11, {
      starts_at: null,
      ends_at: null,
      pinned: false,
    })
    expect(spy).toHaveBeenCalledWith('/api/admin/announcements/11', {
      pinned: false,
      starts_at: 0,
      ends_at: 0,
    })
    spy.mockRestore()
  })
})
