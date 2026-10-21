import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { getUsageSummary, parseUsageSummary } from '@/api/dashboard'
import { dashboardFixture } from './dashboardFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
afterEach(() => vi.restoreAllMocks())
describe('native usage summary', () => {
  it('keeps unavailable dimensions null and converts native quota exactly', () => {
    const result = parseUsageSummary(dashboardFixture())
    expect(result.kpi.totalQuota).toBe('25')
    expect(result.kpi.successRate).toBeNull()
    expect(result.kpi.avgLatency).toBeNull()
    expect(result.token_trend[0]).toMatchObject({
      total: 10,
      input: null,
      output: null,
      cache_read: null,
      hit_rate: null,
    })
    expect(result.meta.coverage.success_rate).toBe(false)
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
  it('preserves disabled logging instead of claiming zero measured use', () => {
    const input = dashboardFixture()
    input.meta.available = false
    input.meta.coverage.consume_logs = false
    expect(parseUsageSummary(input).meta.available).toBe(false)
  })
  it('preserves an explicitly omitted comparison for distribution-only queries', () => {
    const input = { ...dashboardFixture(), comparison: null }
    expect(parseUsageSummary(input).comparison).toBeNull()
  })
  it('rejects fabricated success rates and legacy string quotas', () => {
    const input = dashboardFixture()
    expect(() =>
      parseUsageSummary({ ...input, kpi: { ...input.kpi, successRate: 99.9 } })
    ).toThrow()
    expect(() =>
      parseUsageSummary({ ...input, kpi: { ...input.kpi, totalQuota: '25' } })
    ).toThrow()
  })
})
