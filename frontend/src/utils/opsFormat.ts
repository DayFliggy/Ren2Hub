type OpsStatus = 'ok' | 'warning' | 'critical' | 'unknown'
/** A display value with its unit kept separate, so the unit can sit smaller. */
export interface DisplayValue {
  value: string
  unit: string
}

const EMPTY: DisplayValue = { value: '--', unit: '' }

function decimal(value: number, locale: string, digits: number): string {
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  }).format(value)
}

export function formatPercent(
  value: number | null,
  locale: string
): DisplayValue {
  if (value === null) return EMPTY
  return { value: decimal(value, locale, 1), unit: '%' }
}

/** Sub-second latencies stay in ms; slower ones switch to seconds. */
export function formatLatencyMs(
  value: number | null,
  locale: string
): DisplayValue {
  if (value === null) return EMPTY
  if (value >= 1000)
    return { value: decimal(value / 1000, locale, 2), unit: 's' }
  return { value: decimal(value, locale, value < 10 ? 1 : 0), unit: 'ms' }
}

export function formatBytes(
  value: number | null,
  locale: string
): DisplayValue {
  if (value === null) return EMPTY
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let scaled = value
  let index = 0
  while (scaled >= 1024 && index < units.length - 1) {
    scaled /= 1024
    index++
  }
  return {
    value: decimal(scaled, locale, index === 0 ? 0 : 1),
    unit: units[index]!,
  }
}

export function formatMbps(value: number | null, locale: string): DisplayValue {
  if (value === null) return EMPTY
  if (value >= 1) return { value: decimal(value, locale, 1), unit: 'Mbps' }
  return { value: decimal(value * 1000, locale, 0), unit: 'Kbps' }
}

/** Counts up to 9,999 stay exact; larger ones become compact (12.3K). */
export function formatCount(value: number | null, locale: string): string {
  if (value === null) return '--'
  if (value < 10_000) return decimal(value, locale, 0)
  return new Intl.NumberFormat(locale, {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)
}

export function formatRate(value: number | null, locale: string): string {
  if (value === null) return '--'
  return decimal(value, locale, value < 10 ? 2 : 1)
}

export function formatTimestamp(
  seconds: number | null,
  locale: string
): string {
  if (!seconds) return '--'
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(seconds * 1000)
}

export function formatClock(milliseconds: number, locale: string): string {
  if (!milliseconds) return '--'
  return new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(
    milliseconds
  )
}

export function formatRelative(
  seconds: number | null,
  nowMs: number,
  locale: string
): string {
  if (!seconds) return '--'
  const delta = Math.round((seconds * 1000 - nowMs) / 1000)
  const magnitude = Math.abs(delta)
  const [unit, divisor] =
    magnitude < 60
      ? (['second', 1] as const)
      : magnitude < 3600
        ? (['minute', 60] as const)
        : magnitude < 86_400
          ? (['hour', 3600] as const)
          : (['day', 86_400] as const)
  return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(
    Math.round(delta / divisor),
    unit
  )
}

/** Splits seconds into the two most significant day/hour/minute/second parts. */
export function durationParts(
  seconds: number
): Array<{ unit: 'd' | 'h' | 'm' | 's'; amount: number }> {
  const parts = [
    { unit: 'd' as const, amount: Math.floor(seconds / 86_400) },
    { unit: 'h' as const, amount: Math.floor((seconds % 86_400) / 3600) },
    { unit: 'm' as const, amount: Math.floor((seconds % 3600) / 60) },
    { unit: 's' as const, amount: Math.floor(seconds % 60) },
  ]
  const first = parts.findIndex((part) => part.amount > 0)
  if (first === -1) return [{ unit: 's', amount: 0 }]
  return parts.slice(first, first + 2).filter((part) => part.amount > 0)
}

/** Converts cumulative histogram counts into per-bucket counts. */
export function bucketCounts(cumulative: readonly number[]): number[] {
  return cumulative.map(
    (total, i) => total - (i === 0 ? 0 : cumulative[i - 1]!)
  )
}

export type HealthTone = 'good' | 'degraded' | 'poor' | 'idle'

/** Per-bucket request health: >=90% good, 50-90% degraded, <50% poor. */
export function healthTone(
  requests: number,
  successRate: number | null
): HealthTone {
  if (requests === 0 || successRate === null) return 'idle'
  if (successRate >= 90) return 'good'
  if (successRate >= 50) return 'degraded'
  return 'poor'
}

export const HEALTH_COLORS: Record<HealthTone, string> = {
  good: 'var(--status-success)',
  degraded: 'var(--status-warning)',
  poor: 'var(--status-danger)',
  idle: 'var(--border-default)',
}

export const STATUS_TONES = {
  ok: 'success',
  warning: 'warning',
  critical: 'danger',
  unknown: 'neutral',
} as const satisfies Record<OpsStatus, string>

/** Gauge strokes: healthy uses the dashboard's normal-load colour. */
export const GAUGE_COLORS: Record<OpsStatus, string> = {
  ok: 'var(--glow)',
  warning: 'var(--status-warning)',
  critical: 'var(--status-danger)',
  unknown: 'var(--text-tertiary)',
}

export const STATUS_COLORS: Record<OpsStatus, string> = {
  ok: 'var(--status-success)',
  warning: 'var(--status-warning)',
  critical: 'var(--status-danger)',
  unknown: 'var(--text-tertiary)',
}
