import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { PRICING_KEYS, type ModelPricingConfig } from '@/api/pricingConfig'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelManagementView from '../ModelManagementView.vue'
import ModelPricingView from '../ModelPricingView.vue'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  loadSettings: vi.fn(),
  saveOptions: vi.fn(),
  rawValue: vi.fn(),
  get: vi.fn(),
}))
vi.mock('@/api/client', () => ({ api: { get: mocks.get } }))
vi.mock('@/api/pricingConfig', async (original) => ({
  ...(await original<typeof import('@/api/pricingConfig')>()),
  pricingConfigApi: { load: mocks.load, save: mocks.save },
}))
vi.mock('@/composables/useSystemSettings', () => ({
  useSystemSettings: () => ({
    load: mocks.loadSettings,
    saveOptions: mocks.saveOptions,
    rawValue: mocks.rawValue,
    saving: ref(false),
  }),
}))
// Only the embedded host (ModelManagementView) reads these.
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { allVendors: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isRoot: true }) }))

function config(price = 1, names: string[] = ['model-a']): ModelPricingConfig {
  return {
    entries: names.map((model_name) => ({
      model_name,
      version: 'v1',
      configured: { ModelPrice: price },
      effective: { ModelPrice: price },
    })),
    empty_version: 'empty',
    options: Object.fromEntries(
      PRICING_KEYS.map((key) => [key, '{}'])
    ) as ModelPricingConfig['options'],
  }
}

async function setup(embedded = false) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      embedded
        ? {
            // Production route: the pricing section of model management.
            path: '/models/pricing',
            component: ModelManagementView,
            props: { section: 'pricing' },
          }
        : { path: '/models/pricing', component: ModelPricingView },
    ],
  })
  await router.push('/models/pricing')
  const wrapper = mount(
    { template: '<RouterView />' },
    {
      global: {
        plugins: [
          router,
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
        stubs: {
          ModelPricingEditor: true,
          PriceFetchDialog: true,
          MetadataSyncDialog: true,
          MissingModelsDialog: true,
          PrefillGroupDialog: true,
        },
      },
    }
  )
  await vi.dynamicImportSettled()
  await flushPromises()
  return wrapper
}
function button(wrapper: VueWrapper, text: string) {
  const found = wrapper.findAll('button').find((item) => item.text() === text)
  if (!found) throw new Error(`Missing button: ${text}`)
  return found
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.load.mockResolvedValue(config())
  mocks.get.mockResolvedValue(['model-a'])
  mocks.loadSettings.mockResolvedValue(true)
  mocks.rawValue.mockImplementation(
    (_key: string, fallback: unknown) => fallback
  )
  mocks.saveOptions.mockResolvedValue(true)
})

describe('pricing workspace draft lifecycle', () => {
  it('fails closed after settings fail and retries the complete pricing workspace', async () => {
    mocks.loadSettings.mockResolvedValueOnce(false)
    const wrapper = await setup()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'Failed to load prices'
    )
    expect(
      button(wrapper, 'Save model prices').element.matches(':disabled')
    ).toBe(true)
    expect(
      button(wrapper, 'Save tool prices').element.matches(':disabled')
    ).toBe(true)
    await button(wrapper, 'Reload pricing').trigger('click')
    await flushPromises()
    expect(mocks.loadSettings).toHaveBeenLastCalledWith(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(
      button(wrapper, 'Save tool prices').element.matches(':disabled')
    ).toBe(false)
    wrapper.unmount()
  })

  it('discards an edited tool draft on confirmed reload even when the saved JSON is unchanged', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    mocks.rawValue.mockImplementation((key: string, fallback: unknown) =>
      key === 'tool_price_setting.prices' ? '{"web_search":1}' : fallback
    )
    const wrapper = await setup()
    const input = wrapper.get('.tool-pricing input[inputmode="decimal"]')
    await input.setValue('2')
    await button(wrapper, 'Reload pricing').trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(
      wrapper.get('.tool-pricing input[inputmode="decimal"]').element
    ).toHaveProperty('value', '1')
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('keeps tool-price controls unavailable until their saved settings have loaded', async () => {
    let resolveSettings!: (value: boolean) => void
    mocks.loadSettings.mockReturnValue(
      new Promise<boolean>((resolve) => {
        resolveSettings = resolve
      })
    )
    const wrapper = await setup()
    expect(
      button(wrapper, 'Save tool prices').element.matches(':disabled')
    ).toBe(true)
    expect(
      button(wrapper, 'Add price dimension').element.matches(':disabled')
    ).toBe(true)
    resolveSettings(true)
    await flushPromises()
    expect(
      button(wrapper, 'Save tool prices').element.matches(':disabled')
    ).toBe(false)
    wrapper.unmount()
  })

  it('prevents changing or reloading a model draft while its save is pending', async () => {
    let resolveSave!: () => void
    mocks.save.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveSave = resolve
      })
    )
    const wrapper = await setup()
    await button(wrapper, 'Delete').trigger('click')
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({ model_name: 'model-a', reset: true }),
    ])
    expect(button(wrapper, 'Reload pricing').element.matches(':disabled')).toBe(
      true
    )
    expect(button(wrapper, 'Add model').element.matches(':disabled')).toBe(true)
    expect(button(wrapper, 'JSON').element.matches(':disabled')).toBe(true)
    resolveSave()
    await flushPromises()
    expect(button(wrapper, 'Reload pricing').element.matches(':disabled')).toBe(
      false
    )
    wrapper.unmount()
  })

  it('drives the model management header bar from the embedded pricing draft', async () => {
    const wrapper = await setup(true)
    const page = wrapper.get('.model-pricing-page')
    const saves = () =>
      wrapper
        .findAll('button')
        .filter((item) => item.text() === 'Save model prices')
    // The host renders the only bar, outside the embedded pricing section.
    expect(saves()).toHaveLength(1)
    expect(page.text()).not.toContain('Reload pricing')
    expect(wrapper.text()).not.toContain('Unsaved prices')

    await button(wrapper, 'Delete').trigger('click')
    expect(wrapper.text()).toContain('Unsaved prices')
    await saves()[0]!.trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({ model_name: 'model-a', reset: true }),
    ])

    await wrapper
      .findAll('[role="radio"]')
      .find((item) => item.text() === 'Tool prices')!
      .trigger('click')
    expect(saves()).toHaveLength(0)
    expect(button(wrapper, 'Reload pricing').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps earlier pages selected when the header selects another page', async () => {
    const names = Array.from(
      { length: 21 },
      (_, index) => `model-${String(index).padStart(2, '0')}`
    )
    mocks.load.mockResolvedValue(config(1, names))
    mocks.get.mockResolvedValue(names)
    const wrapper = await setup()
    const header = () => wrapper.get('thead input[type="checkbox"]')

    await header().setValue(true)
    expect(wrapper.text()).toContain('20 models selected')
    wrapper.getComponent(TablePagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(
      wrapper.get('tbody input[type="checkbox"]').attributes('aria-label')
    ).toBe('Select model-20')
    await header().setValue(true)
    expect(wrapper.text()).toContain('21 models selected')
    await header().setValue(false)
    expect(wrapper.text()).toContain('20 models selected')
    wrapper.unmount()
  })
})
