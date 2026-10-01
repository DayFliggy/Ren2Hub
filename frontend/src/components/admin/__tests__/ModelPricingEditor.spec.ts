import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelPricingEditor from '../ModelPricingEditor.vue'
import { pricingConfigApi, type PricingValues } from '@/api/pricingConfig'
const plugins = [
  createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
]
beforeEach(() => {
  vi.spyOn(pricingConfigApi, 'preview').mockResolvedValue({ effective: {} })
})
afterEach(() => {
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
describe('manual model price editor', () => {
  it('simulates task facts without hidden request fields while keeping the clock', async () => {
    const simulate = vi
      .spyOn(pricingConfigApi, 'simulate')
      .mockResolvedValue({ amount_usd: '0.4' })
    const pricing: PricingValues = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'u("seconds") * 0.4',
    }
    const wrapper = setup(pricing)
    await wrapper
      .findAll('label')
      .find((label) => label.text() === 'Parameters JSON')!
      .get('textarea')
      .setValue('invalid JSON')
    await wrapper.setProps({
      entry: {
        model_name: 'test-model',
        version: 'version',
        configured: pricing,
        effective: pricing,
        usage_schema: { seconds: { type: 'number', unit: 'second' } },
      },
    })
    expect(wrapper.text()).not.toContain('Parameters JSON')
    expect(wrapper.text()).not.toContain('Headers JSON')
    expect(wrapper.text()).toContain('Task models use task usage facts')
    expect(wrapper.text()).toContain('Simulation time')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Calculate draft charge')!
      .trigger('click')
    await flushPromises()
    expect(simulate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: { params: {}, headers: {} },
        time: expect.any(String),
      })
    )
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
  it('surfaces server simulation failures without displaying a fake zero', async () => {
    vi.spyOn(pricingConfigApi, 'simulate').mockRejectedValue(
      new Error('missing usage')
    )
    const wrapper = setup({
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'p * 2',
    })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Calculate draft charge')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('missing usage')
    expect(wrapper.text()).not.toContain('$0.000')
    wrapper.unmount()
  })
})
