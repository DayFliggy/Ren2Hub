import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
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
  }
}

function mockFeeds(cacheStats: Record<string, number>) {
  return vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/performance/stats') return performanceStats(cacheStats)
    if (url === '/api/performance/logs')
      return { enabled: true, file_count: 4, total_size: 2048, files: [] }
    throw new Error(`unexpected ${url}`)
  })
}

async function render() {
  const wrapper = mount(SystemSettingsMaintenanceActions, {
    props: { kind: 'performance' },
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
