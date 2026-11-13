import { evaluatePasswordStrength } from './password-strength'
import { reactive } from 'vue'
import { formatDecimalUSD } from './decimal'
import { configureQuotaPerUnit, quotaToDecimal } from './quota'

const quotaDisplay = reactive({
  type: 'USD',
  perUnit: 500000,
  cnyRate: 7.3,
  symbol: '¤',
  customRate: 1,
})
export function configureQuotaDisplay(status: {
  quota_display_type?: string
  quota_per_unit?: number
  usd_exchange_rate?: number
  custom_currency_symbol?: string
  custom_currency_exchange_rate?: number
}) {
  quotaDisplay.type = status.quota_display_type ?? 'USD'
  quotaDisplay.perUnit = status.quota_per_unit ?? 500_000
  configureQuotaPerUnit(quotaDisplay.perUnit)
  quotaDisplay.cnyRate = status.usd_exchange_rate ?? 7.3
  quotaDisplay.symbol = status.custom_currency_symbol || '¤'
  quotaDisplay.customRate = status.custom_currency_exchange_rate ?? 1
}

export interface QuotaParts {
  /** Currency symbol; empty in TOKENS mode. */
  prefix: string
  /** Integer part with thousands separators. */
  whole: string
  /** Decimal digits without the point; empty when there are none. */
  fraction: string
}

/** formatQuota split for layouts that size the integer and decimals apart. */
export function formatQuotaParts(
  quota: string | bigint | number,
  digits = 2
): QuotaParts {
  if (quotaDisplay.type === 'TOKENS')
    return {
      prefix: '',
      whole: BigInt(quota).toLocaleString('en-US'),
      fraction: '',
    }
  if (quotaDisplay.type !== 'USD') {
    const rate =
      quotaDisplay.type === 'CNY'
        ? quotaDisplay.cnyRate
        : quotaDisplay.type === 'CUSTOM'
          ? quotaDisplay.customRate
          : 1
    const symbol =
      quotaDisplay.type === 'CNY'
        ? '¥'
        : quotaDisplay.type === 'CUSTOM'
          ? quotaDisplay.symbol
          : '$'
    const [whole = '0', fraction = ''] = (
      (Number(quota) / quotaDisplay.perUnit) *
      rate
    )
      .toLocaleString('en-US', {
        minimumFractionDigits: Math.min(digits, 3),
        maximumFractionDigits: Math.min(digits, 3),
      })
      .split('.')
    return { prefix: symbol, whole, fraction }
  }
  const [whole = '0', fraction = ''] = quotaToDecimal(
    quota,
    Math.min(digits, 3),
    quotaDisplay.perUnit
  ).split('.')
  return {
    prefix: '$',
    whole: whole.replace(/\B(?=(\d{3})+(?!\d))/g, ','),
    fraction,
  }
}

export function formatQuota(
  quota: string | bigint | number,
  digits = 2
): string {
  const { prefix, whole, fraction } = formatQuotaParts(quota, digits)
  return `${prefix}${whole}${fraction ? `.${fraction}` : ''}`
}

export function formatMoney(amount: number, digits = 2): string {
  digits = Math.min(digits, 3)
  return `$${amount.toLocaleString('en-US', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })}`
}

export function formatNumber(value: number): string {
  return value.toLocaleString('en-US')
}

