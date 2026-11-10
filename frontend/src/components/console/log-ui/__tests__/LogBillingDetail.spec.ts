import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import LogBillingDetail from '../LogBillingDetail.vue'
import type { LogItem } from '@/types/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
describe('native consumption log detail', () => {
  it('shows the charged quota and the original native evidence', () => {
    const wrapper = mount(LogBillingDetail, {
      props: {
        log: {
          model: 'customer-model',
          quota: '9000',
          other: JSON.stringify({
            model_ratio: 2,
            channel_ratio: '1.5',
            upstream_model_name: 'provider-model',
          }),
        } as LogItem,
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('customer-model')
    expect(wrapper.text()).toContain('$0.02')
    expect(JSON.parse(wrapper.get('pre').text())).toEqual({
      model_ratio: 2,
      channel_ratio: '1.5',
      upstream_model_name: 'provider-model',
    })
    wrapper.unmount()
  })

  it('does not invent price evidence when native details are absent', () => {
    const wrapper = mount(LogBillingDetail, {
      props: { log: { model: 'test', quota: '0', other: '{}' } as LogItem },
      global: { plugins: [i18n] },
    })
    expect(JSON.parse(wrapper.get('pre').text())).toEqual({})
    expect(wrapper.text()).not.toContain('quote_hash')
    wrapper.unmount()
  })
})
