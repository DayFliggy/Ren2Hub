import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  appendOpsSample,
  opsMonitorApi,
  parsePerformanceStats,
  parseRoutingSnapshot,
  projectOpsOverview,
  seriesRange,
  type OpsSeriesPoint,
} from '../opsMonitor'
import { performanceFixture, routingFixture } from './opsFixtures'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('../client', () => ({ api: mocks }))
beforeEach(() => vi.clearAllMocks())

describe('native ops monitoring', () => {
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
    ['config', 'monitor_disk_threshold', 101],
    ['config', 'disk_cache_path', 9],
    ['memory_stats', 'alloc', -1],
  ])('rejects malformed %s.%s in the API feed', (section, key, value) => {
    const data = performanceFixture() as unknown as Record<
      string,
      Record<string, unknown>
    >
    data[section as string]![key as string] = value
    expect(() => parsePerformanceStats(data)).toThrow('Invalid API response')
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
    const signal = new AbortController().signal
    await opsMonitorApi.stats(signal)
    await opsMonitorApi.routing(signal)
    expect(mocks.get.mock.calls).toEqual([
      ['/api/performance/stats', undefined, { signal }],
      ['/api/performance/routing', undefined, { signal }],
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
