import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { getUsageSummary, parseUsageSummary } from '@/api/dashboard'
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
    await getUsageSummary({ range: 'today' }, signal)
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/self/summary',
      { range: 'today', tz_offset: '-480' },
      { signal }
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
