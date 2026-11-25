import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  appendOpsSample,
  opsMonitorApi,
  parsePerformanceQuality,
  parsePerformanceStats,
  parseRoutingSnapshot,
  projectOpsOverview,
  seriesRange,
  type OpsSeriesPoint,
} from '../opsMonitor'
import {
  performanceFixture,
  measuredResource,
  qualityFixture,
  routingFixture,
} from './opsFixtures'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('../client', () => ({ api: mocks }))
beforeEach(() => vi.clearAllMocks())

describe('native ops monitoring', () => {
  it('preserves server probe failures, disabled checks and measured coverage', () => {
    const data = performanceFixture()
    data.observability.status = 'critical'
    data.observability.storage.database = {
      status: 'critical',
      latency_ms: null,
      reason: 'timeout',
    }
    data.observability.checks.find((check) => check.id === 'database')!.status =
      'critical'
    data.observability.checks.find((check) => check.id === 'database')!.reason =
      'timeout'
    data.observability.coverage.measured = 2
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(result.status).toBe('critical')
    expect(result.health_score).toBeNull()
    expect(result.observation?.coverage).toEqual({ measured: 2, total: 4 })
    expect(
      result.checks.find((check) => check.key === 'database')
    ).toMatchObject({ status: 'critical', reason: 'timeout', value: null })
    expect(result.checks.find((check) => check.key === 'redis')?.status).toBe(
      'disabled'
    )
  })
  it('keeps unavailable resources, unmeasured traffic and GC null rather than using Go heap', () => {
    const data = performanceFixture()
    // No baseline yet: a fresh, reason-free snapshot without rates.
    data.observability.traffic.inbound_mbps = null
    data.observability.traffic.outbound_mbps = null
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      routingFixture(),
      '1h',
      [],
      3600
    )!
    expect(result.resources?.process.heap_alloc_bytes).toBe(1_048_576)
    expect(result.resources?.process.last_gc_pause_ms).toBeNull()
    expect(result.resources?.memory.percent).toBeNull()
    expect(result.resources?.cpu.percent).toBeNull()
    expect(result.resources?.network).toEqual({
      up_mbps: null,
      down_mbps: null,
    })
    expect(result.resources?.disk.percent).toBe(40)
    expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
      'unknown'
    )
    expect(result.status).toBe('unknown')
    expect(seriesRange(result)).toEqual([0, 3600])
  })
  it('preserves container CPU bursts and disabled thresholds without a second client health model', () => {
    const data = performanceFixture()
    data.observability.resources.cpu = measuredResource(120, -1)
    data.observability.resources.cpu.state = 'disabled'
    const check = data.observability.checks.find((check) => check.id === 'cpu')!
    check.status = 'disabled'
    check.reason = 'disabled'
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(result.resources?.cpu).toEqual({ percent: 120, threshold: -1 })
    expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
      'disabled'
    )
    expect(result.status).toBe('unknown')
  })
  it('reads DB pool counters and keeps an unreadable pool null', () => {
    const data = JSON.parse(JSON.stringify(performanceFixture()))
    data.db_pool.main = null
    data.db_pool.log_shared = false
    const parsed = parsePerformanceStats(data)
    expect(parsed.db_pool).toEqual({ main: null, log: null, log_shared: false })
    expect(parsePerformanceStats(performanceFixture()).db_pool.main).toEqual(
      performanceFixture().db_pool.main
    )
    delete data.db_pool
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it.each([
    ['cache_stats', 'memory_cache_hits', -1],
    ['cache_stats', 'current_disk_usage_bytes', '0'],
    ['disk_space_info', 'used_percent', 101],
    ['disk_space_info', 'total', -1],
    ['config', 'monitor_enabled', 'true'],
    ['config', 'monitor_disk_threshold', 90.5],
    ['config', 'monitor_cpu_threshold', '90'],
    ['config', 'disk_cache_path', 9],
    ['memory_stats', 'alloc', -1],
    ['db_pool', 'log_shared', 'true'],
    ['db_pool', 'main', { in_use: 1 }],
  ])('rejects malformed %s.%s in the API feed', (section, key, value) => {
    const data = performanceFixture() as unknown as Record<
      string,
      Record<string, unknown>
    >
    data[section as string]![key as string] = value
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it.each([
    { cpu_percent: -1, memory_percent: 1 },
    { cpu_percent: Number.POSITIVE_INFINITY, memory_percent: 1 },
    { cpu_percent: 1, memory_percent: 101 },
  ])('rejects an impossible host usage sample %o', (usage) => {
    const data = performanceFixture()
    data.system_usage = { ...usage, sampled_at: 1 }
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it('sums hourly quality and keeps idle hours without a rate', () => {
    const parsed = parsePerformanceQuality(qualityFixture())
    expect(parsed.summary).toEqual({
      requests: 40,
      success_rate: 95,
      avg_latency_ms: 820,
      avg_tps: 31.5,
    })
    expect(parsed.series[0]).toMatchObject({
      request_count: 0,
      success_rate: null,
      avg_latency_ms: null,
    })
  })
  it('does not report 0 t/s when no hour measured throughput', () => {
    const data = qualityFixture()
    data.series[1]!.avg_tps = null
    data.summary = { success_rate: 95, avg_latency_ms: 820, avg_tps: 0 }
    expect(parsePerformanceQuality(data).summary?.avg_tps).toBeNull()
    data.summary = null
    expect(parsePerformanceQuality(data).summary).toBeNull()
  })
  it('rejects impossible quality counts and rates', () => {
    const data = qualityFixture()
    data.series[1]!.success_count = 41
    expect(() => parsePerformanceQuality(data)).toThrow('Invalid API response')
    const rate = qualityFixture()
    rate.series[1]!.success_rate = 101
    expect(() => parsePerformanceQuality(rate)).toThrow('Invalid API response')
  })
  it('preserves zero values and missing optional metrics without filling probes', () => {
    const data: Record<string, unknown> = {
      ...performanceFixture(),
      config: {},
      cache_stats: { memory_cache_hits: 0 },
    }
    const parsed = parsePerformanceStats(data)
    expect(parsed.cache_stats.memory_cache_hits).toBe(0)
    expect(parsed.cache_stats.current_disk_usage_bytes).toBeNull()
    expect(
      projectOpsOverview(parsed, null, '1h', [], 3600)?.resources
        ?.monitor_enabled
    ).toBeNull()
  })
  it('keeps stream and operation identity, milliseconds and latency sample coverage', () => {
    const parsed = parseRoutingSnapshot(routingFixture())
    expect(parsed.observations[0]).toMatchObject({
      operation: 1,
      latency_known: true,
      updated_at: 1_792_353_600_000,
    })
    const legacy = JSON.parse(JSON.stringify(routingFixture()))
    delete legacy.observations[0].latency_known
    delete legacy.observations[0].latency_samples
    delete legacy.observations[0].operation
    expect(parseRoutingSnapshot(legacy).observations[0]).toMatchObject({
      operation: null,
      latency_known: false,
      latency_samples: null,
    })
  })
  it('does not accept latency coverage without samples or classify zero observations as known', () => {
    const data = JSON.parse(JSON.stringify(routingFixture()))
    delete data.observations[0].latency_samples
    data.observations[0].samples = 0
    expect(parseRoutingSnapshot(data).observations[0]).toMatchObject({
      known: false,
      latency_known: false,
      latency_samples: null,
    })
  })
  it('keeps explicit zero cache reuse and distinguishes unreported cache quality', () => {
    const fields = {
      channel_id: 9,
      model: 'test',
      operation: 1,
      stream: true,
      mode: 'explicit',
      known: false,
      band: 0,
      reason: 'insufficient_evidence',
      coverage: 0,
      token_rate: 0,
      samples: 0,
      sessions: 0,
    }
    const value = {
      ...routingFixture(),
      cache: { free_requests: 20, explorations: 1, bands: [fields] },
    }
    expect(parseRoutingSnapshot(value).cache?.bands[0]).toMatchObject({
      known: false,
      coverage: 0,
      token_rate: 0,
    })
    expect(parseRoutingSnapshot(routingFixture()).cache).toBeUndefined()
    value.cache.bands[0]!.coverage = 2
    expect(() => parseRoutingSnapshot(value)).toThrow('Invalid API response')
  })
  it('rejects invalid concurrency and observation metrics', () => {
    const data = routingFixture()
    data.channels[0]!.in_flight = -1
    expect(() => parseRoutingSnapshot(data)).toThrow('Invalid API response')
    data.channels[0]!.in_flight = 0
    data.observations[0]!.error_rate = 1.01
    expect(() => parseRoutingSnapshot(data)).toThrow('Invalid API response')
  })
  it('keeps page-session gaps and bounds retained history without backfilling', () => {
    const point = (ts: number, users: number | null): OpsSeriesPoint => ({
      ts,
      cpu: null,
      memory: null,
      disk: null,
      users,
      channels: null,
    })
    let points = appendOpsSample([point(1, 2)], point(100000, null))
    expect(points).toEqual([point(100000, null)])
    points = appendOpsSample(points, point(100000, 0))
    expect(points).toEqual([point(100000, 0)])
    const result = projectOpsOverview(
      null,
      routingFixture(),
      '1h',
      [point(90000, 1), ...points],
      100000
    )!
    expect(result.series.points).toEqual([point(100000, 0)])
    expect(seriesRange(result)).toEqual([96400, 100000])
  })
  it('uses server instance history, preserving gaps and fractional bucket averages', () => {
    const data = performanceFixture()
    data.observability.series = {
      window: '24h',
      bucket_seconds: 900,
      points: [
        {
          ts: 450,
          cpu: null,
          memory: null,
          disk: null,
          inbound_mbps: null,
          outbound_mbps: null,
          goroutines: null,
          heap_alloc_bytes: null,
          user_in_flight: null,
          channel_in_flight: null,
        },
        {
          ts: 1350,
          cpu: 15,
          memory: 40,
          disk: 40,
          inbound_mbps: 1,
          outbound_mbps: 2,
          goroutines: 2.5,
          heap_alloc_bytes: 1024,
          user_in_flight: null,
          channel_in_flight: null,
        },
      ],
    }
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(result.series.window).toBe('24h')
    expect(result.series.bucket_seconds).toBe(900)
    expect(result.series.points).toEqual(
      data.observability.series.points.map(
        ({ user_in_flight: _u, channel_in_flight: _c, ...point }) => ({
          ...point,
          users: null,
          channels: null,
        })
      )
    )
    expect(seriesRange(result)).toEqual([-82800, 3600])
  })
  it('preserves stale snapshots and an unavailable process instead of inventing measurements', () => {
    const data = performanceFixture()
    data.observability.stale = true
    data.observability.status = 'stale'
    data.observability.process = null
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(result.status).toBe('stale')
    expect(Object.values(result.resources!.process)).toEqual([
      null,
      null,
      null,
      null,
      null,
    ])
  })
  it.each([
    ['coverage', { measured: 5, total: 4 }],
    ['process', { goroutines: -1 }],
    ['checks', [{ id: 'unknown_probe', status: 'ok', reason: '' }]],
    ['series', { window: '1h', bucket_seconds: 0, points: [] }],
  ])('rejects malformed observation %s', (key, value) => {
    const data = performanceFixture()
    Object.assign(data.observability, { [key]: value })
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it.each([
    ['host_interface', 'eth0'],
    ['application_listener', ''],
  ] as const)(
    'reads %s traffic from the server side: up is outbound, down inbound',
    (scope, name) => {
      const data = performanceFixture()
      data.observability.traffic = {
        scope,
        inbound_mbps: 1,
        outbound_mbps: 7,
        interface: name,
        reason: '',
      }
      const result = projectOpsOverview(
        parsePerformanceStats(data),
        null,
        '1h',
        [],
        3600
      )!
      expect(result.observation!.traffic).toEqual(data.observability.traffic)
      expect(result.resources!.network).toEqual({ up_mbps: 7, down_mbps: 1 })
    }
  )
  it('accepts a host read failure and a stale snapshot without rates', () => {
    const failed = performanceFixture()
    failed.observability.traffic = {
      scope: 'host_interface',
      inbound_mbps: null,
      outbound_mbps: null,
      interface: 'eth0',
      reason: 'host_interface_missing',
    }
    expect(parsePerformanceStats(failed).observability.traffic.reason).toBe(
      'host_interface_missing'
    )
    const stale = performanceFixture()
    stale.observability.stale = true
    stale.observability.traffic.inbound_mbps = null
    stale.observability.traffic.outbound_mbps = null
    expect(parsePerformanceStats(stale).observability.traffic).toMatchObject({
      inbound_mbps: null,
      outbound_mbps: null,
    })
  })
  const hostTraffic = {
    scope: 'host_interface',
    inbound_mbps: 1,
    outbound_mbps: 7,
    interface: 'eth0',
    reason: '',
  }
  it.each([
    [
      'the retired body scope',
      { ...hostTraffic, scope: 'application_http_body' },
    ],
    ['an unknown scope', { ...hostTraffic, scope: 'host_network' }],
    [
      'the retired field names',
      {
        scope: 'application_listener',
        request_mbps: 1,
        response_mbps: 7,
        interface: '',
        reason: '',
      },
    ],
    ['a missing interface', { ...hostTraffic, interface: undefined }],
    ['a missing reason', { ...hostTraffic, reason: undefined }],
    ['a negative rate', { ...hostTraffic, inbound_mbps: -1 }],
    ['a string rate', { ...hostTraffic, outbound_mbps: '7' }],
    ['a non-finite rate', { ...hostTraffic, outbound_mbps: Infinity }],
    ['an unknown reason', { ...hostTraffic, reason: 'host_timeout' }],
    [
      'a failure reason beside a measured rate',
      { ...hostTraffic, inbound_mbps: null, reason: 'host_counters_invalid' },
    ],
    [
      'a listener naming an interface',
      { ...hostTraffic, scope: 'application_listener' },
    ],
  ])('rejects traffic with %s', (_name, traffic) => {
    const data = performanceFixture()
    Object.assign(data.observability, { traffic })
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it('rejects history points under the retired field names', () => {
    const data = performanceFixture()
    const { inbound_mbps, outbound_mbps, ...point } =
      data.observability.series.points[0]!
    data.observability.series.points[0] = {
      ...point,
      request_mbps: inbound_mbps,
      response_mbps: outbound_mbps,
    } as never
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it('rejects an unavailable resource carrying a synthetic zero measurement', () => {
    const data = performanceFixture()
    data.observability.resources.cpu.percent = 0
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
  })
  it('uses native reads and maintenance endpoints and propagates cancellation', async () => {
    mocks.get
      .mockResolvedValueOnce(performanceFixture())
      .mockResolvedValueOnce(routingFixture())
      .mockResolvedValueOnce(qualityFixture())
    const signal = new AbortController().signal
    await opsMonitorApi.stats(signal)
    await opsMonitorApi.routing(signal)
    await opsMonitorApi.quality(signal)
    expect(mocks.get.mock.calls).toEqual([
      ['/api/performance/stats', { window: '1h' }, { signal }],
      ['/api/performance/routing', undefined, { signal }],
      ['/api/performance/quality', { hours: '24' }, { signal }],
    ])
    await opsMonitorApi.execute('gc')
    await opsMonitorApi.execute('reset_stats')
    await opsMonitorApi.execute('disk_cache')
    expect(mocks.post.mock.calls).toEqual([
      ['/api/performance/gc'],
      ['/api/performance/reset_stats'],
    ])
    expect(mocks.delete).toHaveBeenCalledExactlyOnceWith(
      '/api/performance/disk_cache'
    )
  })
  it('parses routing breakers, cooldowns, resolved names and rejects invalid breaker states', () => {
    const parsed = parseRoutingSnapshot({
      ...routingFixture(),
      users: [{ id: 1, name: 'alice', limit: 4, in_flight: 2 }],
      channels: [{ id: 9, name: 'Primary', limit: 10, in_flight: 3 }],
      breakers: [
        {
          channel_id: 9,
          channel_name: 'Primary',
          model: 'gpt-4o',
          state: 'open',
          open_until: 1_800_000_060_000,
          probe_in_flight: false,
          trigger: 'consecutive_errors',
          last_status: 502,
          last_error_code: 'upstream_error',
          opened_at: 1_800_000_000_000,
          reopens: 2,
        },
      ],
      cooldowns: [
        {
          channel_id: 11,
          channel_name: 'Fallback',
          until: 1_800_000_030_000,
          source: 'retry_after',
        },
      ],
    })
    expect(parsed.users[0]?.name).toBe('alice')
    expect(parsed.channels[0]?.name).toBe('Primary')
    expect(parsed.breakers).toHaveLength(1)
    expect(parsed.breakers?.[0]).toMatchObject({
      channel_id: 9,
      channel_name: 'Primary',
      state: 'open',
      last_status: 502,
    })
    expect(parsed.cooldowns?.[0]).toMatchObject({
      channel_id: 11,
      channel_name: 'Fallback',
      source: 'retry_after',
    })
    expect(() =>
      parseRoutingSnapshot({
        ...routingFixture(),
        breakers: [
          {
            channel_id: 9,
            model: 'gpt-4o',
            state: 'closed',
            open_until: 0,
            probe_in_flight: false,
            trigger: '',
            opened_at: 0,
            reopens: 0,
          },
        ],
      })
    ).toThrow('Invalid API response')
  })
  it('maps server user_in_flight and channel_in_flight series and supports variable quality hours', async () => {
    const data = performanceFixture()
    data.observability.series.points = [
      {
        ts: 3600,
        cpu: 20,
        memory: 30,
        disk: 40,
        inbound_mbps: 1,
        outbound_mbps: 2,
        goroutines: 12,
        heap_alloc_bytes: 2048,
        user_in_flight: 3,
        channel_in_flight: 5,
      },
    ]
    const projected = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(projected.series.points[0]?.users).toBe(3)
    expect(projected.series.points[0]?.channels).toBe(5)
    mocks.get.mockResolvedValueOnce(qualityFixture())
    await opsMonitorApi.quality(undefined, 72)
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/performance/quality',
      { hours: '72' },
      { signal: undefined }
    )
  })
})
