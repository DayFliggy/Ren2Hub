import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parsePage,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import type { PageResult } from './types'

export type AnnouncementType =
  'default' | 'ongoing' | 'success' | 'warning' | 'error'

export type AnnouncementStatus = 'draft' | 'active' | 'archived'
export type AnnouncementNotifyMode = 'silent' | 'popup'
export type AnnouncementReadFilter = '' | 'read' | 'unread'

export const ANNOUNCEMENT_TYPES: readonly AnnouncementType[] = [
  'default',
  'ongoing',
  'success',
  'warning',
  'error',
]

export const ANNOUNCEMENT_STATUSES: readonly AnnouncementStatus[] = [
  'draft',
  'active',
  'archived',
]

export const ANNOUNCEMENT_NOTIFY_MODES: readonly AnnouncementNotifyMode[] = [
  'silent',
  'popup',
]

export interface AnnouncementBase {
  id: number
  title: string
  content: string
  type: AnnouncementType
  status: AnnouncementStatus
  notify_mode: AnnouncementNotifyMode
  pinned: boolean
  starts_at: number | null
  ends_at: number | null
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export interface UserAnnouncement extends AnnouncementBase {
  read_at: number | null
}

export interface UserAnnouncementList {
  items: UserAnnouncement[]
  unread_count: number
}

export interface AdminAnnouncement extends AnnouncementBase {
  read_count: number
}

export interface AnnouncementUserReadStatus {
  user_id: number
  username: string
  display_name: string
  email: string
  read_at: number | null
}

export interface AnnouncementReadStatusPage extends PageResult<AnnouncementUserReadStatus> {
  total_users: number
  read_count: number
}

export interface AnnouncementWriteInput {
  title: string
  content: string
  type: AnnouncementType
  status: AnnouncementStatus
  notify_mode: AnnouncementNotifyMode
  pinned: boolean
  starts_at?: number | null
  ends_at?: number | null
}

export interface AdminAnnouncementListParams {
  page?: number
  page_size?: number
  keyword?: string
  status?: AnnouncementStatus | ''
  type?: AnnouncementType | ''
}

export interface AnnouncementReadStatusParams {
  page?: number
  page_size?: number
  keyword?: string
  filter?: AnnouncementReadFilter
}

function optionalUnix(value: unknown, endpoint: string): number | null {
  if (value === null || value === undefined) return null
  const num = requiredStrictInteger(value, endpoint)
  if (num < 0) invalidResponse(endpoint)
  return num
}

function parseType(value: unknown, endpoint: string): AnnouncementType {
  const str = requiredString(value, endpoint, false)
  if ((ANNOUNCEMENT_TYPES as readonly string[]).includes(str)) {
    return str as AnnouncementType
  }
  invalidResponse(endpoint)
}

function parseStatus(value: unknown, endpoint: string): AnnouncementStatus {
  const str = requiredString(value, endpoint, false)
  if ((ANNOUNCEMENT_STATUSES as readonly string[]).includes(str)) {
    return str as AnnouncementStatus
  }
  invalidResponse(endpoint)
}

function parseNotifyMode(
  value: unknown,
  endpoint: string
): AnnouncementNotifyMode {
  const str = requiredString(value, endpoint, false)
  if ((ANNOUNCEMENT_NOTIFY_MODES as readonly string[]).includes(str)) {
    return str as AnnouncementNotifyMode
  }
  invalidResponse(endpoint)
}

function parseBase(value: unknown, endpoint: string): AnnouncementBase {
  if (!isRecord(value)) invalidResponse(endpoint)
  const id = requiredStrictInteger(value.id, endpoint)
  if (id <= 0) invalidResponse(endpoint)
  return {
    id,
    title: requiredString(value.title, endpoint, false),
    content: requiredString(value.content, endpoint, false),
    type: parseType(value.type, endpoint),
    status: parseStatus(value.status, endpoint),
    notify_mode: parseNotifyMode(value.notify_mode, endpoint),
    pinned: requiredBoolean(value.pinned, endpoint),
    starts_at: optionalUnix(value.starts_at, endpoint),
    ends_at: optionalUnix(value.ends_at, endpoint),
    created_by: requiredStrictInteger(value.created_by, endpoint),
    updated_by: requiredStrictInteger(value.updated_by, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    updated_at: requiredStrictInteger(value.updated_at, endpoint),
  }
}

export function parseUserAnnouncement(
  value: unknown,
  endpoint = '/api/announcements'
): UserAnnouncement {
  const base = parseBase(value, endpoint)
  const record = value as Record<string, unknown>
  return {
    ...base,
    read_at: optionalUnix(record.read_at, endpoint),
  }
}

export function parseUserAnnouncementList(
  value: unknown,
  endpoint = '/api/announcements'
): UserAnnouncementList {
  if (!isRecord(value) || !Array.isArray(value.items)) invalidResponse(endpoint)
  const unreadCount = requiredStrictInteger(value.unread_count, endpoint)
  if (unreadCount < 0) invalidResponse(endpoint)
  return {
    items: value.items.map((item) => parseUserAnnouncement(item, endpoint)),
    unread_count: unreadCount,
  }
}

export function parseAdminAnnouncement(
  value: unknown,
  endpoint = '/api/admin/announcements'
): AdminAnnouncement {
  const base = parseBase(value, endpoint)
  const record = value as Record<string, unknown>
  const readCount = requiredStrictInteger(record.read_count, endpoint)
  if (readCount < 0) invalidResponse(endpoint)
  return {
    ...base,
    read_count: readCount,
  }
}

export function parseAdminAnnouncementPage(
  value: unknown,
  endpoint = '/api/admin/announcements'
): PageResult<AdminAnnouncement> {
  return parsePage(value, endpoint, parseAdminAnnouncement)
}

export function parseAnnouncementUserReadStatus(
  value: unknown,
  endpoint = '/api/admin/announcements/:id/read-status'
): AnnouncementUserReadStatus {
  if (!isRecord(value)) invalidResponse(endpoint)
  const userId = requiredStrictInteger(value.user_id, endpoint)
  if (userId <= 0) invalidResponse(endpoint)
  return {
    user_id: userId,
    username: requiredString(value.username, endpoint),
    display_name: requiredString(value.display_name, endpoint),
    email: requiredString(value.email, endpoint),
    read_at: optionalUnix(value.read_at, endpoint),
  }
}

export function parseAnnouncementReadStatusPage(
  value: unknown,
  endpoint = '/api/admin/announcements/:id/read-status'
): AnnouncementReadStatusPage {
  const page = parsePage(value, endpoint, parseAnnouncementUserReadStatus)
  const record = value as Record<string, unknown>
  const totalUsers = requiredStrictInteger(record.total_users, endpoint)
  const readCount = requiredStrictInteger(record.read_count, endpoint)
  if (totalUsers < 0 || readCount < 0) invalidResponse(endpoint)
  return {
    ...page,
    total_users: totalUsers,
    read_count: readCount,
  }
}

export const announcementsApi = {
  async listForUser(
    unreadOnly = false,
    signal?: AbortSignal
  ): Promise<UserAnnouncementList> {
    const endpoint = '/api/announcements'
    const raw = await api.get<unknown>(
      endpoint,
      unreadOnly ? { unread_only: 'true' } : undefined,
      { signal }
    )
    return parseUserAnnouncementList(raw, endpoint)
  },

  async markRead(id: number): Promise<{ id: number; read_at: number }> {
    const endpoint = `/api/announcements/${id}/read`
    const raw = await api.post<unknown>(endpoint)
    if (!isRecord(raw)) invalidResponse(endpoint)
    const parsedId = requiredStrictInteger(raw.id, endpoint)
    const readAt = requiredStrictInteger(raw.read_at, endpoint)
    if (parsedId <= 0 || readAt < 0) invalidResponse(endpoint)
    return {
      id: parsedId,
      read_at: readAt,
    }
  },

  async markAllRead(): Promise<{ updated: number }> {
    const endpoint = '/api/announcements/read-all'
    const raw = await api.post<unknown>(endpoint)
    if (!isRecord(raw)) invalidResponse(endpoint)
    const updated = requiredStrictInteger(raw.updated, endpoint)
    if (updated < 0) invalidResponse(endpoint)
    return { updated }
  },
}

export const adminAnnouncementsApi = {
  async list(
    params: AdminAnnouncementListParams = {},
    signal?: AbortSignal
  ): Promise<PageResult<AdminAnnouncement>> {
    const endpoint = '/api/admin/announcements'
    const raw = await api.get<unknown>(
      endpoint,
      {
        p: params.page ?? 1,
        page_size: params.page_size ?? 20,
        ...(params.keyword?.trim() ? { keyword: params.keyword.trim() } : {}),
        ...(params.status ? { status: params.status } : {}),
        ...(params.type ? { type: params.type } : {}),
      },
      { signal }
    )
    return parseAdminAnnouncementPage(raw, endpoint)
  },

  async get(id: number, signal?: AbortSignal): Promise<AdminAnnouncement> {
    const endpoint = `/api/admin/announcements/${id}`
    const raw = await api.get<unknown>(endpoint, undefined, { signal })
    return parseAdminAnnouncement(raw, endpoint)
  },

  async create(input: AnnouncementWriteInput): Promise<AdminAnnouncement> {
    const endpoint = '/api/admin/announcements'
    const raw = await api.post<unknown>(endpoint, {
      title: input.title,
      content: input.content,
      type: input.type,
      status: input.status,
      notify_mode: input.notify_mode,
      pinned: input.pinned,
      starts_at: input.starts_at ?? null,
      ends_at: input.ends_at ?? null,
    })
    return parseAdminAnnouncement(raw, endpoint)
  },

  async update(
    id: number,
    input: Partial<AnnouncementWriteInput>
  ): Promise<AdminAnnouncement> {
    const endpoint = `/api/admin/announcements/${id}`
    const body: Record<string, unknown> = {}
    if (input.title !== undefined) body.title = input.title
    if (input.content !== undefined) body.content = input.content
    if (input.type !== undefined) body.type = input.type
    if (input.status !== undefined) body.status = input.status
    if (input.notify_mode !== undefined) body.notify_mode = input.notify_mode
    if (input.pinned !== undefined) body.pinned = input.pinned
    if (input.starts_at !== undefined) body.starts_at = input.starts_at ?? 0
    if (input.ends_at !== undefined) body.ends_at = input.ends_at ?? 0
    const raw = await api.put<unknown>(endpoint, body)
    return parseAdminAnnouncement(raw, endpoint)
  },

  async remove(id: number): Promise<void> {
    await api.delete(`/api/admin/announcements/${id}`)
  },

  async readStatus(
    id: number,
    params: AnnouncementReadStatusParams = {},
    signal?: AbortSignal
  ): Promise<AnnouncementReadStatusPage> {
    const endpoint = `/api/admin/announcements/${id}/read-status`
    const raw = await api.get<unknown>(
      endpoint,
      {
        p: params.page ?? 1,
        page_size: params.page_size ?? 20,
        ...(params.keyword?.trim() ? { keyword: params.keyword.trim() } : {}),
        ...(params.filter ? { filter: params.filter } : {}),
      },
      { signal }
    )
    return parseAnnouncementReadStatusPage(raw, endpoint)
  },
}
