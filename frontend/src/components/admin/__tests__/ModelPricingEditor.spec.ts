import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelPricingEditor from '../ModelPricingEditor.vue'
import { configureQuotaPerUnit } from '@/utils/quota'
import { pricingConfigApi, type PricingValues } from '@/api/pricingConfig'
const plugins = [
  createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
]
beforeEach(() => {
  vi.spyOn(pricingConfigApi, 'preview').mockResolvedValue({ effective: {} })
})
afterEach(() => {
  configureQuotaPerUnit(500_000)
  vi.restoreAllMocks()
})
function setup(pricing: PricingValues = {}) {
  return mount(ModelPricingEditor, {
    props: { modelName: 'test-model', pricing },
    global: {
      plugins,
      stubs: { ConsoleModal: { template: '<div><slot /></div>' } },
    },
  })
}
type EditorWrapper = ReturnType<typeof setup>
function button(wrapper: EditorWrapper, text: string) {
  const found = wrapper.findAll('button').find((item) => item.text() === text)
  if (!found) throw new Error(`Missing button: ${text}`)
  return found
}
function field(wrapper: EditorWrapper, scope: string, name: string) {
  const found = wrapper.findAll(`${scope} label`).find((item) => {
    const caption = item.find('span')
    return caption.exists() && caption.text() === name
  })
  if (!found) throw new Error(`Missing field: ${name}`)
  return found.get('input')
}
const tiered = (expression: string): PricingValues => ({
  'billing_setting.billing_mode': 'tiered_expr',
  'billing_setting.billing_expr': expression,
})
describe('expression editor drafts', () => {
  it('applies the base tier preset once its blank prices are entered', async () => {
    const wrapper = setup()
    await button(wrapper, 'Add a base tier').trigger('click')
    const prices = wrapper.findAll('.price-tier-line input')
    await prices[0]!.setValue('2')
    await prices[1]!.setValue('4')
    expect(wrapper.vm.dirty()).toBe(true)
    expect(wrapper.vm.commit()?.pricing).toEqual(
      tiered('tier("base", p * 2 + c * 4)')
    )
    wrapper.unmount()
  })
  it('drops a stale visual error once valid source is entered', async () => {
    const wrapper = setup()
    await button(wrapper, 'Add a base tier').trigger('click')
    // Blank preset prices are not free; saving must stop with an error.
    expect(wrapper.vm.commit()).toBeNull()
    await button(wrapper, 'Expression source').trigger('click')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.get('.price-expression textarea').setValue('p * 2')
    expect(wrapper.vm.commit()?.pricing).toEqual(tiered('p * 2'))
    wrapper.unmount()
  })
  it('opens another model with a fresh expression editor', async () => {
    const wrapper = setup(tiered('tier("base", p * 2 + c * 4)'))
    await button(wrapper, 'Visual tiers').trigger('click')
    await wrapper.get('.price-tier-line input').setValue('')
    expect(wrapper.vm.dirty()).toBe(true)
    await wrapper.setProps({
      modelName: 'other-model',
      pricing: tiered('p * 5'),
    })
    expect(wrapper.get('.price-expression textarea').element).toHaveProperty(
      'value',
      'p * 5'
    )
    expect(wrapper.vm.dirty()).toBe(false)
    expect(wrapper.vm.commit()?.pricing).toEqual(tiered('p * 5'))
    wrapper.unmount()
  })
  it('keeps a changed price dimension in the visual tier', async () => {
    const wrapper = setup(tiered('v1:tier("base", p * 2 + c * 4)'))
    await button(wrapper, 'Visual tiers').trigger('click')
    // Opening the visual editor alone must not rewrite the saved expression.
    expect(wrapper.vm.dirty()).toBe(false)
    await wrapper.findAll('.price-tier-line select')[1]!.setValue('cr')
    expect(wrapper.vm.commit()?.pricing).toEqual(
      tiered('v1:tier("base", p * 2 + cr * 4)')
    )
    wrapper.unmount()
  })
  it('offers the OpenAI 12h cache write as a price dimension', async () => {
    const wrapper = setup(tiered('v1:tier("base", p * 2 + c * 4)'))
    await button(wrapper, 'Visual tiers').trigger('click')
    await wrapper.findAll('.price-tier-line select')[1]!.setValue('cc12h')
    expect(wrapper.vm.commit()?.pricing).toEqual(
      tiered('v1:tier("base", p * 2 + cc12h * 4)')
    )
    wrapper.unmount()
  })
  it('stops the editor save on an incomplete request rule', async () => {
    const wrapper = setup(tiered('p * 2'))
    await button(wrapper, 'Add multiplier rule').trigger('click')
    await field(wrapper, '.price-rule', 'Field path').setValue('service_tier')
    expect(wrapper.vm.dirty()).toBe(true)
    await button(wrapper, 'Save model prices').trigger('click')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(
      wrapper.get('.pricing-editor-scroll > [role="alert"]').text()
    ).toContain('Complete condition paths, values, and valid multipliers.')
    wrapper.unmount()
  })
})
describe('manual model price editor', () => {
  it('saves dollar prices at the runtime quota unit, preserving untouched ratios', async () => {
    configureQuotaPerUnit(1_000_000)
    const wrapper = setup({ ModelRatio: '2', CompletionRatio: '3' })
    const input = wrapper.get('input[inputmode="decimal"]')
    expect(input.element).toHaveProperty('value', '2')
    expect(wrapper.vm.commit()?.pricing).toEqual({
      ModelRatio: '2',
      CompletionRatio: '3',
    })
    await input.setValue('5')
    expect(wrapper.vm.commit()?.pricing).toMatchObject({ ModelRatio: '5' })
    wrapper.unmount()
  })
  it('reprojects clean prices on status refresh and preserves edited dollar intent', async () => {
    const wrapper = setup({ ModelRatio: '2' })
    const input = wrapper.get('input[inputmode="decimal"]')
    expect(input.element).toHaveProperty('value', '4')
    configureQuotaPerUnit(1_000_000)
    await flushPromises()
    expect(input.element).toHaveProperty('value', '2')
    expect(wrapper.vm.dirty()).toBe(false)
    await input.setValue('5')
    configureQuotaPerUnit(100_000)
    await flushPromises()
    expect(input.element).toHaveProperty('value', '5')
    expect(wrapper.vm.commit()?.pricing).toEqual({ ModelRatio: '0.5' })
    wrapper.unmount()
  })

  it('keeps its own save action and saves only a committed draft', async () => {
    const wrapper = setup()
    const save = () =>
      wrapper
        .findAll('button')
        .find((button) => button.text() === 'Save model prices')!
    // An empty expression is not free, so nothing is saved.
    await save().trigger('click')
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper.get('textarea').setValue('0')
    await save().trigger('click')
    expect(wrapper.emitted('update')?.at(-1)).toEqual([
      'test-model',
      {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': '0',
      },
    ])
    expect(wrapper.emitted('save')).toHaveLength(1)
    wrapper.unmount()
  })
  it.each([
    {},
    { ModelRatio: '0.123456789012345678901', CompletionRatio: '3' },
    { ModelPrice: '0.123456789012345678901' },
    {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'v1:p * 2',
    },
  ] satisfies PricingValues[])(
    'edits a plugin price independently of the default price %j',
    async (defaultPricing) => {
      const pricing: PricingValues = {
        ...defaultPricing,
        'billing_setting.plugin_billing_expr': { provider: 'u("seconds")' },
      }
      const wrapper = setup(pricing)
      await wrapper.setProps({
        siteCurrency: 'CNY',
        exchangeRate: '7',
        entry: {
          model_name: 'test-model',
          version: 'version',
          configured: pricing,
          effective: pricing,
          plugin_variants: [
            {
              plugin_key: 'provider',
              plugin_name: 'Task provider',
              usage_schema: { seconds: { type: 'number', unit: 'second' } },
              configured: 'u("seconds")',
              effective: 'u("seconds")',
              compatible: true,
            },
          ],
        },
      })
      await wrapper
        .findAll('label')
        .find((label) => label.text().startsWith('Display currency'))!
        .get('select')
        .setValue('CNY')
      expect(wrapper.vm.dirty()).toBe(false)
      await wrapper
        .findAll('label')
        .find((label) => label.text().startsWith('Plugin pricing'))!
        .get('select')
        .setValue('provider')
      await wrapper
        .get('.price-expression textarea')
        .setValue('u("seconds") * 2')
      expect(wrapper.vm.commit()?.pricing).toEqual({
        ...defaultPricing,
        'billing_setting.plugin_billing_expr': {
          provider: 'u("seconds") * 2',
        },
      })
      await wrapper.get('.price-expression textarea').setValue('')
      expect(wrapper.vm.commit()?.pricing).toEqual(defaultPricing)
      wrapper.unmount()
    }
  )
  it('creates only a plugin price for an unconfigured task model', async () => {
    const wrapper = setup()
    await wrapper.setProps({
      entry: {
        model_name: 'test-model',
        version: 'empty',
        configured: {},
        effective: {},
        plugin_variants: [
          {
            plugin_key: 'provider',
            plugin_name: 'Task provider',
            usage_schema: { seconds: { type: 'number', unit: 'second' } },
            configured: '',
            effective: '',
            compatible: true,
          },
        ],
      },
    })
    await wrapper
      .findAll('label')
      .find((label) => label.text().startsWith('Plugin pricing'))!
      .get('select')
      .setValue('provider')
    await wrapper.get('.price-expression textarea').setValue('u("seconds") * 2')
    expect(wrapper.vm.commit()?.pricing).toEqual({
      'billing_setting.plugin_billing_expr': { provider: 'u("seconds") * 2' },
    })
    wrapper.unmount()
  })

  // Ported from upstream 8802113: inheriting plugins keep the shared task
  // price as their own override before the default becomes ordinary pricing.
  it('starts ordinary per-image pricing and keeps the inherited plugin price', async () => {
    const shared = 'tier("base", u("image_count") * 0.04)'
    const wrapper = setup(tiered(shared))
    await wrapper.setProps({
      entry: {
        model_name: 'test-model',
        version: 'version',
        configured: tiered(shared),
        effective: tiered(shared),
        plugin_variants: [
          {
            plugin_key: 'provider',
            plugin_name: 'Task provider',
            usage_schema: { image_count: { type: 'number', unit: 'count' } },
            configured: '',
            effective: shared,
            compatible: true,
          },
        ],
      },
    })
    await button(wrapper, 'Configure ordinary OpenAI pricing').trigger('click')
    // The zero placeholder must be priced before it can be saved.
    expect(wrapper.vm.commit()).toBeNull()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Set the ordinary OpenAI price first')
    await button(wrapper, 'Visual tiers').trigger('click')
    await field(wrapper, '.price-tier', 'Price per image (USD)').setValue(
      '0.02'
    )
    expect(wrapper.vm.commit()?.pricing).toEqual({
      ...tiered('tier("image", fixed(0.02)) * image_count'),
      'billing_setting.plugin_billing_expr': { provider: shared },
    })
    expect(wrapper.text()).not.toContain('Configure ordinary OpenAI pricing')
    wrapper.unmount()
  })
  it('offers ordinary pricing for a u() default whose plugins all have prices', async () => {
    const shared = 'tier("base", u("seconds") * 0.1)'
    const own = { provider: 'tier("own", u("seconds") * 0.2)' }
    const pricing = {
      ...tiered(shared),
      'billing_setting.plugin_billing_expr': own,
    }
    const wrapper = setup(pricing)
    await wrapper.setProps({
      entry: {
        model_name: 'test-model',
        version: 'version',
        configured: pricing,
        effective: pricing,
        plugin_variants: [
          {
            plugin_key: 'provider',
            plugin_name: 'Task provider',
            usage_schema: { seconds: { type: 'number', unit: 'second' } },
            configured: own.provider,
            effective: own.provider,
            compatible: true,
          },
        ],
      },
    })
    await button(wrapper, 'Configure ordinary OpenAI pricing').trigger('click')
    await wrapper.get('textarea').setValue('tier("base", p * 2 + c * 4)')
    expect(wrapper.vm.commit()?.pricing).toEqual({
      ...tiered('tier("base", p * 2 + c * 4)'),
      'billing_setting.plugin_billing_expr': own,
    })
    wrapper.unmount()
  })
  it('does not invent a free expression for an empty model', async () => {
    const wrapper = setup()
    expect(wrapper.get('textarea').element).toHaveProperty('value', '')
    expect(wrapper.vm.commit()).toBeNull()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('explicitly enter 0')
    await wrapper.get('textarea').setValue('0')
    expect(wrapper.vm.commit()?.pricing).toEqual({
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': '0',
    })
    wrapper.unmount()
  })
  it('keeps the version prefix at the start when applying a task condition', async () => {
    const pricing: PricingValues = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'v1:u("seconds") * 0.4',
    }
    const wrapper = setup(pricing)
    await wrapper.setProps({
      entry: {
        model_name: 'test-model',
        version: 'version',
        configured: pricing,
        effective: pricing,
        usage_schema: { seconds: { type: 'number', unit: 'second' } },
      },
    })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Add condition')!
      .trigger('click')
    const condition = wrapper.get('.pricing-task-condition')
    await condition.findAll('select')[0]!.setValue('seconds')
    await condition.get('input').setValue('10')
    await wrapper
      .findAll('label')
      .find((label) => label.text() === 'Multiplier')!
      .get('input')
      .setValue('2')
    await wrapper
      .findAll('button')
      .filter((button) => button.text() === 'Apply')
      .at(-1)!
      .trigger('click')
    expect(wrapper.vm.commit()?.pricing['billing_setting.billing_expr']).toBe(
      'v1:(u("seconds") * 0.4) * ((u("seconds") == 10) ? 2 : 1)'
    )
    wrapper.unmount()
  })
  it('preserves input precision and emits a draft without saving', async () => {
    const save = vi.spyOn(pricingConfigApi, 'save')
    const wrapper = setup({ ModelPrice: '0.000000123456789012' })
    const result = wrapper.vm.commit()
    expect(result?.pricing.ModelPrice).toBe('0.000000123456789012')
    expect(wrapper.emitted('update')).toHaveLength(1)
    expect(save).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('preserves exact USD input through display-currency round trips', async () => {
    const price = '0.123456789012345678901'
    const wrapper = setup({ ModelPrice: price })
    await wrapper.setProps({ siteCurrency: 'CNY', exchangeRate: '7' })
    const currency = wrapper
      .findAll('label')
      .find((label) => label.text().startsWith('Display currency'))!
      .get('select')
    await currency.setValue('CNY')
    expect(wrapper.vm.dirty()).toBe(false)
    expect(wrapper.vm.commit()?.pricing).toEqual({ ModelPrice: price })
    await currency.setValue('USD')
    expect(wrapper.get('input[inputmode="decimal"]').element).toHaveProperty(
      'value',
      price
    )
    expect(wrapper.vm.dirty()).toBe(false)
    expect(wrapper.vm.commit()?.pricing).toEqual({ ModelPrice: price })
    wrapper.unmount()
  })
  it('keeps conversion a draft requiring existing save', async () => {
    const convert = vi
      .spyOn(pricingConfigApi, 'convert')
      .mockResolvedValue({ effective: {}, expression: 'fixed(0.2)' })
    const save = vi.spyOn(pricingConfigApi, 'save')
    const wrapper = setup({ ModelPrice: '0.2' })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Convert to expression')!
      .trigger('click')
    await flushPromises()
    expect(convert).toHaveBeenCalledWith('test-model', { ModelPrice: '0.2' })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Confirm')!
      .trigger('click')
    expect(wrapper.vm.commit()?.pricing['billing_setting.billing_expr']).toBe(
      'fixed(0.2)'
    )
    expect(save).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
