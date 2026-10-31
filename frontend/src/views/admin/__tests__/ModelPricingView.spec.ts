import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref, type Ref } from 'vue'
import {
  PRICING_KEYS,
  type ModelPricingConfig,
  type PricingValues,
} from '@/api/pricingConfig'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
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
  width: null as unknown as Ref<number>,
}))
// The page width picks the inline editor or the drawer; tests set it here.
vi.mock('@vueuse/core', async (original) => {
  const { ref } = await import('vue')
  mocks.width = ref(0)
  return {
    ...(await original<typeof import('@vueuse/core')>()),
    useElementSize: () => ({ width: mocks.width, height: ref(0) }),
  }
})
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

function reloadDialog(wrapper: VueWrapper) {
  const found = wrapper
    .findAllComponents(ConfirmDialog)
    .find((dialog) => dialog.props('title') === 'Reload pricing')
  if (!found) throw new Error('Missing reload dialog')
  return found
}
async function setup(
  embedded = false,
  stubEditor = true,
  path = '/models/pricing',
  locale = 'en'
) {
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
  await router.push(path)
  const wrapper = mount(
    { template: '<RouterView />' },
    {
      global: {
        plugins: [
          router,
          createI18n({ legacy: false, locale, messages: { [locale]: {} } }),
        ],
        stubs: {
          ModelPricingEditor: stubEditor,
          PriceFetchDialog: true,
          MetadataSyncDialog: true,
          MissingModelsDialog: true,
          PrefillGroupDialog: true,
          teleport: true,
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
  mocks.width.value = 0
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
    mocks.rawValue.mockImplementation((key: string, fallback: unknown) =>
      key === 'tool_price_setting.prices' ? '{"web_search":1}' : fallback
    )
    const wrapper = await setup()
    const input = wrapper.get('.tool-pricing input[inputmode="decimal"]')
    await input.setValue('2')
    await button(wrapper, 'Reload pricing').trigger('click')
    expect(reloadDialog(wrapper).props('open')).toBe(true)
    expect(mocks.load).toHaveBeenCalledTimes(1)
    reloadDialog(wrapper).vm.$emit('confirm')
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledTimes(2)
    expect(
      wrapper.get('.tool-pricing input[inputmode="decimal"]').element
    ).toHaveProperty('value', '1')
    wrapper.unmount()
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
      { model_name: 'model-a', expected_version: 'v1', pricing: {} },
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
      { model_name: 'model-a', expected_version: 'v1', pricing: {} },
    ])

    await wrapper
      .findAll('[role="radio"]')
      .find((item) => item.text() === 'Tool prices')!
      .trigger('click')
    expect(saves()).toHaveLength(0)
    expect(button(wrapper, 'Reload pricing').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not report a save when the pricing draft has no changes', async () => {
    const wrapper = await setup()
    const loadCalls = mocks.load.mock.calls.length
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(mocks.load).toHaveBeenCalledTimes(loadCalls)
    expect(wrapper.text()).not.toContain('Model prices saved')
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
    expect(reloadDialog(wrapper).props('open')).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(
      button(wrapper, 'Save model prices').element.matches(':disabled')
    ).toBe(false)
    wrapper.unmount()
  })

  it('names a version conflict only when the server reports one', async () => {
    const wrapper = await setup()
    await button(wrapper, 'Delete').trigger('click')
    mocks.save.mockRejectedValueOnce(
      new ApiError('model pricing changed; reload before saving', {
        status: 409,
      })
    )
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'Prices changed on the server'
    )
    mocks.save.mockRejectedValueOnce(
      new ApiError('invalid billing expression', { status: 400 })
    )
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    const alert = wrapper.get('[role="alert"]').text()
    expect(alert).toContain('Saving failed; your draft is preserved.')
    expect(alert).toContain('invalid billing expression')
    expect(alert).not.toContain('Prices changed on the server')
    // A rejected save keeps the draft for another attempt.
    expect(wrapper.text()).toContain('Unsaved prices')
    wrapper.unmount()
  })

  it('opens the ?model= editor after the first load only', async () => {
    mocks.load.mockResolvedValue(config(1, ['model-a', 'model-b']))
    const wrapper = await setup(false, true, '/models/pricing?model=model-a')
    const editor = () => wrapper.get('model-pricing-editor-stub')
    expect(editor().attributes('modelname')).toBe('model-a')
    await button(wrapper, 'model-b').trigger('click')
    await button(wrapper, 'Reload pricing').trigger('click')
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledTimes(2)
    expect(editor().attributes('modelname')).toBe('model-b')
    wrapper.unmount()
  })

  it('drops a post-save refresh that a newer reload replaced', async () => {
    const wrapper = await setup()
    await button(wrapper, 'Delete').trigger('click')
    let resolveRefresh!: (value: ModelPricingConfig) => void
    mocks.save.mockResolvedValueOnce(undefined)
    mocks.load.mockImplementationOnce(
      () =>
        new Promise<ModelPricingConfig>((resolve) => {
          resolveRefresh = resolve
        })
    )
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    const refreshSignal = mocks.load.mock.calls.at(-1)?.[0] as AbortSignal
    expect(refreshSignal).toBeInstanceOf(AbortSignal)
    wrapper.unmount()
    expect(refreshSignal.aborted).toBe(true)
    resolveRefresh(config(9))
    await flushPromises()
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

  it('edits in a drawer below the two-column width and keeps the draft across the breakpoint', async () => {
    mocks.load.mockResolvedValue(
      config(1, ['model-a'], tiered('tier("base", p * 2 + c * 4)'))
    )
    mocks.width.value = 1200
    const wrapper = await setup(false, false)
    const drawers = () =>
      wrapper
        .findAllComponents(ConsoleModal)
        .filter((modal) => modal.props('presentation') === 'drawer')
    await button(wrapper, 'model-a').trigger('click')
    expect(wrapper.find('.model-pricing-page.is-compact').exists()).toBe(false)
    expect(drawers()).toHaveLength(0)
    expect(
      wrapper.find('.pricing-editor-container .pricing-editor').exists()
    ).toBe(true)
    await wrapper.get('.price-tier-line input').setValue('3')

    // 800px is a tablet beside the console sidebar: one column, drawer editor.
    mocks.width.value = 800
    await flushPromises()
    expect(wrapper.find('.model-pricing-page.is-compact').exists()).toBe(true)
    expect(wrapper.find('.pricing-editor-container').exists()).toBe(false)
    expect(drawers()).toHaveLength(1)
    const drawer = drawers()[0]!
    expect(drawer.props('title')).toBe('')
    expect(drawer.find('h2').text()).toBe('Edit model pricing')
    expect(wrapper.findAll('.pricing-editor')).toHaveLength(1)
    expect(
      (wrapper.get('.price-tier-line input').element as HTMLInputElement).value
    ).toBe('3')
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({
        pricing: tiered('tier("base", p * 3 + c * 4)'),
      }),
    ])
    wrapper.unmount()
  })

  it('keeps an invalid edit in place when the page crosses the breakpoint', async () => {
    mocks.load.mockResolvedValue(
      config(1, ['model-a'], tiered('tier("base", p * 2 + c * 4)'))
    )
    mocks.width.value = 1200
    const wrapper = await setup(false, false)
    await button(wrapper, 'model-a').trigger('click')
    await wrapper.get('.price-tier-line input').setValue('')

    mocks.width.value = 800
    await flushPromises()
    // The unparsed input would be lost if the editor remounted in the drawer.
    expect(
      wrapper.find('.pricing-editor-container .pricing-editor').exists()
    ).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).not.toBe('')
    expect(
      (wrapper.get('.price-tier-line input').element as HTMLInputElement).value
    ).toBe('')

    // Fixing the edit does not move the editor under the user's cursor; the
    // next resize, save, model switch or close does.
    await wrapper.get('.price-tier-line input').setValue('5')
    await flushPromises()
    expect(wrapper.find('.pricing-editor-container').exists()).toBe(true)
    mocks.width.value = 790
    await flushPromises()
    expect(wrapper.find('.pricing-editor-container').exists()).toBe(false)
    expect(
      (wrapper.get('.price-tier-line input').element as HTMLInputElement).value
    ).toBe('5')
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({
        pricing: tiered('tier("base", p * 5 + c * 4)'),
      }),
    ])
    wrapper.unmount()
  })

  it('keeps an invalid model draft open instead of switching model or tab', async () => {
    mocks.load.mockResolvedValue(
      config(1, ['model-a', 'model-b'], tiered('tier("base", p * 2)'))
    )
    const wrapper = await setup(false, false)
    await button(wrapper, 'model-a').trigger('click')
    await wrapper.get('.price-tier-line input').setValue('')
    await button(wrapper, 'model-b').trigger('click')
    expect(wrapper.get('.pricing-editor-header').text()).toContain('model-a')
    await wrapper
      .findAll('[role="radio"]')
      .find((item) => item.text() === 'Tool prices')!
      .trigger('click')
    expect(wrapper.get('.pricing-panel').attributes('aria-label')).toBe(
      'Models'
    )
    wrapper.unmount()
  })

  it('stays in JSON mode when the pricing JSON does not parse', async () => {
    const wrapper = await setup()
    await button(wrapper, 'JSON').trigger('click')
    await wrapper.get('.pricing-panel textarea').setValue('{')
    await button(wrapper, 'Visual').trigger('click')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('.pricing-panel textarea').exists()).toBe(true)
    expect(button(wrapper, 'Visual').exists()).toBe(true)
    wrapper.unmount()
  })

  it('round-trips tool prices through their JSON editor', async () => {
    mocks.rawValue.mockImplementation((key: string, fallback: unknown) =>
      key === 'tool_price_setting.prices' ? '{"web_search":1}' : fallback
    )
    const wrapper = await setup()
    const tools = () => wrapper.get('.tool-pricing')
    await tools()
      .findAll('button')
      .find((item) => item.text() === 'JSON')!
      .trigger('click')
    await tools().get('textarea').setValue('{"web_search": 2}')
    await button(wrapper, 'Save tool prices').trigger('click')
    await flushPromises()
    expect(mocks.saveOptions).toHaveBeenCalledWith({
      'tool_price_setting.prices': '{\n  "web_search": 2\n}',
    })
    expect(wrapper.text()).toContain('Tool prices saved')
    wrapper.unmount()
  })

  it('copies the open model pricing to the selected models', async () => {
    const loaded = config(1, ['model-a', 'model-b'])
    loaded.entries[0]!.configured = { ModelPrice: 5 }
    loaded.entries[0]!.effective = { ModelPrice: 5 }
    mocks.load.mockResolvedValue(loaded)
    const wrapper = await setup()
    await button(wrapper, 'model-a').trigger('click')
    await wrapper.get('input[aria-label="Select model-b"]').setValue(true)
    await button(wrapper, 'Copy current model pricing').trigger('click')
    await button(wrapper, 'Save model prices').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith([
      expect.objectContaining({
        model_name: 'model-b',
        pricing: { ModelPrice: 5 },
      }),
    ])
    wrapper.unmount()
  })

  it('lists enabled models without prices and filters by billing mode', async () => {
    const loaded = config(1, ['model-a', 'model-b'])
    loaded.entries[1]!.configured = tiered('p * 2')
    loaded.entries[1]!.effective = tiered('p * 2')
    mocks.load.mockResolvedValue(loaded)
    mocks.get.mockResolvedValue(['model-a', 'model-b', 'model-c'])
    const wrapper = await setup()
    const names = () =>
      wrapper.findAll('.pricing-model-name').map((item) => item.text())
    expect(names()).toEqual(['model-a', 'model-b'])
    wrapper
      .getComponent(FilterSelect)
      .vm.$emit('update:modelValue', 'tiered_expr')
    await flushPromises()
    expect(names()).toEqual(['model-b'])
    await wrapper
      .findAll('[role="radio"]')
      .find((item) => item.text() === 'Unpriced models')!
      .trigger('click')
    wrapper.getComponent(FilterSelect).vm.$emit('update:modelValue', '')
    await flushPromises()
    expect(names()).toEqual(['model-c'])
    wrapper.unmount()
  })

  // English renders both keys as "All"; Chinese tells the filter's "全部"
  // apart from the condition tree's "全部满足".
  it('labels the unfiltered billing mode as every mode, not a condition group', async () => {
    const wrapper = await setup(false, true, '/models/pricing', 'zh-CN')
    const trigger = wrapper.get('[role="combobox"][aria-label="模式"]')
    expect(trigger.findAll('span').map((item) => item.text())).toContain('全部')
    expect(trigger.text()).not.toContain('全部满足')
    wrapper.unmount()
  })

  it('puts the expose toggle back when saving it fails', async () => {
    mocks.saveOptions.mockResolvedValue(false)
    const wrapper = await setup()
    const toggle = wrapper
      .findAll('label')
      .find((item) => item.text() === 'Expose ratio API')!
      .get('input')
    await toggle.setValue(true)
    await flushPromises()
    expect(mocks.saveOptions).toHaveBeenCalledWith({ ExposeRatioEnabled: true })
    expect((toggle.element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })
})
