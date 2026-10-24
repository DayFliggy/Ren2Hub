import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import OpsMonitorView from '@/views/admin/OpsMonitorView.vue'
import OpsStatusHeader from '@/components/admin/ops/OpsStatusHeader.vue'
import OpsResourcesCard from '@/components/admin/ops/OpsResourcesCard.vue'
import OpsGatewayCard from '@/components/admin/ops/OpsGatewayCard.vue'
import OpsQualityCard from '@/components/admin/ops/OpsQualityCard.vue'
import OpsStorageCards from '@/components/admin/ops/OpsStorageCards.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import {
  performanceFixture,
  measuredResource,
  qualityFixture,
  routingFixture,
} from '@/api/__tests__/opsFixtures'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
const authState = reactive({
  user: { id: 1, role: 100 } as { id: number; role: number } | null,
})
vi.mock('@/api/client', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get user() {
      return authState.user
    },
    get isRoot() {
      return (authState.user?.role ?? 0) >= 100
    },
    hasPermission: () => (authState.user?.role ?? 0) >= 100,
  }),
}))
vi.mock('@/charts/useEChart', () => ({ useEChart: vi.fn() }))
let wrapper: VueWrapper | undefined
function setup() {
  wrapper = mount(OpsMonitorView, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: {
              common: {
                confirm: 'Confirm',
                cancel: 'Cancel',
                pageInfo: '{page} of {total}',
                pageSize: 'Page size',
                itemsPerPage: 'Items per page',
              },
            },
          },
        }),
      ],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  authState.user = { id: 1, role: 100 }
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    value: false,
  })
  mocks.get.mockImplementation(async (path: string) => {
    if (path === '/api/performance/stats') return performanceFixture()
    if (path === '/api/performance/routing') return routingFixture()
    if (path === '/api/performance/quality') return qualityFixture()
    if (path === '/api/user/model-catalog') return modelCatalogFixture()
    throw new Error(`Unexpected API ${path}`)
  })
  mocks.getEnvelope.mockImplementation(async (path: string) => {
    if (path === '/api/system-task/list') return { data: [], total: 0 }
    throw new Error(`Unexpected API ${path}`)
  })
  mocks.post.mockResolvedValue({})
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

