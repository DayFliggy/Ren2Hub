import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import SystemSettingsMaintenanceActions from '../SystemSettingsMaintenanceActions.vue'

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))

/** Shape of controller.PerformanceStats with common.DiskCacheStats counters. */
function performanceStats(cacheStats: Record<string, number>) {
  return {
    cache_stats: cacheStats,
    memory_stats: {
      alloc: 1_048_576,
      total_alloc: 8_388_608,
      sys: 4_194_304,
      num_gc: 3,
      num_goroutine: 12,
    },
    disk_cache_info: {
      path: '/tmp/cache',
      exists: true,
      file_count: 2,
      total_size: 4096,
    },
    disk_space_info: { total: 1000, free: 600, used: 400, used_percent: 40 },
    config: { disk_cache_enabled: true, monitor_enabled: false },
    // C4 always sends both; this page does not render them.
    system_usage: null,
    db_pool: { main: null, log: null, log_shared: true },
  }
}

const enabledLogs = {
  log_dir: '/var/log/ren2hub',
  enabled: true,
  file_count: 4,
  total_size: 2048,
  files: [],
}
// controller.GetLogFiles answer when no log directory is configured.
const disabledLogs = {
  log_dir: '',
  enabled: false,
  file_count: 0,
  total_size: 0,
  files: null,
}

function mockFeeds(
  cacheStats: Record<string, number>,
  logs: () => unknown = () => enabledLogs
) {
  return vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/performance/stats') return performanceStats(cacheStats)
    if (url === '/api/performance/logs') return logs()
    throw new Error(`unexpected ${url}`)
  })
}

async function render(
  kind: 'performance' | 'channel-affinity' = 'performance'
) {
  const wrapper = mount(SystemSettingsMaintenanceActions, {
    props: { kind },
    global: { plugins: [i18n], stubs: { ConfirmDialog: true } },
  })
  await flushPromises()
  return wrapper
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

describe('affinity cache statistics', () => {
  const stats = (total: number, capacity = 20) => ({
    enabled: true,
    total,
    unknown: 0,
    cache_capacity: capacity,
    cache_algo: 'lru',
  })
  async function refresh(wrapper: Awaited<ReturnType<typeof render>>) {
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '刷新')!
      .trigger('click')
    await flushPromises()
  }

  it('shows a failed first read as unavailable and accepts a real zero after retry', async () => {
    const get = vi.spyOn(api, 'get').mockRejectedValue(new Error('forbidden'))
    const wrapper = await render('channel-affinity')
    expect(wrapper.get('[role="status"]').text()).toBe(
      '无法读取亲和缓存统计，请重试。'
    )
    expect(wrapper.text()).not.toContain('缓存 0 条')
    expect(wrapper.text()).not.toContain('缓存条目')
    expect(toast.error).toHaveBeenCalledWith('forbidden')

    get.mockResolvedValue(stats(0, 0))
    await refresh(wrapper)
    expect(wrapper.text()).toContain('缓存 0 条，容量 0。')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('marks retained statistics stale after failure and clears the warning after recovery', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(stats(7))
    const wrapper = await render('channel-affinity')
    get.mockRejectedValue(new Error('offline'))
    await refresh(wrapper)
    expect(wrapper.text()).toContain('缓存 7 条，容量 20。')
    expect(wrapper.get('[role="status"]').text()).toBe(
      '刷新失败，当前显示上次读取的统计。'
    )

    get.mockResolvedValue(stats(3))
    await refresh(wrapper)
    expect(wrapper.text()).toContain('缓存 3 条，容量 20。')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('performance maintenance summary', () => {
  it('renders the native disk cache counters instead of a hit rate', async () => {
    mockFeeds({
      active_disk_files: 2,
      current_disk_usage_bytes: 5_242_880,
      active_memory_buffers: 3,
      current_memory_usage_bytes: 3072,
      disk_cache_hits: 7,
      memory_cache_hits: 9,
      disk_cache_max_bytes: 1_073_741_824,
      disk_cache_threshold_bytes: 10_485_760,
    })
    const wrapper = await render()

    expect(wrapper.text()).toContain(
      '活动内存缓冲 3 个（3.0 KB），活动磁盘文件 2 个（5.0 MB），日志文件 4 个。'
    )
    expect(wrapper.text()).toContain('4.0 KB')
    expect(wrapper.text()).not.toContain('NaN')
    expect(wrapper.text()).not.toContain('命中率')
    expect(toast.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('marks absent counters as unknown rather than NaN or zero', async () => {
    mockFeeds({})
    const wrapper = await render()

    expect(wrapper.text()).toContain(
      '活动内存缓冲 — 个（—），活动磁盘文件 — 个（—），日志文件 4 个。'
    )
    expect(wrapper.text()).not.toContain('NaN')
    wrapper.unmount()
  })
})

describe('log file cleanup', () => {
  async function confirmCleanup(wrapper: Awaited<ReturnType<typeof render>>) {
    await wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
  }
  async function clickButton(
    wrapper: Awaited<ReturnType<typeof render>>,
    label: string
  ) {
    await wrapper
      .findAll('button')
      .find((button) => button.text() === label)!
      .trigger('click')
    await flushPromises()
  }

  function hasCleanupButton(wrapper: Awaited<ReturnType<typeof render>>) {
    return wrapper
      .findAll('button')
      .some((button) => button.text() === '清理日志')
  }

  it('explains disabled file logging and offers no cleanup', async () => {
    mockFeeds({}, () => disabledLogs)
    const remove = vi.spyOn(api, 'delete')
    const wrapper = await render()

    expect(wrapper.get('[data-log-disabled]').text()).toBe(
      '服务器文件日志未启用（未配置日志目录）'
    )
    expect(hasCleanupButton(wrapper)).toBe(false)
    expect(remove).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cleans up enabled log files with the native mode and value', async () => {
    mockFeeds({})
    const remove = vi.spyOn(api, 'delete').mockResolvedValue(undefined)
    const wrapper = await render()

    expect(wrapper.find('[data-log-disabled]').exists()).toBe(false)
    await clickButton(wrapper, '清理日志')
    await confirmCleanup(wrapper)
    expect(remove).toHaveBeenCalledWith('/api/performance/logs', {
      mode: 'by_count',
      value: 30,
    })
    expect(toast.success).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('hides cleanup when the first log read fails', async () => {
    mockFeeds({}, () => Promise.reject(new Error('logs offline')))
    const wrapper = await render()

    expect(toast.error).toHaveBeenCalledWith('logs offline')
    expect(hasCleanupButton(wrapper)).toBe(false)
    expect(wrapper.find('[data-log-disabled]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('drops an earlier enabled state when a refresh fails', async () => {
    let logsOnline = true
    mockFeeds({}, () =>
      logsOnline ? enabledLogs : Promise.reject(new Error('logs offline'))
    )
    const remove = vi.spyOn(api, 'delete')
    const wrapper = await render()
    await clickButton(wrapper, '清理日志')

    logsOnline = false
    await clickButton(wrapper, '刷新')
    expect(toast.error).toHaveBeenCalledWith('logs offline')
    expect(hasCleanupButton(wrapper)).toBe(false)
    await confirmCleanup(wrapper)
    expect(remove).not.toHaveBeenCalled()
    expect(toast.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
