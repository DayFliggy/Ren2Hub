import { describe, expect, it } from 'vitest'

import type { UsageDistributionPoint } from '@/utils/usageDistribution'
import {
  buildUsageDistributionView,
  usageCoverageRange,
} from '@/utils/usageDistribution'

function dateKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function history(days: number, end = new Date(2026, 6, 27)) {
  return Array.from({ length: days }, (_, index): UsageDistributionPoint => {
    const date = new Date(end)
    date.setDate(date.getDate() - (days - index - 1))
    return {
      date: dateKey(date),
      requests: index + 1,
      consume: (index + 1) * 500_000,
      tokens: (index + 1) * 1_000,
    }
  })
}

describe('buildUsageDistributionView', () => {
  const today = new Date(2026, 6, 27)
  const points = history(365, today)
  // A full year of coverage: the default 365-day log limit.
  const covered = usageCoverageRange(365, today).start

  it.each([
    ['month', 30],
    ['quarter', 91],
    ['year', 364],
  ] as const)('selects the %s window', (period, expectedDays) => {
    const view = buildUsageDistributionView(
      points,
      period,
      'requests',
      covered,
      today
    )

    expect(view.cells.filter((cell) => cell.inRange)).toHaveLength(expectedDays)
    expect(view.cells).toHaveLength(view.weekCount * 7)
  })

  it('aligns the padded grid to Monday and marks future cells empty', () => {
    const view = buildUsageDistributionView(
      points,
      'quarter',
      'requests',
      covered,
      today
    )
    const first = new Date(`${view.cells[0]!.date}T00:00:00`)

    expect(first.getDay()).toBe(1)
    expect(
      view.cells.filter((cell) => cell.future).every((cell) => !cell.inRange)
    ).toBe(true)
  })

  it('builds five positive intensity bands without treating zero as active', () => {
    const sparse = history(30, today).map((point, index) => ({
      ...point,
      requests: index % 6,
    }))
    const view = buildUsageDistributionView(
      sparse,
      'month',
      'requests',
      covered,
      today
    )
    const levels = new Set(
      view.cells.filter((cell) => cell.inRange).map((cell) => cell.level)
    )

    expect(levels).toEqual(new Set([0, 1, 2, 3, 4, 5]))
    expect(view.activeDays).toBe(25)
  })

  it('ranks peak days and averages every weekday for the selected metric', () => {
    const view = buildUsageDistributionView(
      points,
      'month',
      'consume',
      covered,
      today
    )

    expect(view.topDays).toHaveLength(3)
    expect(view.topDays[0]!.date).toBe('2026-07-27')
    expect(view.peak).toEqual(view.topDays[0])
    expect(view.weekdays).toHaveLength(7)
    expect(view.weekdays.every((entry) => entry.value > 0)).toBe(true)
  })

  it('returns an empty peak for an all-zero period', () => {
    const zeroes = history(30, today).map((point) => ({
      ...point,
      requests: 0,
    }))
    const view = buildUsageDistributionView(
      zeroes,
      'month',
      'requests',
      covered,
      today
    )

    expect(view.total).toBe(0)
    expect(view.activeDays).toBe(0)
    expect(view.peak).toBeNull()
    expect(view.topDays).toEqual([])
  })

  it('shortens the queried year to the log range limit', () => {
    expect(usageCoverageRange(365, today)).toEqual({
      start: '2025-07-29',
      end: '2026-07-27',
    })
    expect(usageCoverageRange(10, today)).toEqual({
      start: '2026-07-18',
      end: '2026-07-27',
    })
  })

  it('leaves days before the queried window out instead of counting zero', () => {
    // A 10-day log limit: the backend only returns 2026-07-18 through 07-27,
    // with requests 1..10 (Saturday the 18th through Monday the 27th).
    const view = buildUsageDistributionView(
      history(10, today),
      'month',
      'requests',
      usageCoverageRange(10, today).start,
      today
    )
    const inRange = view.cells.filter((cell) => cell.inRange)
    const uncovered = inRange.filter((cell) => !cell.covered)

    expect(inRange).toHaveLength(30)
    expect(uncovered).toHaveLength(20)
    expect(uncovered.every((cell) => cell.level === 0)).toBe(true)
    expect(view.total).toBe(55)
    expect(view.activeDays).toBe(10)
    // Mondays 07-20 and 07-27; zero-filled June and early July Mondays would
    // pull this down to 2.6.
    expect(view.weekdays[0]!.value).toBe(6.5)
  })
})
