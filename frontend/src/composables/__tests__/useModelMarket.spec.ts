import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { api } from '@/api/client'
import { useModelMarket } from '@/composables/useModelMarket'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
import i18n from '@/i18n'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
let wrapper: VueWrapper | undefined
function setupMarket() {
  let state!: ReturnType<typeof useModelMarket>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useModelMarket()
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  return state
}
function performanceSummary(successRate: number) {
  return {
    summary: null,
    models: [
      {
        model_name: 'fixture-model',
        avg_latency_ms: 800,
        success_rate: successRate,
        avg_tps: 0,
      },
    ],
  }
}
beforeEach(() => {
  localStorage.clear()
  vi.mocked(api.get).mockImplementation(async (url) =>
    url === '/api/perf-metrics/summary'
      ? performanceSummary(99)
      : modelCatalogFixture()
  )
})
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
})
describe('native catalog browser', () => {
  it('retains the supply filters through each visual mode without refetching', async () => {
    const state = setupMarket()
    await state.load()
    state.channel.value = 9
    state.keyword.value = 'Fixture channel'
    state.billing.value = 'token'
    for (const view of ['models', 'channels', 'comparison'] as const) {
      state.view.value = view
      expect(state.filtered.value.map((model) => model.id)).toEqual([
        'fixture-model',
      ])
      expect(state.visibleChannels.value.map((channel) => channel.id)).toEqual([
        9,
      ])
    }
    expect(
      vi
        .mocked(api.get)
        .mock.calls.map(([url]) => url)
        .sort()
    ).toEqual(['/api/perf-metrics/summary', '/api/user/model-catalog'])
    expect(api.get).toHaveBeenCalledWith('/api/user/model-catalog', undefined, {
      signal: expect.any(AbortSignal),
    })
  })
  it('never lets a stale performance response overwrite a newer one', async () => {
    let resolve!: (value: unknown) => void
    vi.mocked(api.get)
      .mockReturnValueOnce(
        new Promise((done) => {
          resolve = done
        })
      )
      .mockResolvedValueOnce(performanceSummary(99))
    const state = setupMarket()
    const first = state.loadPerformance()
    await state.loadPerformance()
    expect(vi.mocked(api.get).mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    resolve(performanceSummary(10))
    await first
    expect(state.performanceFor('fixture-model')).toEqual({
      status: 'ready',
      summary: expect.objectContaining({ success_rate: 99 }),
    })
  })
  it('discards permission-scoped prices after a failed refresh', async () => {
    const state = setupMarket()
    await state.load()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('denied'))
    await state.load()
    expect(state.catalog.value).toBeNull()
    expect(state.filtered.value).toEqual([])
    expect(state.error.value).toBeTruthy()
  })
  it('never lets a stale response overwrite a newer catalog', async () => {
    let resolve!: (value: ReturnType<typeof modelCatalogFixture>) => void
    vi.mocked(api.get)
      .mockReturnValueOnce(
        new Promise((done) => {
          resolve = done
        })
      )
      .mockResolvedValueOnce(modelCatalogFixture())
    const state = setupMarket()
    const first = state.load()
    await state.load()
    const older = modelCatalogFixture()
    older.models[0]!.model_name = 'old'
    resolve(older)
    await first
    expect(state.catalog.value!.models[0]!.id).toBe('fixture-model')
  })
  it('normalizes a retired saved view without persisting price data', async () => {
    localStorage.setItem('ren2hub_models_view', 'grid')
    const state = setupMarket()
    await state.load()
    expect(state.view.value).toBe('models')
    expect(Object.values(localStorage).join(' ')).not.toContain(
      'Fixture channel'
    )
  })
})
