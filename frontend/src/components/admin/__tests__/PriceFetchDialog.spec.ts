import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PriceFetchDialog from '../PriceFetchDialog.vue'
import { pricingConfigApi } from '@/api/pricingConfig'
afterEach(() => vi.restoreAllMocks())
describe('price source confirmation', () => {
  it('renders precise field and expression differences as inert text', async () => {
    const oldExpression = 'p * 0.123456789012345678 + c * 2'
    const newExpression =
      'p * 0.123456789012345679 + c * 3 + "<img src=x onerror=alert(1)>"'
    vi.spyOn(pricingConfigApi, 'fetch').mockResolvedValue([
      {
        id: 'sub2api',
        name: 'Sub2API',
        status: 'success',
        errors: [],
        models: [
          {
            model_name: 'a',
            provider: 'p',
            pricing: {
              ModelRatio: '0',
              'billing_setting.billing_expr': newExpression,
            },
          },
        ],
      },
    ])
    const wrapper = mount(PriceFetchDialog, {
      props: {
        draft: {
          a: { ModelPrice: '0', 'billing_setting.billing_expr': oldExpression },
        },
      },
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
        stubs: {
          ConsoleModal: { template: '<div><slot /></div>' },
          TablePagination: true,
        },
      },
    })
    await flushPromises()
    await wrapper.get('input[value=sub2api]').setValue(true)
    expect(wrapper.text()).toContain('Not set')
    expect(wrapper.text()).toContain('0.123456789012345679')
    expect(wrapper.findAll('[data-diff="removed"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[data-diff="added"]').length).toBeGreaterThan(0)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.emitted('fill')).toBeUndefined()
    wrapper.unmount()
  })
  it('requires one selected source and fills only selected models without saving', async () => {
    vi.spyOn(pricingConfigApi, 'fetch').mockResolvedValue([
      {
        id: 'sub2api',
        name: 'Sub2API',
        status: 'success',
        errors: [],
        models: [
          { model_name: 'a', provider: 'p', pricing: { ModelPrice: '1' } },
          { model_name: 'b', provider: 'p', pricing: { ModelPrice: '2' } },
        ],
      },
      {
        id: 'axonhub',
        name: 'AxonHub',
        status: 'error',
        errors: ['source unavailable'],
        models: [],
      },
    ])
    const save = vi.spyOn(pricingConfigApi, 'save')
    const wrapper = mount(PriceFetchDialog, {
      props: { draft: { a: { ModelPrice: '4' } } },
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
        stubs: {
          ConsoleModal: { template: '<div><slot /></div>' },
          TablePagination: true,
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('input[type=checkbox]').exists()).toBe(false)
    expect(wrapper.text()).toContain('source unavailable')
    await wrapper.get('input[value=sub2api]').setValue(true)
    await wrapper.get('input[type=checkbox]').setValue(true)
    expect(wrapper.text()).toContain('1 models already have prices')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Fill pricing draft')!
      .trigger('click')
    expect(wrapper.emitted('fill')?.[0]?.[0]).toEqual([
      { model_name: 'a', provider: 'p', pricing: { ModelPrice: '1' } },
    ])
    expect(save).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
