import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import ToolPricingEditor from '../ToolPricingEditor.vue'
describe('tool price settings', () => {
  it('starts empty and does not save incomplete prices as zero', async () => {
    const wrapper = mount(ToolPricingEditor, {
      props: { value: '{}' },
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
      },
    })
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Add price dimension')!
      .trigger('click')
    await wrapper.get('input').setValue('web_search')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save tool prices')!
      .trigger('click')
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper.get('input[inputmode=decimal]').setValue('0')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save tool prices')!
      .trigger('click')
    expect(JSON.parse(String(wrapper.emitted('save')?.[0]?.[0]))).toEqual({
      web_search: 0,
    })
    wrapper.unmount()
  })
})