export function formatCompact(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

export function formatTime(epochSec: number): string {
  const d = new Date(epochSec * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function formatDate(epochSec: number): string {
  const d = new Date(epochSec * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/**
 * Local calendar date as 'YYYY-MM-DD' (DateRangePicker's wire format),
 * offset in whole days from today.
 */
export function dateInputValue(offsetDays = 0): string {
  const d = new Date()
  d.setDate(d.getDate() + offsetDays)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** Validated [year, monthIndex, day] of a strict 'YYYY-MM-DD' value. */
function parseLocalDay(value: string): [number, number, number] {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match) {
    const [year, month, day] = match.slice(1).map(Number)
    const date = new Date(year, month - 1, day)
    if (
      date.getFullYear() === year &&
      date.getMonth() === month - 1 &&
      date.getDate() === day
    ) {
      return [year, month - 1, day]
    }
  }
  throw new Error(`Invalid local date: ${value}`)
}

/** Epoch milliseconds of the first local moment of a 'YYYY-MM-DD' day. */
export function localDayStartMs(value: string): number {
  const [year, month, day] = parseLocalDay(value)
  return new Date(year, month, day).getTime()
}

/**
 * Epoch milliseconds of the last moment of a local 'YYYY-MM-DD' day. The next
 * day is built from calendar fields rather than by adding to a Date, so a
 * daylight-saving jump at midnight cannot carry its shifted hour forward.
 */
export function localDayEndMs(value: string): number {
  const [year, month, day] = parseLocalDay(value)
  return new Date(year, month, day + 1).getTime() - 1
}

/**
 * The browser's current UTC offset as the tz_offset query value (minutes,
 * positive west of UTC). Endpoints that bucket by calendar day or month use it
 * as one fixed offset for the whole window.
 */
export function viewerTzOffset(): string {
  return String(new Date().getTimezoneOffset())
}

export function relativeTime(epochSec: number, locale = 'zh-CN'): string {
  const diff = Math.floor(Date.now() / 1000) - epochSec
  const absolute = Math.abs(diff)
  const zh = locale.startsWith('zh')
  if (absolute < 60) return zh ? '刚刚' : 'just now'
  const direction = diff >= 0 ? (zh ? '前' : ' ago') : zh ? '后' : ' from now'
  if (absolute < 3600) {
    const minutes = Math.floor(absolute / 60)
    return zh ? `${minutes} 分钟${direction}` : `${minutes}m${direction}`
  }
  if (absolute < 86_400) {
    const hours = Math.floor(absolute / 3600)
    return zh ? `${hours} 小时${direction}` : `${hours}h${direction}`
  }
  const days = Math.floor(absolute / 86_400)
  if (days < 30) return zh ? `${days} 天${direction}` : `${days}d${direction}`
  return formatDate(epochSec)
}

/** Price per 1M tokens, e.g. "$1.250". null renders as "—" upstream. */
export function formatTokenPrice(value: string | undefined): string {
  if (value == null) return '—'
  return formatDecimalUSD(value, 3)
}

/** Latency snapshot: "1.78s" (<10s) / "18.0s" (≥10s), matching the design. */
export function formatLatency(sec: number): string {
  return sec >= 10 ? `${sec.toFixed(1)}s` : `${sec.toFixed(2)}s`
}

/** Human byte size: 2_205_000 → "2.1 MB", 12_800 → "12.5 KB". */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

/** Video length: 5 → "5s", 75 → "1:15". */
export function formatDuration(sec: number): string {
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

export function maskKey(key: string): string {
  const value = key.trim()
  if (!value) return ''
  if (value.length <= 4) return '•'.repeat(value.length)
  if (value.length <= 11) {
    const prefixLength = Math.min(3, Math.max(1, Math.floor(value.length / 3)))
    const suffixLength = Math.min(2, Math.max(1, Math.floor(value.length / 4)))
    return `${value.slice(0, prefixLength)}••••${value.slice(-suffixLength)}`
  }
  return `${value.slice(0, 6)}••••${value.slice(-4)}`
}

/** Compatibility score for existing consumers; length alone controls acceptance. */
export function passwordStrength(password: string): 0 | 1 | 2 | 3 {
  return Math.min(evaluatePasswordStrength(password).score, 3) as 0 | 1 | 2 | 3
}

/**
 * Epoch seconds as a datetime-local value ('YYYY-MM-DDTHH:mm:ss') in the
 * browser time zone, keeping the full instant like upstream's date picker.
 */
export function toDateTimeLocal(epochSec: number): string {
  const d = new Date(epochSec * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/**
 * Parses a datetime-local value as browser-local time into whole epoch
 * seconds; optional seconds and fractions follow the HTML value format.
 * Returns null for text that is not a real local date and time, including
 * times skipped by a daylight-saving jump. A repeated local hour resolves to
 * its first occurrence, so callers keep the stored epoch when the text is
 * unchanged.
 */
export function fromDateTimeLocal(value: string): number | null {
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.\d{1,3})?)?$/.exec(
      value
    )
  if (!match) return null
  const [year, month, day, hour, minute] = match.slice(1, 6).map(Number)
  const second = Number(match[6] ?? 0)
  const date = new Date(year, month - 1, day, hour, minute, second)
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day ||
    date.getHours() !== hour ||
    date.getMinutes() !== minute ||
    date.getSeconds() !== second
  )
    return null
  return Math.floor(date.getTime() / 1000)
}
