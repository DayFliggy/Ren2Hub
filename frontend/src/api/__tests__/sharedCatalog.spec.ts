import { beforeEach, describe, expect, it, vi } from 'vitest'
import { modelCatalogFixture, nativeTokenFixture } from './modelCatalogFixture'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ api: mocks }))
import { getModelCatalog, parseModelCatalog } from '../modelCatalog'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { getTokenClientConfig, loadClientConfigCatalog } from '../clientConfig'
import { getUsageSummary, parseUsageSummary } from '../dashboard'
import { parseRoutingSnapshot } from '../opsMonitor'
beforeEach(() => vi.clearAllMocks())
describe('shared native model catalog', () => {
  it('uses server prices without applying the channel multiplier a second time', () => {
    const data = parseModelCatalog(modelCatalogFixture()),
      market = buildModelMarketCatalog(data)
    expect(data.vendors[0]?.id).toBe(-10)
    expect(market.models[0]?.capabilities).toBeNull()
    expect(market.models[0]?.offers[0]?.pricing.priceLines).toEqual([
      { key: 'input', amount: '0.25', unit: 'per_million_tokens' },
      { key: 'output', amount: '1', unit: 'per_million_tokens' },
    ])
  })
  it('keeps explicit zero and unavailable prices distinct', () => {
    const data = modelCatalogFixture()
    data.models[0]!.price_status = 'zero'
    data.models[0]!.pricing_values = { request: '0' }
    expect(parseModelCatalog(data).models[0]!.pricing_values.request).toBe('0')
    data.models[0]!.price_status = 'unavailable'
    data.models[0]!.pricing_values = {}
    data.models[0]!.price_reason = 'dynamic expression'
    expect(parseModelCatalog(data).models[0]!.price_reason).toBe(
      'dynamic expression'
    )
  })
  it.each([
    'numeric_ratio',
    'numeric_price',
    'dangling_offer',
    'unsupported_capabilities',
  ])('rejects invalid catalog %s', (kind) => {
    const data = JSON.parse(JSON.stringify(modelCatalogFixture()))
    if (kind === 'numeric_ratio') data.channels[0].channel_ratio = 1
    if (kind === 'numeric_price') data.models[0].pricing_values.input = 1
    if (kind === 'dangling_offer') data.offers[0].channel_id = 900
    if (kind === 'unsupported_capabilities')
      data.models[0].capabilities = { context_limit: 128000 }
    expect(() => parseModelCatalog(data)).toThrow('Invalid API response')
  })
  it('uses the selected key and propagates cancellation without compatibility requests', async () => {
    mocks.get.mockResolvedValue(modelCatalogFixture())
    const signal = new AbortController().signal
    await getModelCatalog(1, signal)
    expect(mocks.get).toHaveBeenCalledExactlyOnceWith(
      '/api/user/model-catalog',
      { token_id: 1 },
      { signal }
    )
  })
  it('checks key availability and protocol eligibility again when exporting', async () => {
    mocks.get.mockImplementation((path: string) =>
      Promise.resolve(
        path.startsWith('/api/token/')
          ? nativeTokenFixture
          : modelCatalogFixture()
      )
    )
    expect((await loadClientConfigCatalog(1)).protocolModels.codex).toEqual([
      'fixture-model',
    ])
    const result = await getTokenClientConfig(
      1,
      'codex',
      'fixture-model',
      'https://api.example.test'
    )
    expect(result.content).toContain('env_key = "REN2HUB_API_KEY"')
    expect(result.content).not.toContain('sk-')
    mocks.get.mockImplementation((path: string) => {
      const data = modelCatalogFixture()
      data.offers = []
      return Promise.resolve(
        path.startsWith('/api/token/') ? nativeTokenFixture : data
      )
    })
    await expect(
      getTokenClientConfig(
        1,
        'codex',
        'fixture-model',
        'https://api.example.test'
      )
    ).rejects.toThrow('model_unavailable')
  })
  it('requires explicit local Pi limits and rejects disabled keys', async () => {
    mocks.get.mockImplementation((path: string) =>
      Promise.resolve(
        path.startsWith('/api/token/')
          ? nativeTokenFixture
          : modelCatalogFixture()
      )
    )
    await expect(
      getTokenClientConfig(1, 'pi', 'fixture-model', 'https://api.example.test')
    ).rejects.toThrow('pi_settings_required')
    mocks.get.mockImplementation((path: string) =>
      Promise.resolve(
        path.startsWith('/api/token/')
          ? { ...nativeTokenFixture, status: 2 }
          : modelCatalogFixture()
      )
    )
    await expect(loadClientConfigCatalog(1)).rejects.toThrow(
      'token_unavailable'
    )
  })
})
function usageSummaryFixture() {
  return {
    kpi: {
      totalTokens: 4,
      totalQuota: 5,
      totalRequests: 1,
      avgLatency: null,
      successRate: null,
    },
    comparison: { quotaDelta: null, requestsDelta: null },
    models: [],
    hourly: [],
    flow: [],
    token_trend: [
      {
        date: '2026-10-18',
        total: 4,
        input: null,
        output: null,
        cache_create: null,
        cache_read: null,
        hit_rate: null,
      },
    ],
    distribution: [],
    meta: {
      start: 1,
      end: 2,
      tz_offset: 0,
      generated_at: 2,
      available: true,
      source: 'consume_logs',
      cache_coverage: {
        records: 1,
        read_records: 0,
        write_records: 0,
        invalid_records: 0,
      },
      coverage: {
        consume_logs: true,
        cache_usage: false,
        latency: false,
        success_rate: false,
      },
    },
  }
}
it('preserves unknown usage metrics and rejects false success coverage', () => {
  const summary = usageSummaryFixture()
  expect(parseUsageSummary(summary).kpi).toMatchObject({
    totalQuota: '5',
    avgLatency: null,
    successRate: null,
  })
  expect(parseUsageSummary(summary).token_trend[0]?.input).toBeNull()
  expect(() =>
    parseUsageSummary({
      ...summary,
      meta: {
        ...summary.meta,
        coverage: { ...summary.meta.coverage, success_rate: true },
      },
    })
  ).toThrow()
})
it('keeps routing observation unknown instead of treating zero samples as measured health', () => {
  const parsed = parseRoutingSnapshot({
    users: [],
    channels: [{ id: 9, limit: 0, in_flight: 2 }],
    observations: [
      {
        channel_id: 9,
        model: 'fixture',
        stream: false,
        samples: 0,
        error_rate: 0,
        latency_ms: 0,
        known: false,
        updated_at: 1,
      },
    ],
  })
  expect(parsed.channels[0]?.limit).toBe(0)
  expect(parsed.observations[0]?.known).toBe(false)
})

it.each([-480, -330, 210])(
  'sends the native Date timezone offset %i unchanged',
  async (offset) => {
    const timezone = vi
      .spyOn(Date.prototype, 'getTimezoneOffset')
      .mockReturnValue(offset)
    try {
      mocks.get.mockResolvedValue(usageSummaryFixture())
      await getUsageSummary({ range: 'today' })
      expect(mocks.get).toHaveBeenCalledExactlyOnceWith(
        '/api/data/self/summary',
        { tz_offset: String(offset), range: 'today' },
        { signal: undefined }
      )
    } finally {
      timezone.mockRestore()
    }
  }
)
