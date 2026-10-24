import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  parsePerfMetricsSummary,
  parsePerfMetricsSummaryResult,
} from '../liveContracts'
const mocks = vi.hoisted(() => ({ session: vi.fn(), public: vi.fn() }))
vi.mock('../httpTransport', () => ({
  httpTransport: { request: mocks.session },
  publicHttpTransport: { request: mocks.public },
}))
import { publicCatalogApi } from '../publicCatalog'
import { getPerformanceSummary } from '../modelMarket'
beforeEach(() => vi.resetAllMocks())
describe('home native performance and pricing contracts', () => {
  const models = [
    { model_name: 'one', avg_latency_ms: 100, avg_tps: 0, success_rate: 100 },
    { model_name: 'two', avg_latency_ms: 900, avg_tps: 0, success_rate: 100 },
  ]
  it('uses the weighted global summary independently of per-model averages', () => {
    const input = { models, summary: { avg_latency_ms: 180 } }
    expect(parsePerfMetricsSummaryResult(input).summary?.avg_latency_ms).toBe(
      180
    )
    expect(parsePerfMetricsSummary(input)).toEqual(models)
  })
  it('distinguishes no sample and measured zero and rejects a malformed aggregate', () => {
    expect(
      parsePerfMetricsSummaryResult({ models: [], summary: null }).summary
    ).toBeNull()
    expect(
      parsePerfMetricsSummaryResult({
        models: [],
        summary: { avg_latency_ms: 0 },
      }).summary?.avg_latency_ms
    ).toBe(0)
    for (const summary of [
      undefined,
      {},
      { avg_latency_ms: -1 },
      { avg_latency_ms: '2' },
    ])
      expect(() =>
        parsePerfMetricsSummaryResult({ models: [], summary })
      ).toThrow()
  })
  it.each([false, true])(
    'routes summary and pricing through the chosen audience transport (%s)',
    async (authenticated) => {
      const selected = authenticated ? mocks.session : mocks.public
      const other = authenticated ? mocks.public : mocks.session
      const signal = new AbortController().signal
      selected.mockResolvedValueOnce({
        success: true,
        data: { models: [], summary: null },
      })
      await getPerformanceSummary(authenticated, signal)
      expect(selected).toHaveBeenCalledWith(
        'GET',
        '/api/perf-metrics/summary',
        expect.objectContaining({ signal, params: { hours: 24 } })
      )
      selected.mockResolvedValueOnce({
        success: true,
        data: [],
        supported_endpoint: {},
      })
      expect(
        (await publicCatalogApi.pricing(signal, authenticated)).models
      ).toEqual([])
      expect(selected).toHaveBeenLastCalledWith('GET', '/api/pricing', {
        signal,
      })
      expect(other).not.toHaveBeenCalled()
    }
  )
})
