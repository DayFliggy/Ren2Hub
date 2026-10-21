import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
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
vi.mock('@/api/client', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isRoot: true, hasPermission: () => true }),
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
      'Go runtime system memory:'
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
    expect(view.get('[data-ops-storage="redis"]').text()).toContain('Unknown')
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
        data.db_pool.log_shared = false
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
  it('keeps independently completed feeds continuous across seconds and window changes', async () => {
    const start = 1_800_000_000_000
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(start)
    const defaultGet = mocks.get.getMockImplementation()!
    let performanceCalls = 0
    let completeRouting!: (value: ReturnType<typeof routingFixture>) => void
    mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
      if (path === '/api/performance/stats') {
        const data = performanceFixture()
        data.disk_space_info.used_percent = 40 + performanceCalls++
        return Promise.resolve(data)
      }
      if (path === '/api/performance/routing')
        return new Promise((resolve) => {
          completeRouting = resolve
        })
      return defaultGet(path, ...args)
    })
    const view = setup()
    await flushPromises()
    vi.setSystemTime(start + 1000)
    completeRouting(routingFixture())
    await flushPromises()
    vi.setSystemTime(start + 30_000)
    view.getComponent(OpsStatusHeader).vm.$emit('refresh')
    await flushPromises()
    expect(
      view.getComponent(OpsGatewayCard).props('overview')!.series.points
    ).toHaveLength(1)
    vi.setSystemTime(start + 31_000)
    const routing = routingFixture()
    routing.users[0]!.in_flight = 3
    completeRouting(routing)
    await flushPromises()
    const resources = view.getComponent(OpsResourcesCard)
    const gateway = view.getComponent(OpsGatewayCard)
    expect(
      resources
        .props('overview')!
        .series.points.map(({ ts, disk }) => [ts, disk])
    ).toEqual([
      [start / 1000, 40],
      [start / 1000 + 30, 41],
    ])
    expect(
      gateway
        .props('overview')!
        .series.points.map(({ ts, users }) => [ts, users])
    ).toEqual([
      [start / 1000 + 1, 2],
      [start / 1000 + 31, 3],
    ])
    view.getComponent(SegmentedToggle).vm.$emit('update:modelValue', '24h')
    await flushPromises()
    for (const card of [resources, gateway]) {
      expect(card.props('overview')!.series.window).toBe('24h')
      expect(card.props('overview')!.series.points).toHaveLength(2)
    }
  })
  it.each(['stats', 'routing'] as const)(
    'records repeated %s failures as gaps without breaking its peer, then resumes with fresh values',
    async (endpoint) => {
      const start = 1_800_000_000_000
      vi.useFakeTimers({ toFake: ['Date'] })
      vi.setSystemTime(start)
      const defaultGet = mocks.get.getMockImplementation()!
      let failed = false
      let recovered = false
      mocks.get.mockImplementation((path: string, ...args: unknown[]) => {
        if (path === `/api/performance/${endpoint}` && failed)
          return Promise.reject(new Error('feed offline'))
        if (path === '/api/performance/stats') {
          const data = performanceFixture()
          if (recovered) data.disk_space_info.used_percent = 50
          return Promise.resolve(data)
        }
        if (path === '/api/performance/routing') {
          const data = routingFixture()
          if (recovered) data.users[0]!.in_flight = 3
          return Promise.resolve(data)
        }
        return defaultGet(path, ...args)
      })
      const view = setup()
      await flushPromises()
      failed = true
      for (const elapsed of [30_000, 60_000]) {
        vi.setSystemTime(start + elapsed)
        view.getComponent(OpsStatusHeader).vm.$emit('refresh')
        await flushPromises()
      }
      failed = false
      recovered = true
      vi.setSystemTime(start + 90_000)
      view.getComponent(OpsStatusHeader).vm.$emit('refresh')
      await flushPromises()
      const disks = view
        .getComponent(OpsResourcesCard)
        .props('overview')!
        .series.points.map(({ disk }) => disk)
      const users = view
        .getComponent(OpsGatewayCard)
        .props('overview')!
        .series.points.map(({ users }) => users)
      expect(disks).toEqual(
        endpoint === 'stats' ? [40, null, null, 50] : [40, 40, 40, 50]
      )
      expect(users).toEqual(
        endpoint === 'routing' ? [2, null, null, 3] : [2, 2, 2, 3]
      )
    }
  )
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
