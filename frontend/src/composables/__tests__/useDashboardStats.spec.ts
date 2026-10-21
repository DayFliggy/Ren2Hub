import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { useDashboardStats } from '@/composables/useDashboardStats'
import { dashboardFixture } from '@/api/__tests__/dashboardFixture'
import i18n from '@/i18n'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
let wrapper: VueWrapper | undefined
function setupStats() {
  let state!: ReturnType<typeof useDashboardStats>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useDashboardStats()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  return state
}
afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
  vi.resetAllMocks()
})
describe('native dashboard statistics lifecycle', () => {
  it('discards an old response after switching the selected period', async () => {
    const stats = setupStats()
    let resolve!: (value: ReturnType<typeof dashboardFixture>) => void
    vi.mocked(api.get)
      .mockReturnValueOnce(
        new Promise((done) => {
          resolve = done
        })
      )
      .mockResolvedValueOnce(dashboardFixture(20))
    const first = stats.load()
    stats.range.value = '7d'
    await stats.load()
    expect(stats.data.value?.kpi.totalTokens).toBe(20)
    resolve(dashboardFixture(10))
    await first
    expect(stats.data.value?.kpi.totalTokens).toBe(20)
  })
  it('sends custom dates with the browser offset sign to the native endpoint', async () => {
    vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(-330)
    vi.mocked(api.get).mockResolvedValue(dashboardFixture())
    const stats = setupStats()
    stats.range.value = 'custom'
    stats.customStart.value = '2026-10-01'
    stats.customEnd.value = '2026-10-21'
    await stats.load()
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/self/summary',
      {
        range: 'custom',
        start: '2026-10-01',
        end: '2026-10-21',
        tz_offset: '-330',
      },
      { signal: expect.any(AbortSignal) }
    )
    expect(stats.data.value?.kpi.successRate).toBe(50)
  })
  it('removes old results on a failed or unavailable period instead of showing zero use', async () => {
    const stats = setupStats()
    vi.mocked(api.get)
      .mockResolvedValueOnce(dashboardFixture())
      .mockRejectedValueOnce(new Error('failed'))
    await stats.load()
    await stats.load()
    expect(stats.data.value).toBeNull()
    expect(stats.error.value).toBeTruthy()
    const unavailable = dashboardFixture()
    unavailable.meta.available = false
    vi.mocked(api.get).mockResolvedValueOnce(unavailable)
    await stats.load()
    expect(stats.data.value).toBeNull()
    expect(stats.error.value).toBeTruthy()
  })
})
