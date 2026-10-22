import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  currentMonthRange,
  getUsageSummary,
  logRangeLimit,
  parseUsageSummary,
  summaryFits,
} from '@/api/dashboard'
import { dashboardFixture } from './dashboardFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
afterEach(() => vi.restoreAllMocks())
describe('quota_data usage summary', () => {
  it('reads personal success rate, hourly timeline and cache tokens', () => {
    const result = parseUsageSummary(dashboardFixture())
    expect(result.kpi.totalQuota).toBe('25')
    expect(result.kpi.successRate).toBe(50)
    expect(result.kpi.avgLatency).toBe(820)
    expect(result.hourly.map((hour) => hour.ts)).toEqual([
      1792537200, 1792540800,
    ])
    expect(result.hourly[1]).toEqual({
      ts: 1792540800,
      requests: 2,
      tokens: 10,
      quota: '25',
    })
    expect(result.token_trend[0]).toEqual({
      date: '2026-10-21',
      total: 10,
      cache_read: 4,
      cache_create: 1,
    })
    expect(result.meta.flush_interval_minutes).toBe(5)
  })
  it('sends the browser timezone convention without changing its sign', async () => {
    vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(-480)
    vi.mocked(api.get).mockResolvedValue(dashboardFixture())
    const signal = new AbortController().signal
    await getUsageSummary({ range: 'today' }, false, signal)
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/self/summary',
      { range: 'today', tz_offset: '-480', include_comparison: 'false' },
      { signal }
    )
  })
  it('always states include_comparison, since the backend defaults to true', async () => {
    vi.mocked(api.get).mockResolvedValue(dashboardFixture())
    const range = currentMonthRange('2026-10-22')
    await getUsageSummary(range, true)
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/self/summary',
      expect.objectContaining({
        range: 'custom',
        start: '2026-10-01',
        end: '2026-10-22',
        include_comparison: 'true',
      }),
      { signal: undefined }
    )
  })
  it('preserves a missing data source instead of claiming zero use', () => {
    const input = dashboardFixture()
    input.meta.available = false
    expect(parseUsageSummary(input).meta.available).toBe(false)
  })
  it('keeps an uncovered or sample-less dimension unknown', () => {
    const input = dashboardFixture()
    input.meta.coverage = { latency: false, success_rate: false }
    input.kpi.avgLatency = null
    input.kpi.successRate = null
    input.models[0]!.avgLatency = null
    const result = parseUsageSummary(input)
    expect(result.kpi.successRate).toBeNull()
    expect(result.kpi.avgLatency).toBeNull()
  })
  it('rejects a rate reported while its log source is off', () => {
    const input = dashboardFixture()
    input.meta.coverage.success_rate = false
    expect(() => parseUsageSummary(input)).toThrow()
    const latency = dashboardFixture()
    latency.meta.coverage.latency = false
    latency.kpi.avgLatency = null
    expect(() => parseUsageSummary(latency)).toThrow()
  })
  it('rejects out-of-range rates, string quotas and the retired log contract', () => {
    const input = dashboardFixture()
    expect(() =>
      parseUsageSummary({ ...input, kpi: { ...input.kpi, successRate: 101 } })
    ).toThrow()
    expect(() =>
      parseUsageSummary({ ...input, kpi: { ...input.kpi, totalQuota: '25' } })
    ).toThrow()
    expect(() =>
      parseUsageSummary({
        ...input,
        meta: { ...input.meta, source: 'consume_logs' },
      })
    ).toThrow()
  })
})
// Mirrors the cases in controller/dashboard_summary_test.go.
describe('summaryFits', () => {
  const custom = (start: string, end: string) =>
    ({ range: 'custom', start, end }) as const

  it('rejects a 31-day month under a 30-day log limit', () => {
    expect(
      summaryFits(currentMonthRange('2026-10-31'), 30, false, '2026-10-31')
    ).toBe(false)
    expect(
      summaryFits(currentMonthRange('2026-10-30'), 30, false, '2026-10-30')
    ).toBe(true)
  })

  it('counts the prior window toward the limit', () => {
    const today = '2026-10-22'
    expect(summaryFits({ range: '30d' }, 30, false, today)).toBe(true)
    expect(summaryFits({ range: '30d' }, 30, true, today)).toBe(false)
    expect(summaryFits({ range: '30d' }, 60, true, today)).toBe(true)
    expect(summaryFits(custom('2026-10-08', today), 30, true, today)).toBe(true)
    expect(summaryFits(custom('2026-10-07', today), 30, true, today)).toBe(
      false
    )
  })

  it('keeps today queryable at the smallest limit, without comparison', () => {
    expect(summaryFits({ range: 'today' }, 1, false, '2026-10-22')).toBe(true)
    expect(summaryFits({ range: 'today' }, 1, true, '2026-10-22')).toBe(false)
    expect(summaryFits({ range: '7d' }, 1, false, '2026-10-22')).toBe(false)
  })

  it('applies the 366-day summary cap above a larger log limit', () => {
    const today = '2026-10-22'
    expect(
      summaryFits(custom('2025-10-22', '2026-10-22'), 1000, true, today)
    ).toBe(true)
    expect(
      summaryFits(custom('2025-10-21', '2026-10-22'), 1000, false, today)
    ).toBe(false)
  })

  it('measures a past range by its own length, not its age', () => {
    const today = '2026-10-22'
    expect(
      summaryFits(custom('2026-01-01', '2026-01-15'), 30, true, today)
    ).toBe(true)
    expect(
      summaryFits(custom('2026-01-01', '2026-01-16'), 30, true, today)
    ).toBe(false)
  })

  it('counts calendar days across a DST change', () => {
    // US and EU clocks change inside March; a local-millisecond count would
    // lose an hour and round the 31 days down.
    const march = custom('2026-03-01', '2026-03-31')
    expect(summaryFits(march, 31, false, '2026-04-15')).toBe(true)
    expect(summaryFits(march, 30, false, '2026-04-15')).toBe(false)
  })

  it('treats a missing log_max_range_days as a contract violation', () => {
    expect(logRangeLimit({ log_max_range_days: 45 })).toBe(45)
    expect(() => logRangeLimit({})).toThrow('Invalid API response: /api/status')
  })
})
