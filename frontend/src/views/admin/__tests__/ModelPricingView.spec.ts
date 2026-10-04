import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import {
  PRICING_KEYS,
  type ModelPricingConfig,
  type PricingValues,
} from '@/api/pricingConfig'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelManagementView from '../ModelManagementView.vue'
import ModelPricingView from '../ModelPricingView.vue'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  preview: vi.fn(),
  loadSettings: vi.fn(),
  saveOptions: vi.fn(),
  rawValue: vi.fn(),
  get: vi.fn(),
}))
vi.mock('@/api/client', () => ({ api: { get: mocks.get } }))
vi.mock('@/api/pricingConfig', async (original) => ({
  ...(await original<typeof import('@/api/pricingConfig')>()),
  pricingConfigApi: {
    load: mocks.load,
    save: mocks.save,
    preview: mocks.preview,
  },
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

function config(
  price = 1,
  names: string[] = ['model-a'],
  pricing: PricingValues = { ModelPrice: price }
): ModelPricingConfig {
  return {
    entries: names.map((model_name) => ({
      model_name,
      version: 'v1',
      configured: pricing,
      effective: pricing,
    })),
    empty_version: 'empty',
    options: Object.fromEntries(
      PRICING_KEYS.map((key) => [key, '{}'])
    ) as ModelPricingConfig['options'],
  }
}
const tiered = (expression: string): PricingValues => ({
  'billing_setting.billing_mode': 'tiered_expr',
  'billing_setting.billing_expr': expression,
})

async function setup(embedded = false, stubEditor = true) {
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
          ModelPricingEditor: stubEditor,
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
function field(wrapper: VueWrapper, scope: string, name: string) {
  const found = wrapper.findAll(`${scope} label`).find((item) => {
    const caption = item.find('span')
    return caption.exists() && caption.text() === name
  })
  if (!found) throw new Error(`Missing field: ${name}`)
  return found.get('input')
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.load.mockResolvedValue(config())
  mocks.preview.mockResolvedValue({ effective: {} })
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

  it('saves visual tier and request rule edits from the header without a separate apply', async () => {
    mocks.load.mockResolvedValue(
      config(1, ['model-a'], tiered('tier("base", p * 2 + c * 4)'))
    )
    const wrapper = await setup(false, false)
    await button(wrapper, 'model-a').trigger('click')
    await button(wrapper, 'Visual tiers').trigger('click')
    await wrapper.get('.price-tier-line input').setValue('3')
    await button(wrapper, 'Add multiplier rule').trigger('click')
    await field(wrapper, '.price-rule', 'Field path').setValue('service_tier')
    await field(wrapper, '.price-rule', 'Value').setValue('priority')
    await field(wrapper, '.price-rule', 'Multiplier').setValue('2')
    expect(wrapper.text()).toContain('Unsaved prices')
    // The page header save is the first of the two save buttons.
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({
        model_name: 'model-a',
        pricing: tiered(
          '(tier("base", p * 3 + c * 4)) * (param("service_tier") == "priority" ? 2 : 1)'
        ),
      }),
    ])
    expect(wrapper.text()).toContain('Model prices saved')
    wrapper.unmount()
  })

  it('guards an invalid visual tier edit and refuses to report it saved', async () => {
    mocks.load.mockResolvedValue(
      config(1, ['model-a'], tiered('tier("base", p * 2 + c * 4)'))
    )
    const wrapper = await setup(false, false)
    await button(wrapper, 'model-a').trigger('click')
    await button(wrapper, 'Visual tiers').trigger('click')
    await wrapper.get('.price-tier-line input').setValue('')
    expect(wrapper.text()).toContain('Unsaved prices')
    const leave = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(leave)
    expect(leave.defaultPrevented).toBe(true)
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(wrapper.get('.pricing-editor [role="alert"]').text()).toContain(
      'Enter a finite, non-negative price.'
    )
    expect(wrapper.text()).not.toContain('Model prices saved')
    wrapper.unmount()
  })

  it('reports a save whose refresh failed and requires a reload before saving again', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = await setup()
    await button(wrapper, 'Delete').trigger('click')
    mocks.load.mockRejectedValueOnce(new Error('network down'))
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    const alert = wrapper.get('[role="alert"]').text()
    expect(alert).toContain('Prices saved, but refreshing failed')
    expect(alert).toContain('network down')
    expect(wrapper.text()).not.toContain('Unsaved prices')
    expect(
      button(wrapper, 'Save model prices').element.matches(':disabled')
    ).toBe(true)
    await button(wrapper, 'Reload pricing').trigger('click')
    await flushPromises()
    expect(confirm).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(
      button(wrapper, 'Save model prices').element.matches(':disabled')
    ).toBe(false)
    wrapper.unmount()
    confirm.mockRestore()
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
