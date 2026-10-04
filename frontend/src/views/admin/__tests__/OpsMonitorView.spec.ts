import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import {
  opsMonitorApi,
  parseOpsBillingMetrics,
  parseOpsChannels,
  parseOpsOverview,
  parseOpsRequests,
} from '@/api/opsMonitor'
import {
  parseSystemTaskSummary,
  systemManagementApi,
} from '@/api/systemManagement'
import {
  billingFixture,
  channelsFixture,
  overviewFixture,
  requestsFixture,
  summaryFixture,
} from '@/api/__tests__/opsFixtures'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import OpsMonitorView from '@/views/admin/OpsMonitorView.vue'

vi.mock('@/charts/useEChart', () => ({
  useEChart: () => ({ refresh: vi.fn(), dispatch: vi.fn() }),
}))

const Empty = defineComponent({ template: '<div />' })
let wrapper: VueWrapper | undefined

async function render(role: number, channelRead = false) {
  const pinia = createPinia()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/ops', name: 'ops-monitor', component: Empty }],
  })
  await router.push('/ops')
  setActivePinia(pinia)
  useAuthStore().persist({
    role,
    permissions: {
      admin_permissions: channelRead ? { channel: { read: true } } : {},
    },
  } as never)
  wrapper = mount(OpsMonitorView, {
    global: { plugins: [pinia, router, i18n] },
  })
  await flushPromises()
  return wrapper
}

const section = (name: string) =>
  wrapper!.find(`[data-ops-section="${name}"]`).exists()

