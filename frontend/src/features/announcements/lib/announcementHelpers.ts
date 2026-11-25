import type {
  AnnouncementBase,
  AnnouncementStatus,
  AnnouncementType,
} from '@/api/announcements'

export type ChipTone =
  'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'accent'

export function announcementTypeTone(type: AnnouncementType): ChipTone {
  switch (type) {
    case 'ongoing':
      return 'info'
    case 'success':
      return 'success'
    case 'warning':
      return 'warning'
    case 'error':
      return 'danger'
    default:
      return 'neutral'
  }
}

export function announcementTypeDotColor(type: AnnouncementType): string {
  switch (type) {
    case 'ongoing':
      return 'var(--status-info)'
    case 'success':
      return 'var(--status-success)'
    case 'warning':
      return 'var(--status-warning)'
    case 'error':
      return 'var(--status-danger)'
    default:
      return 'var(--text-tertiary)'
  }
}

export function announcementStatusTone(status: AnnouncementStatus): ChipTone {
  switch (status) {
    case 'active':
      return 'success'
    case 'archived':
      return 'warning'
    default:
      return 'neutral'
  }
}

export function announcementLiveState(
  item: Pick<AnnouncementBase, 'status' | 'starts_at' | 'ends_at'>,
  nowSec = Math.floor(Date.now() / 1000)
): 'scheduled' | 'expired' | null {
  if (item.status !== 'active') return null
  if (item.starts_at !== null && nowSec < item.starts_at) return 'scheduled'
  if (item.ends_at !== null && nowSec >= item.ends_at) return 'expired'
  return null
}

/** Strips common Markdown syntax into a concise plain-text preview line. */
export function announcementExcerpt(markdown: string, maxLength = 120): string {
  const plain = markdown
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s*(?:#{1,6}|>+|\d+\.|[-*+])\s+/gm, '')
    .replace(/[*_~]+/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  if (plain.length <= maxLength) return plain
  return `${plain.slice(0, maxLength).trimEnd()}…`
}

export function formatUnixDateTime(unixSec: number, locale = 'zh-CN'): string {
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(unixSec * 1000))
}

export function unixToLocalInput(unixSec: number | null): string {
  if (unixSec === null || unixSec <= 0) return ''
  const date = new Date(unixSec * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function localInputToUnix(value: string): number | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  const ms = new Date(trimmed).getTime()
  if (!Number.isFinite(ms) || ms <= 0) return null
  return Math.floor(ms / 1000)
}
