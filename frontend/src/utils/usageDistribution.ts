import { formatCompact, formatNumber, formatQuota } from '@/utils/format'

export interface UsageDistributionPoint {
  date: string
  requests: number
  /** Spend in quota units; callers convert API quota strings for the chart. */
  consume: number
  tokens: number
}

export type UsageDistributionMetric = 'requests' | 'consume' | 'tokens'
export type UsageDistributionPeriod = 'month' | 'quarter' | 'year'

export interface UsageHeatmapCell {
  date: string
  value: number
  level: 0 | 1 | 2 | 3 | 4 | 5
  inRange: boolean
  /** False for in-range days before the queried window; they are not zero. */
  covered: boolean
  future: boolean
  monthKey: string
}

export interface UsageWeekdaySummary {
  weekday: number
  value: number
}

export interface UsageDistributionView {
  cells: UsageHeatmapCell[]
  weekCount: number
  total: number
  activeDays: number
  peak: UsageHeatmapCell | null
  topDays: UsageHeatmapCell[]
  weekdays: UsageWeekdaySummary[]
  rangeStart: string
  rangeEnd: string
}

const PERIOD_DAYS: Record<UsageDistributionPeriod, number> = {
  month: 30,
  quarter: 91,
  year: 364,
}

/** Ranked analytics always reserve this many rows so the card height is fixed. */
export const USAGE_TOP_DAY_LIMIT = 3

function localDateKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function fromDateKey(key: string): Date {
  const [year, month, day] = key.split('-').map(Number)
  return new Date(year!, month! - 1, day)
}

function addDays(date: Date, amount: number): Date {
  const next = new Date(date)
  next.setDate(next.getDate() + amount)
  return next
}

export function usageDistributionRange(
  period: UsageDistributionPeriod,
  referenceDate = new Date()
): { start: string; end: string } {
  return {
    start: localDateKey(addDays(referenceDate, -(PERIOD_DAYS[period] - 1))),
    end: localDateKey(referenceDate),
  }
}

/** The year window, shortened to the newest days log_max_range_days allows. */
export function usageCoverageRange(
  limitDays: number,
  referenceDate = new Date()
): { start: string; end: string } {
  const days = Math.min(PERIOD_DAYS.year, limitDays)
  return {
    start: localDateKey(addDays(referenceDate, -(days - 1))),
    end: localDateKey(referenceDate),
  }
}

function mondayOf(date: Date): Date {
  return addDays(date, -((date.getDay() + 6) % 7))
}

function sundayOf(date: Date): Date {
  return addDays(mondayOf(date), 6)
}

function metricValue(
  point: UsageDistributionPoint | undefined,
  metric: UsageDistributionMetric
): number {
  return point?.[metric] ?? 0
}

function quantileThresholds(values: number[]): number[] {
  const sorted = values.filter((value) => value > 0).sort((a, b) => a - b)
  if (!sorted.length) return []
  return [1, 2, 3, 4].map((part) => {
    const index = Math.max(0, Math.ceil((sorted.length * part) / 5) - 1)
    return sorted[index]!
  })
}

function intensityLevel(
  value: number,
  thresholds: number[]
): 0 | 1 | 2 | 3 | 4 | 5 {
  if (value <= 0) return 0
  let level = 1
  thresholds.forEach((threshold) => {
    if (value > threshold) level++
  })
  return Math.min(5, level) as 1 | 2 | 3 | 4 | 5
}