beforeEach(async () => {
  await setLocale('en')
  await loadMessageDomain('console')
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.spyOn(opsMonitorApi, 'overview').mockResolvedValue(
    parseOpsOverview(overviewFixture())
  )
  vi.spyOn(opsMonitorApi, 'requests').mockResolvedValue(
    parseOpsRequests(requestsFixture())
  )
  vi.spyOn(opsMonitorApi, 'channels').mockResolvedValue(
    parseOpsChannels(channelsFixture())
  )
  vi.spyOn(systemManagementApi, 'summary').mockResolvedValue(
    parseSystemTaskSummary(summaryFixture())
  )
  vi.spyOn(systemManagementApi, 'tasks').mockResolvedValue({
    items: [],
    total: 0,
  })
  vi.spyOn(opsMonitorApi, 'billing').mockResolvedValue(
    parseOpsBillingMetrics(billingFixture())
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

describe('ops monitor access', () => {
  it('never mounts or requests Root-only and channel sections for a plain admin', async () => {
    const view = await render(10)
    expect(opsMonitorApi.overview).toHaveBeenCalledWith(
      '1h',
      expect.any(AbortSignal)
    )
    expect(opsMonitorApi.requests).toHaveBeenCalledWith(
      24,
      expect.any(AbortSignal)
    )
    expect(opsMonitorApi.channels).not.toHaveBeenCalled()
    expect(systemManagementApi.summary).not.toHaveBeenCalled()
    expect(systemManagementApi.tasks).not.toHaveBeenCalled()
    expect(opsMonitorApi.billing).not.toHaveBeenCalled()
    expect(section('upstream')).toBe(false)
    expect(section('tasks')).toBe(false)
    expect(section('billing')).toBe(false)
    expect(view.get('[data-ops-section="quality"]').classes()).toContain(
      'ops-area--full'
    )
  })

  it('shows upstream health to an admin holding channel.read', async () => {
    const view = await render(10, true)
    expect(opsMonitorApi.channels).toHaveBeenCalledTimes(1)
    expect(view.get('[data-ops-section="upstream"]').text()).toContain(
      'openai-a'
    )
    expect(section('tasks')).toBe(false)
  })

  it('gives Root the task registry without billing health', async () => {
    const view = await render(100)
    expect(systemManagementApi.summary).toHaveBeenCalledTimes(1)
    const tasks = view.get('[data-ops-section="tasks"]')
    expect(tasks.text()).toContain('Gateway state cleanup')
    expect(tasks.text()).toContain('Failing')
    expect(
      tasks
        .findAll('select')[0]!
        .findAll('option')
        .map((option) => option.text())
    ).toEqual(['All types', 'Batch channel test', 'Gateway state cleanup'])
    expect(view.find('[data-ops-section="billing"]').exists()).toBe(false)
  })

  it('folds task history until Root expands it', async () => {
    const view = await render(100)
    const history = view.get('[data-ops-task-history]')
    const toggle = history.get('button[aria-expanded]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(history.text()).not.toContain('Clean up task history')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(history.text()).toContain('Clean up task history')
  })
})

describe('ops monitor data states', () => {
  it('does not mount billing health section for any user', async () => {
    const view = await render(100)
    expect(view.find('[data-ops-section="billing"]').exists()).toBe(false)
  })

  it('renders unmeasured readings as "--" instead of zero', async () => {
    const view = await render(10)
    expect(view.get('[data-ops-gauge="cpu"]').text()).toContain('--')
    const kpis = view.get('[data-ops-kpis]').text()
    expect(kpis).toContain('--')
    expect(kpis).toContain('Fewer than 15 samples')
    expect(view.get('[data-ops-storage="redis"]').text()).toContain(
      'Probe timed out'
    )
  })

  it('names graded problems in the header, critical first', async () => {
    const view = await render(10)
    const reasons = view.get('[data-ops-reasons]').findAll('[data-ops-reason]')
    expect(reasons.map((item) => item.find('strong').text())).toEqual([
      'Redis',
      'Master node',
    ])
    expect(
      view.get('[data-ops-overall-status]').attributes('data-status')
    ).toBe('warning')
  })

  it('names unmeasured checks instead of calling an idle instance fully checked', async () => {
    vi.mocked(opsMonitorApi.overview).mockResolvedValue(
      parseOpsOverview({
        ...overviewFixture(),
        status: 'ok',
        health_score: 100,
        checks: [
          { key: 'cpu', status: 'ok', value: 12.5, threshold: 90 },
          { key: 'success_rate', status: 'unknown' },
        ],
      })
    )
    const view = await render(10)
    const reasons = view.get('[data-ops-reasons]').text()
    expect(reasons).toContain('Measured checks passing')
    expect(reasons).not.toContain('All checks passing')
    expect(view.get('[data-ops-unmeasured]').text()).toContain('Success rate')
  })

  it('does not call an instance without channels healthy', async () => {
    vi.mocked(opsMonitorApi.channels).mockResolvedValue(
      parseOpsChannels({
        channels: {
          total: 0,
          enabled: 0,
          manually_disabled: 0,
          auto_disabled: 0,
        },
        breakers: { closed: 0, open: 0, half_open: 0 },
        unhealthy: [],
      })
    )
    const view = await render(10, true)
    const upstream = view.get('[data-ops-section="upstream"]').text()
    expect(upstream).toContain('No upstream channels configured')
    expect(upstream).not.toContain('All upstream channels are healthy')
  })

  it('shows queue waits per bucket, not the cumulative counts', async () => {
    const view = await render(10)
    const counts = view
      .get('[data-ops-waits]')
      .findAll('.ops-histogram-count')
      .map((item) => item.text())
    expect(counts).toEqual(['1', '1', '0', '1', '0', '0', '0', '1'])
  })

  it('notes a shared log store on the main database card', async () => {
    const view = await render(10)
    expect(view.find('[data-ops-storage="log_database"]').exists()).toBe(false)
    expect(
      view.get('[data-ops-storage="database"] [data-ops-log-shared]').text()
    ).toBe('Logs share the main database')
  })

  it('keeps earlier data visible only behind an explicit stale marker', async () => {
    const view = await render(10)
    vi.mocked(opsMonitorApi.overview).mockRejectedValueOnce(
      new Error('overview down')
    )
    await view
      .get('[data-ops-section="status"] button[aria-label="Refresh"]')
      .trigger('click')
    await flushPromises()
    const alert = view.get('[data-ops-section="status"] [role="alert"]')
    expect(alert.text()).toContain('overview down')
    expect(alert.text()).toContain('Retry')
    expect(
      view.get('[data-ops-section="resources"]').attributes('data-ops-stale')
    ).toBe('true')
    expect(view.get('[data-ops-gauge="memory"]').text()).toContain('40.1')
  })

  it('aborts the running overview request when the trend window changes', async () => {
    const view = await render(10)
    let firstSignal: AbortSignal | undefined
    vi.mocked(opsMonitorApi.overview).mockImplementationOnce(
      (_window, signal) => {
        firstSignal = signal
        return new Promise(() => {})
      }
    )
    await view
      .get('[data-ops-section="status"] button[aria-label="Refresh"]')
      .trigger('click')
    const sixHours = view
      .findAll('[role="radio"]')
      .find((option) => option.text() === '6 hours')!
    await sixHours.trigger('click')
    expect(firstSignal?.aborted).toBe(true)
    expect(opsMonitorApi.overview).toHaveBeenLastCalledWith(
      '6h',
      expect.any(AbortSignal)
    )
  })
})
