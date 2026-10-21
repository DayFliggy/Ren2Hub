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
beforeEach(() => {
  localStorage.clear()
  vi.mocked(api.get).mockResolvedValue(modelCatalogFixture())
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
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledWith('/api/user/model-catalog', undefined, {
      signal: expect.any(AbortSignal),
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