describe('historical ops layout with native data', () => {
  it('restores every section, preserves real concurrency and marks missing probes unavailable', async () => {
    const view = setup()
    await flushPromises()
    for (const name of [
      'status',
      'quality',
      'upstream',
      'gateway',
      'resources',
      'storage',
      'tasks',
    ]) {
      expect(view.find(`[data-ops-section="${name}"]`).exists()).toBe(true)
    }
    expect(view.get('[data-ops-section="storage"]').classes()).toContain(
      'ops-area--storage'
    )
    for (const gauge of ['cpu', 'memory'])
      expect(
        view.get(`[data-ops-gauge="${gauge}"] [data-ops-unavailable]`).text()
      ).toBe('Unavailable')
    expect(view.get('[data-ops-gauge="cpu"] .ops-gauge-value').text()).toBe(
      '--'
    )
    expect(view.get('[data-ops-gauge="disk"]').text()).toContain('40%')
    expect(view.getComponent(OpsResourcesCard).text()).toContain(
      'Go runtime reserved heap:'
    )
    expect(view.get('[data-ops-concurrency="users"]').text()).toContain(
      '#12 / 4'
    )
    expect(view.get('[data-ops-concurrency="channels"]').text()).toContain(
      '#92 / Unlimited'
    )
    const database = view.get('[data-ops-storage="database"]').text()
    expect(database).toContain('Unlimited')
    expect(database).toContain('1.5s')
    expect(database).toContain('Logs share the main database')
    expect(view.find('[data-ops-storage="logDatabase"]').exists()).toBe(false)
    expect(view.get('[data-ops-storage="redis"]').text()).toContain('Disabled')
    expect(view.get('[data-ops-quality-summary]').text()).toContain('95%')
    expect(
      view
        .findAll('[data-ops-health-strip] span')
        .map((cell) => cell.attributes('data-tone'))
    ).toEqual(['idle', 'good'])
    expect(view.get('[data-ops-task-summary-unavailable]').text()).toContain(
      'not the scheduler registry'
    )
    expect(view.find('pre').exists()).toBe(false)
    expect(
      mocks.get.mock.calls.every(
        ([path]) =>
          !String(path).includes('/api/ops/') &&
          !String(path).includes('/api/next')
      )
    ).toBe(true)
    expect(
      view.getComponent(OpsGatewayCard).props('overview')!.series.points
    ).toHaveLength(1)
  })
  it('shows sampled host usage and a separate unreadable log pool', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        data.system_usage = {
          cpu_percent: 12.5,
          memory_percent: 40,
          sampled_at: 1,
        }
        data.observability.resources.cpu = measuredResource(12.5)
        data.observability.resources.memory = measuredResource(40)
        data.db_pool.log_shared = false
        data.observability.storage.log_shared = false
        data.observability.storage.log_database = {
          status: 'unknown',
          latency_ms: null,
          reason: 'unavailable',
        }
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    expect(view.get('[data-ops-gauge="cpu"]').text()).toContain('12.5%')
    expect(
      view.find('[data-ops-gauge="cpu"] [data-ops-unavailable]').exists()
    ).toBe(false)
    expect(
      view
        .get('[data-ops-storage="logDatabase"] [data-ops-pool-unavailable]')
        .text()
    ).toContain('could not be read')
  })
  it('treats a negative backend threshold as a disabled check', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        data.observability.resources.cpu = measuredResource(99, -1)
        data.observability.resources.cpu.state = 'disabled'
        data.observability.checks.find((check) => check.id === 'cpu')!.status =
          'disabled'
        data.observability.checks.find((check) => check.id === 'cpu')!.reason =
          'disabled'
        data.config.monitor_cpu_threshold = -1
        data.system_usage = {
          cpu_percent: 99,
          memory_percent: 40,
          sampled_at: 1,
        }
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    expect(view.find('[data-ops-error]').exists()).toBe(false)
    expect(view.get('[data-ops-gauge="cpu"]').text()).toContain('Threshold off')
    expect(view.get('[data-ops-unmeasured]').text()).toContain('CPU')
    expect(view.get('[data-ops-coverage]').text()).toContain('1 / 4')
  })
  it('shows a CPU burst above 100% while capping its ring at a full arc', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        data.observability.resources.cpu = measuredResource(101.2)
        data.observability.resources.cpu.state = 'critical'
        data.observability.checks.find((check) => check.id === 'cpu')!.status =
          'critical'
        data.observability.checks.find((check) => check.id === 'cpu')!.reason =
          'threshold_exceeded'
        data.system_usage = {
          cpu_percent: 101.2,
          memory_percent: 40,
          sampled_at: 1,
        }
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    const gauge = view.get('[data-ops-gauge="cpu"]')
    expect(gauge.text()).toContain('101.2%')
    expect(gauge.find('.ops-metric-value--critical').exists()).toBe(true)
    expect(gauge.get('circle[transform]').attributes('stroke-dashoffset')).toBe(
      '0'
    )
  })
  it('keeps quality metrics unknown when perf metrics are off or idle', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    let enabled = false
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/quality') {
        const data = qualityFixture()
        data.enabled = enabled
        data.summary = null
        data.series[1] = { ...data.series[0]!, ts: data.series[1]!.ts }
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    const card = view.getComponent(OpsQualityCard)
    expect(card.find('[data-ops-quality-disabled]').exists()).toBe(true)
    expect(card.find('[data-ops-quality-summary]').exists()).toBe(false)
    enabled = true
    card.vm.$emit('retry')
    await flushPromises()
    const summary = card.get('[data-ops-quality-summary]').text()
    expect(summary).not.toContain('0%')
    expect(summary).toContain('Unavailable')
    expect(card.find('[data-ops-no-traffic]').exists()).toBe(true)
  })
  it('does not imply the platform has no channels when the catalog has none published', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/user/model-catalog') {
        const data = modelCatalogFixture()
        data.channels = []
        data.offers = []
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    expect(view.get('[data-ops-section="upstream"]').text()).toContain(
      'Other configured channels may exist'
    )
  })
  it('keeps resources and cache current when routing refresh fails and recovers by retry', async () => {
    let failed = false
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/routing' && !failed) {
        failed = true
        return Promise.reject(new Error('routing offline'))
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    expect(view.getComponent(OpsQualityCard).text()).toContain(
      'routing offline'
    )
    expect(view.getComponent(OpsResourcesCard).props('error')).toBe('')
    expect(view.getComponent(OpsResourcesCard).text()).toContain(
      'System resources'
    )
    expect(
      view
        .getComponent(OpsStorageCards)
        .get('[data-ops-storage="cache"]')
        .text()
    ).toContain('Memory hits')
    view.getComponent(OpsQualityCard).vm.$emit('retry')
    await flushPromises()
    expect(view.getComponent(OpsQualityCard).props('error')).toBe('')
    expect(view.get('[data-ops-top-models]').text()).toContain('fixture-model')
  })
  it('loads history recorded before opening the page and fetches distinct server buckets for a new window', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        const window = (args[0] as { window: '1h' | '24h' }).window
        data.observability.series = {
          window,
          bucket_seconds: window === '1h' ? 60 : 900,
          points: [
            {
              ts: 900,
              cpu: 10,
              memory: 20,
              disk: 40,
              request_mbps: 1,
              response_mbps: 2,
              goroutines: 5,
              heap_alloc_bytes: 1024,
            },
          ],
        }
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    const resources = view.getComponent(OpsResourcesCard)
    expect(resources.props('overview')!.series.points[0]?.ts).toBe(900)
    expect(resources.props('overview')!.series.bucket_seconds).toBe(60)
    view.getComponent(SegmentedToggle).vm.$emit('update:modelValue', '24h')
    await flushPromises()
    expect(resources.props('overview')!.series.window).toBe('24h')
    expect(resources.props('overview')!.series.bucket_seconds).toBe(900)
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/performance/stats',
      { window: '24h' },
      { signal: expect.any(AbortSignal) }
    )
  })
  it('keeps server history with a visible read error when the API fails and replaces it after retry', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    let failed = false
    let disk = 40
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        if (failed) return Promise.reject(new Error('feed offline'))
        const data = performanceFixture()
        data.observability.series.points = [
          {
            ts: 900,
            cpu: null,
            memory: null,
            disk,
            request_mbps: null,
            response_mbps: null,
            goroutines: 5,
            heap_alloc_bytes: 1024,
          },
        ]
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    failed = true
    view.getComponent(OpsStatusHeader).vm.$emit('refresh')
    await flushPromises()
    const resources = view.getComponent(OpsResourcesCard)
    expect(resources.props('error')).toBe('feed offline')
    expect(
      resources.props('overview')!.series.points.map((point) => point.disk)
    ).toEqual([40])
    expect(view.getComponent(OpsGatewayCard).props('error')).toBe('')
    failed = false
    disk = 50
    view.getComponent(OpsStatusHeader).vm.$emit('refresh')
    await flushPromises()
    expect(resources.props('error')).toBe('')
    expect(
      resources.props('overview')!.series.points.map((point) => point.disk)
    ).toEqual([50])
  })
  it('does not sample superseded or aborted routing responses', async () => {
    const start = 1_800_000_000_000
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(start)
    const defaultGet = mocks.get.getMockImplementation()!
    const pending: ((value: ReturnType<typeof routingFixture>) => void)[] = []
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/routing')
        return new Promise((resolve) => {
          pending.push(resolve)
        })
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    vi.setSystemTime(start + 1000)
    view.getComponent(OpsStatusHeader).vm.$emit('refresh')
    await flushPromises()
    pending[0]!(routingFixture())
    await flushPromises()
    expect(view.getComponent(OpsGatewayCard).props('overview')).toBeNull()
    vi.setSystemTime(start + 2000)
    pending[1]!(routingFixture())
    await flushPromises()
    expect(
      view
        .getComponent(OpsGatewayCard)
        .props('overview')!
        .series.points.map(({ ts }) => ts)
    ).toEqual([start / 1000 + 2])
  })
  it('does not display latency before its independent sample threshold', async () => {
    const defaultGet = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/routing') {
        const data = routingFixture()
        data.observations[0]!.latency_known = false
        return Promise.resolve(data)
      }
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    const row = view.get('[data-ops-top-models] tbody tr')
    expect(row.findAll('td').map((cell) => cell.text())).toEqual([
      '60',
      '10%',
      '--',
    ])
  })
  it('requires confirmation for maintenance and keeps failures visible for retry', async () => {
    mocks.post
      .mockRejectedValueOnce(new Error('maintenance denied'))
      .mockResolvedValue({})
    const view = setup()
    await flushPromises()
    const storage = view.getComponent(OpsStorageCards)
    await storage.get('.ops-maintenance-actions button').trigger('click')
    expect(mocks.post).not.toHaveBeenCalled()
    const confirm = storage.getComponent(ConfirmDialog)
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(confirm.props('open')).toBe(true)
    expect(storage.text()).toContain('maintenance denied')
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(confirm.props('open')).toBe(false)
    expect(mocks.post).toHaveBeenNthCalledWith(2, '/api/performance/gc')
    expect(storage.text()).toContain('Completed: Run garbage collection')
  })
  it('surfaces a rejected fullscreen request', async () => {
    Object.defineProperty(document.documentElement, 'requestFullscreen', {
      configurable: true,
      value: vi.fn().mockRejectedValue(new Error('fullscreen denied')),
    })
    const view = setup()
    await flushPromises()
    await view
      .getComponent(OpsStatusHeader)
      .get('button[aria-label="Fullscreen NOC"]')
      .trigger('click')
    await flushPromises()
    expect(view.text()).toContain('fullscreen denied')
  })
})

