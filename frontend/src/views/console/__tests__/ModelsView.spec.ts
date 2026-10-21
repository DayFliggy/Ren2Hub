import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/client'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import ModelsView from '../ModelsView.vue'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
let wrapper: VueWrapper | undefined
function render() {
  wrapper = mount(ModelsView, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
beforeEach(() => vi.mocked(api.get).mockResolvedValue(modelCatalogFixture()))
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
  localStorage.clear()
})
describe('native model catalog page', () => {
  it('loads one catalog and preserves search through all historical visual modes', async () => {
    const view = render()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/api/user/model-catalog', undefined, {
      signal: expect.any(AbortSignal),
    })
    expect(view.findAll('[data-model-card]')).toHaveLength(1)
    await view.get('input[name="model-search"]').setValue('fixture-model')
    for (const label of ['渠道货架', '比价矩阵', '模型聚合']) {
      await view.get(`[role="radio"][aria-label="${label}"]`).trigger('click')
      expect(
        (view.get('input[name="model-search"]').element as HTMLInputElement)
          .value
      ).toBe('fixture-model')
      expect(view.text()).toContain('1 个模型')
    }
    expect(api.get).toHaveBeenCalledTimes(1)
  })
  it('opens native channel and baseline prices with their supplied endpoint paths', async () => {
    const view = render()
    await flushPromises()
    await view
      .get('[data-model-name="fixture-model"] button[aria-label*="模型详情"]')
      .trigger('click')
    await flushPromises()
    const popup = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    expect(popup.textContent).toContain('/v1/responses')
    expect(popup.textContent).toContain('$0.250')
    expect(popup.textContent).toContain('$1.250')
    expect(
      popup.querySelector('[data-market-baseline] [data-market-telemetry]')
    ).toBeNull()
    expect(popup.querySelector('.market-detail-variant')).toBeNull()
    expect(popup.textContent).toContain('未提供')
  })
  it('does not render permission-scoped prices after a catalog failure', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('denied'))
    const view = render()
    await flushPromises()
    expect(view.find('[data-model-card]').exists()).toBe(false)
    expect(view.find('[data-offer-id]').exists()).toBe(false)
  })
})
