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
  it('keeps missing connectivity probes outside measured resource health', () => {
    const stats = performanceFixture()
    stats.system_usage = {
      cpu_percent: 10,
      memory_percent: 20,
      sampled_at: 3600,
    }
    const result = projectOpsOverview(
      parsePerformanceStats(stats),
      routingFixture(),
      '1h',
      [],
      3600
    )!
    expect(result.status).toBe('ok')
    expect(result.health_score).toBeNull()
    expect(
      result.checks
        .filter((check) => check.unavailable_reason === 'not_provided')
        .map((check) => check.key)
    ).toEqual(['database', 'redis'])
    expect(
      result.checks.find((check) => check.key === 'database')?.status
    ).toBe('unknown')
  })
  it('keeps Go process memory separate from unmeasured host memory and health', () => {
    const result = projectOpsOverview(
      parsePerformanceStats(performanceFixture()),
      routingFixture(),
      '1h',
      [],
      3600
    )!
    expect(result.resources?.process.heap_alloc_bytes).toBe(1_048_576)
    expect(result.resources?.memory.percent).toBeNull()
    expect(result.resources?.cpu.percent).toBeNull()
    expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
      'unknown'
    )
    expect(result.resources?.network.up_mbps).toBeNull()
    expect(result.resources?.disk.percent).toBe(40)
    expect(result.checks.find((row) => row.key === 'disk')?.status).toBe('ok')
    expect(
      result.checks.find((row) => row.key === 'database')?.value
    ).toBeNull()
    expect(result.health_score).toBeNull()
    expect(result.status).toBe('unknown')
    expect(seriesRange(result)).toEqual([0, 3600])
  })
  it('rates sampled host CPU and memory against their thresholds', () => {
    const data = performanceFixture()
    data.system_usage = {
      cpu_percent: 92,
      memory_percent: 40,
      sampled_at: 3590,
    }
    const result = projectOpsOverview(data, null, '1h', [], 3600)!
    expect(result.resources?.cpu.percent).toBe(92)
    expect(result.resources?.memory.percent).toBe(40)
    expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
      'critical'
    )
    expect(result.checks.find((row) => row.key === 'memory')?.status).toBe('ok')
    expect(result.status).toBe('critical')
  })
  it.each([
    { threshold: -1, status: 'unknown' },
    { threshold: 0, status: 'unknown' },
    { threshold: 150, status: 'ok' },
  ])(
    'accepts the backend CPU threshold $threshold and rates it $status',
    ({ threshold, status }) => {
      const data = performanceFixture()
      data.config.monitor_cpu_threshold = threshold
      data.system_usage = { cpu_percent: 120, memory_percent: 1, sampled_at: 1 }
      const result = projectOpsOverview(
        parsePerformanceStats(data),
        null,
        '1h',
        [],
        3600
      )!
      expect(result.resources?.cpu.threshold).toBe(threshold)
      expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
        status
      )
    }
  )
  it.each([
    { value: 89.9, status: 'warning' },
    { value: 90, status: 'warning' },
    { value: 90.5, status: 'warning' },
    { value: 90.99, status: 'warning' },
    { value: 91, status: 'critical' },
  ])(
    'uses the Go integer threshold boundary for a sample of $value',
    ({ value, status }) => {
      const data = performanceFixture()
      data.config.monitor_cpu_threshold = 90
      data.config.monitor_memory_threshold = 90
      data.config.monitor_disk_threshold = 90
      data.system_usage = {
        cpu_percent: value,
        memory_percent: value,
        sampled_at: 1,
      }
      data.disk_space_info = {
        total: 1000,
        used: value * 10,
        used_percent: value,
      }
      const result = projectOpsOverview(
        parsePerformanceStats(data),
        null,
        '1h',
        [],
        3600
      )!
      for (const key of ['cpu', 'memory', 'disk']) {
        expect(result.checks.find((check) => check.key === key)?.status).toBe(
          status
        )
      }
      expect(result.status).toBe(status)
    }
  )
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
  it('does not treat a failed native disk probe or disabled threshold as healthy', () => {
    const data = performanceFixture()
    data.disk_space_info = { total: 0, used: 0, used_percent: 0 }
    const unavailable = projectOpsOverview(data, null, '1h', [], 3600)!
    expect(unavailable.resources?.disk.percent).toBeNull()
    expect(unavailable.checks.find((row) => row.key === 'disk')?.status).toBe(
      'unknown'
    )
    data.disk_space_info = { total: 1000, used: 980, used_percent: 98 }
    expect(projectOpsOverview(data, null, '1h', [], 3600)?.status).toBe(
      'critical'
    )
    data.config.monitor_enabled = false
    expect(projectOpsOverview(data, null, '1h', [], 3600)?.status).toBe(
      'unknown'
    )
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
  it('accepts a container CPU burst above 100% and still rates it critical', () => {
    const data = performanceFixture()
    data.system_usage = { cpu_percent: 101.2, memory_percent: 1, sampled_at: 1 }
    const result = projectOpsOverview(
      parsePerformanceStats(data),
      null,
      '1h',
      [],
      3600
    )!
    expect(result.resources?.cpu.percent).toBe(101.2)
    expect(result.checks.find((row) => row.key === 'cpu')?.status).toBe(
      'critical'
    )
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
  it('retains the full 24-hour window even with frequent manual refreshes', () => {
    const point = (ts: number): OpsSeriesPoint => ({
      ts,
      cpu: null,
      memory: null,
      disk: 40,
      users: null,
      channels: null,
    })
    const points = appendOpsSample(
      Array.from({ length: 3000 }, (_, index) => point(100000 + index * 10)),
      point(130000)
    )
    expect(points).toHaveLength(3001)
    expect(points[0]?.ts).toBe(100000)
    for (const window of ['1h', '6h', '24h'] as const) {
      const result = projectOpsOverview(
        performanceFixture(),
        null,
        window,
        points,
        130000
      )!
      const seconds = { '1h': 3600, '6h': 21600, '24h': 86400 }[window]
      expect(
        result.series.points.every(({ ts }) => ts >= 130000 - seconds)
      ).toBe(true)
      expect(seriesRange(result)).toEqual([130000 - seconds, 130000])
    }
    expect(
      projectOpsOverview(performanceFixture(), null, '24h', points, 130000)!
        .series.points
    ).toHaveLength(3001)
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
      ['/api/performance/stats', undefined, { signal }],
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
})
