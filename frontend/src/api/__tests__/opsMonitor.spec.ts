import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  opsMonitorApi,
  parseOpsBillingMetrics,
  parseOpsChannels,
  parseOpsOverview,
  parseOpsRequests,
} from '../opsMonitor'
import {
  billingFixture,
  channelsFixture,
  overviewFixture,
  requestsFixture,
} from './opsFixtures'

type Overview = ReturnType<typeof overviewFixture>

afterEach(() => vi.restoreAllMocks())

describe('ops billing health contract', () => {
  it('reads recovery and settlement counters from the Root gateway endpoint', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue(billingFixture())
    const metrics = await opsMonitorApi.billing(signal)
    expect(get).toHaveBeenCalledWith('/api/gateway/metrics', undefined, {
      signal,
    })
    expect(metrics.billing_recovery).toEqual({
      pending: 2,
      oldest_updated_at: 1_767_225_000,
    })
    expect(metrics.billing.settlements_total).toEqual({
      confirmed: 115,
      estimated: 5,
      frozen_estimate: 2,
    })
  })

  it('rejects obsolete versions, wrong scopes and missing or invalid counters', () => {
    const valid = billingFixture()
    for (const value of [
      { ...valid, schema_version: 1 },
      { ...valid, recovery_scope: 'instance' },
      { ...valid, billing: { ...valid.billing, counter_scope: 'database' } },
      {
        ...valid,
        billing_recovery: { ...valid.billing_recovery, pending: -1 },
      },
      { ...valid, billing: { ...valid.billing, calculation_total: '123' } },
      {
        ...valid,
        billing: {
          ...valid.billing,
          settlements_total: { confirmed: 1, estimated: 0 },
        },
      },
    ])
      expect(() => parseOpsBillingMetrics(value)).toThrow(
        'Invalid API response'
      )
  })
})

describe('ops overview contract', () => {
  it('keeps unmeasured values as null and measured zeros as zero', () => {
    const overview = parseOpsOverview(overviewFixture())
    expect(overview.health_score).toBe(85)
    expect(overview.resources.cpu.percent).toBeNull()
    expect(overview.resources.network).toEqual({
      up_mbps: null,
      down_mbps: 1.1,
    })
    expect(overview.gateway.success_rate).toBeNull()
    expect(overview.gateway.granted_per_minute).toBeNull()
    expect(overview.gateway.rejected_per_minute).toBe(0)
    expect(overview.gateway.wait_bucket_bounds_ms.at(-1)).toBeNull()
    expect(overview.storage.log_database).toBeNull()
    expect(overview.storage.redis).toMatchObject({
      latency_ms: null,
      error: 'timeout',
    })
    expect(overview.storage.database.error).toBeNull()
    expect(overview.series.points[0]).toMatchObject({
      success_rate: null,
      requests_per_minute: 0,
    })
  })

  it('normalizes omitted check fields without inventing values', () => {
    const [cpu, redis, master] = parseOpsOverview(overviewFixture()).checks
    expect(cpu).toEqual({
      key: 'cpu',
      status: 'ok',
      value: 12.5,
      threshold: 90,
      error: null,
    })
    expect(redis).toMatchObject({
      value: null,
      threshold: null,
      error: 'timeout',
    })
    expect(master).toMatchObject({ value: null, threshold: null, error: null })
  })

  it.each<[string, (value: Overview) => void]>([
    ['an unknown schema', (v) => void (v.schema_version = 2)],
    ['an unknown status', (v) => void (v.status = 'degraded')],
    ['an out of range health score', (v) => void (v.health_score = 101)],
    [
      'a missing health score',
      (v) => void delete (v as Record<string, unknown>).health_score,
    ],
    ['an unknown check key', (v) => void (v.checks[0]!.key = 'gpu')],
    ['an unknown probe error', (v) => void (v.storage.redis.error = 'refused')],
    [
      'a stringified percent',
      (v) =>
        void ((v.resources.memory as Record<string, unknown>).percent = '40'),
    ],
    [
      'an omitted nullable field',
      (v) => void delete (v.gateway as Record<string, unknown>).success_rate,
    ],
    [
      'a negative counter',
      (v) => void (v.gateway.admission.granted_total = -1),
    ],
    [
      'mismatched wait buckets',
      (v) => void v.gateway.admission.wait_duration_ms_buckets.pop(),
    ],
    [
      'non-cumulative wait buckets',
      (v) => void (v.gateway.admission.wait_duration_ms_buckets[3] = 0),
    ],
    [
      'a finite last wait bound',
      (v) => void ((v.gateway.wait_bucket_bounds_ms as unknown[])[7] = 10_000),
    ],
    ['an unknown series window', (v) => void (v.series.window = '7d')],
    [
      'unordered series points',
      (v) => void v.series.points.push({ ...v.series.points[0]! }),
    ],
  ])('rejects %s', (_case, mutate) => {
    const value = overviewFixture()
    mutate(value)
    expect(() => parseOpsOverview(value)).toThrow('Invalid API response')
  })

  it('requests the selected window and rejects a series for another window', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue(overviewFixture())
    expect((await opsMonitorApi.overview('1h', signal)).series.window).toBe(
      '1h'
    )
    expect(get).toHaveBeenCalledWith(
      '/api/ops/overview',
      { window: '1h' },
      { signal }
    )
    await expect(opsMonitorApi.overview('6h')).rejects.toThrow(
      'Invalid API response'
    )
  })
})

describe('ops requests and channels contracts', () => {
  it('keeps empty buckets unmeasured', () => {
    const parsed = parseOpsRequests(requestsFixture())
    expect(parsed.series[0]).toEqual({
      ts: 1_767_222_000,
      requests: 0,
      success_rate: null,
      avg_latency_ms: 0,
      avg_tps: 0,
    })
    expect(parsed.top_models[0]?.model_name).toBe('gpt-4o')
  })

  it('rejects out-of-range windows, oversized model lists and null averages', () => {
    const requests = requestsFixture()
    expect(() => parseOpsRequests({ ...requests, hours: 169 })).toThrow()
    expect(() =>
      parseOpsRequests({
        ...requests,
        top_models: new Array(7).fill(requests.top_models[0]),
      })
    ).toThrow()
    expect(() =>
      parseOpsRequests({
        ...requests,
        summary: { ...requests.summary, avg_tps: null },
      })
    ).toThrow()
  })

  it('keeps unmeasured latency and rejects closed or unknown breaker states', () => {
    const channels = channelsFixture()
    expect(
      parseOpsChannels(channels).unhealthy[0]?.first_token_latency_ms
    ).toBeNull()
    for (const state of ['closed', 'tripped'])
      expect(() =>
        parseOpsChannels({
          ...channels,
          unhealthy: [{ ...channels.unhealthy[0], state }],
        })
      ).toThrow()
    expect(() =>
      parseOpsChannels({
        ...channels,
        unhealthy: new Array(51).fill(channels.unhealthy[0]),
      })
    ).toThrow()
  })
})