describe('ops permission lifecycle', () => {
  it.each([
    ['anonymous', null],
    ['user', { id: 2, role: 1 }],
    ['admin', { id: 2, role: 10 }],
  ] as const)(
    'does not mount or poll for an initial %s session',
    async (_label, user) => {
      vi.useFakeTimers()
      authState.user = user
      const view = setup()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(view.find('[data-ops-page]').exists()).toBe(false)
      expect(mocks.get).not.toHaveBeenCalled()
      expect(mocks.getEnvelope).not.toHaveBeenCalled()
    }
  )

  it.each([
    ['downgrade', { id: 1, role: 1 }],
    ['logout', null],
  ] as const)(
    'hides data and aborts every feed on %s, ignoring late responses',
    async (_label, user) => {
      vi.useFakeTimers()
      const view = setup()
      await flushPromises()
      expect(view.find('[data-ops-page]').exists()).toBe(true)
      const originalGet = mocks.get.getMockImplementation()!
      const pending: {
        signal: AbortSignal
        settle: () => void
      }[] = []
      mocks.get.mockImplementation(
        (path: string, query: unknown, options: { signal: AbortSignal }) =>
          new Promise((resolve, reject) => {
            pending.push({
              signal: options.signal,
              settle:
                path === '/api/performance/routing'
                  ? () => reject(new Error('old routing failure'))
                  : () => resolve(originalGet(path, query, options)),
            })
          })
      )
      mocks.getEnvelope.mockImplementation(
        (_path: string, _query: unknown, options: { signal: AbortSignal }) =>
          new Promise((resolve) => {
            pending.push({
              signal: options.signal,
              settle: () => resolve({ data: [], total: 0 }),
            })
          })
      )
      // Include the maintenance feeds as well as all four overview feeds.
      await vi.advanceTimersByTimeAsync(60_000)
      expect(pending).toHaveLength(6)
      expect(pending.every(({ signal }) => !signal.aborted)).toBe(true)
      authState.user = user
      await nextTick()
      expect(view.find('[data-ops-page]').exists()).toBe(false)
      expect(pending.every(({ signal }) => signal.aborted)).toBe(true)
      for (const { settle } of pending) settle()
      await flushPromises()
      const calls =
        mocks.get.mock.calls.length + mocks.getEnvelope.mock.calls.length
      await vi.advanceTimersByTimeAsync(60_000)
      expect(view.find('[data-ops-page]').exists()).toBe(false)
      expect(
        mocks.get.mock.calls.length + mocks.getEnvelope.mock.calls.length
      ).toBe(calls)
    }
  )

  it('remounts all feeds for a different Root identity without accepting the old response', async () => {
    const view = setup()
    await flushPromises()
    const originalGet = mocks.get.getMockImplementation()!
    let oldSignal: AbortSignal | undefined
    let finishOld: (() => void) | undefined
    mocks.get.mockImplementationOnce(
      (_path: string, _query: unknown, options: { signal: AbortSignal }) =>
        new Promise((resolve) => {
          oldSignal = options.signal
          finishOld = () => resolve(performanceFixture())
        })
    )
    view.getComponent(OpsStatusHeader).vm.$emit('refresh')
    await flushPromises()
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        data.observability.resources.disk = measuredResource(65)
        return Promise.resolve(data)
      }
      return originalGet(path, ...args)
    })
    const previousResources = view.findComponent(OpsResourcesCard)
    authState.user = { id: 2, role: 100 }
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    expect(previousResources.exists()).toBe(false)
    expect(view.get('[data-ops-gauge="disk"]').text()).toContain('65%')
    expect(
      mocks.get.mock.calls.filter(([path]) => path === '/api/performance/stats')
    ).toHaveLength(3)
    expect(mocks.getEnvelope).toHaveBeenCalledTimes(4)
    finishOld!()
    await flushPromises()
    expect(view.get('[data-ops-gauge="disk"]').text()).toContain('65%')
  })
})
