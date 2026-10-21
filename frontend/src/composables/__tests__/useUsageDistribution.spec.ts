import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { useUsageDistribution } from '@/composables/useUsageDistribution'
import { dashboardFixture } from '@/api/__tests__/dashboardFixture'
import i18n from '@/i18n'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
let wrapper: VueWrapper | undefined
function setupDistribution() {
  let state!: ReturnType<typeof useUsageDistribution>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useUsageDistribution()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  return state
}
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
  vi.useRealTimers()
})
describe('native usage distribution', () => {
  it('requests the displayed rolling year across January and maps native data', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 0, 2, 12))
    vi.mocked(api.get).mockResolvedValue({
      ...dashboardFixture(),
      comparison: null,
    })
    const state = setupDistribution()
    await state.load()
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/self/summary',
      expect.objectContaining({
        range: 'custom',
        start: '2025-01-04',
        end: '2026-01-02',
        include_comparison: 'false',
      }),
      { signal: expect.any(AbortSignal) }
    )
    expect(state.points.value[0]).toMatchObject({
      consume: '25',
      tokens: 10,
      requests: 2,
    })
  })
  it('clears the heatmap and exposes an error when measurements are unavailable', async () => {
    const input = dashboardFixture()
    input.meta.available = false
    vi.mocked(api.get).mockResolvedValue(input)
    const state = setupDistribution()
    await state.load()
    expect(state.points.value).toEqual([])
    expect(state.error.value).toBeTruthy()
  })
})