export function buildUsageDistributionView(
  points: UsageDistributionPoint[],
  period: UsageDistributionPeriod,
  metric: UsageDistributionMetric,
  /** First queried date ('YYYY-MM-DD'); earlier days are excluded, not zero. */
  coveredFrom: string,
  referenceDate = new Date()
): UsageDistributionView {
  const today = new Date(referenceDate)
  today.setHours(0, 0, 0, 0)
  const range = usageDistributionRange(period, today)
  const rangeEnd = fromDateKey(range.end)
  const rangeStart = fromDateKey(range.start)
  const gridStart = mondayOf(rangeStart)
  const gridEnd = sundayOf(rangeEnd)
  const pointMap = new Map(points.map((point) => [point.date, point]))

  const values: number[] = []
  for (
    let cursor = rangeStart;
    cursor <= rangeEnd;
    cursor = addDays(cursor, 1)
  ) {
    const date = localDateKey(cursor)
    if (date >= coveredFrom)
      values.push(metricValue(pointMap.get(date), metric))
  }
  const thresholds = quantileThresholds(values)

  const cells: UsageHeatmapCell[] = []
  for (let cursor = gridStart; cursor <= gridEnd; cursor = addDays(cursor, 1)) {
    const date = localDateKey(cursor)
    const future = cursor > today
    const inRange = cursor >= rangeStart && cursor <= rangeEnd
    const covered = inRange && date >= coveredFrom
    const value = covered ? metricValue(pointMap.get(date), metric) : 0
    cells.push({
      date,
      value,
      level: covered ? intensityLevel(value, thresholds) : 0,
      inRange,
      covered,
      future,
      monthKey: date.slice(0, 7),
    })
  }

  const active = cells.filter((cell) => cell.covered && cell.value > 0)
  const ranked = [...active].sort(
    (left, right) =>
      right.value - left.value || right.date.localeCompare(left.date)
  )
  const weekdays = Array.from({ length: 7 }, (_, weekday) => {
    const matching = cells.filter(
      (cell) =>
        cell.covered && (fromDateKey(cell.date).getDay() + 6) % 7 === weekday
    )
    return {
      weekday,
      value: matching.length
        ? matching.reduce((sum, cell) => sum + cell.value, 0) / matching.length
        : 0,
    }
  })

  return {
    cells,
    weekCount: cells.length / 7,
    total: values.reduce((sum, value) => sum + value, 0),
    activeDays: active.length,
    peak: ranked[0] ?? null,
    topDays: ranked.slice(0, USAGE_TOP_DAY_LIMIT),
    weekdays,
    rangeStart: localDateKey(rangeStart),
    rangeEnd: localDateKey(rangeEnd),
  }
}

export function shiftUsageDate(dateKey: string, days: number): string {
  return localDateKey(addDays(fromDateKey(dateKey), days))
}

/** One formatter for totals, cells, tooltips and analytics of every metric. */
export function formatUsageMetric(
  metric: UsageDistributionMetric,
  value: number
): string {
  // Averages are fractional quota; the site currency setting picks the unit.
  if (metric === 'consume') return formatQuota(Math.round(value))
  if (metric === 'tokens') return formatCompact(Math.round(value))
  return formatNumber(Math.round(value))
}

export function formatUsageDate(dateKey: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
  }).format(fromDateKey(dateKey))
}

/**
 * Weekday names, Monday first, matching the grid's week layout. Narrow names
 * label columns; short names stand alone in text, where "T" would be ambiguous.
 */
export function usageWeekdayLabels(
  locale: string,
  width: 'narrow' | 'short' = 'narrow'
): string[] {
  const format = new Intl.DateTimeFormat(locale, { weekday: width })
  return Array.from({ length: 7 }, (_, index) =>
    format.format(new Date(2024, 0, 1 + index))
  )
}

/**
 * Cell and legend colours for one intensity level of the active metric. The
 * theme owns the ramp: tokens.css sets each level's mix and the day-number
 * colour that stays readable on it.
 */
export function usageHeatStyle(
  level: UsageHeatmapCell['level'],
  color: string
): { background: string; borderColor: string; color: string } {
  const foreground = `var(--heatmap-foreground-${level})`
  if (level === 0) {
    return {
      background: 'var(--surface-muted)',
      borderColor: 'var(--border-subtle)',
      color: foreground,
    }
  }
  const amount = `var(--heatmap-mix-${level})`
  return {
    background: `color-mix(in srgb, ${color} ${amount}, var(--surface-solid))`,
    borderColor: `color-mix(in srgb, ${color} ${amount}, var(--border-default))`,
    color: foreground,
  }
}
