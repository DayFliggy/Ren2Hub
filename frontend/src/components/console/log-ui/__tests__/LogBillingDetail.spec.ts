import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import LogBillingDetail from '../LogBillingDetail.vue'
import type { LogItem } from '@/types/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
describe('saved billing detail', () => {
  it('renders saved charge and matched rules without fetching current prices', () => {
    const log = {
      model: 'customer-model',
      other: {
        price_version_id: 12,
        price_scope: 'channel_model',
        price_channel_id: 9,
        quote_hash: 'frozen-quote-hash',
        upstream_model_name: 'provider-model',
        response_model_name: 'unexpected-model',
        charge_result: {
          lines: [
            {
              quantity: 'images',
              count: '2',
              unit: 'per_image',
              unit_price: '0.1',
              subtotal_usd: '0.2',
            },
          ],
          final_quota: '100000',
          total_usd: '0.2',
          unrounded_quota: '100000',
          rounding_delta: '0',
          calculation_version: 'billing-v2',
          rounding_version: 'half-up-positive-minimum-v1',
          trace: {
            matched_tier: 'night',
            image_count: 2,
            request_rules: [
              { cond: 'hour("UTC") < 6', multiplier: '0.5', matched: true },
            ],
          },
        },
      },
    } as unknown as LogItem
    const wrapper = mount(LogBillingDetail, {
      props: { log },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('provider-model')
    expect(wrapper.text()).toContain('channel_model')
    expect(wrapper.text()).toContain('frozen-quote-hash')
    expect(wrapper.text()).toContain('unexpected-model')
    expect(wrapper.text()).toContain('Response model differs')
    expect(wrapper.text()).toContain('$0.200')
    expect(wrapper.text()).toContain('night')
    expect(wrapper.text()).toContain('Matched')
    expect(wrapper.text()).toContain(
      `${i18n.global.t('billing.quantities.images')}: 2`
    )
    wrapper.unmount()
  })
  it('does not invent a price for missing saved evidence', () => {
    const wrapper = mount(LogBillingDetail, {
      props: { log: { model: 'test', other: '{}' } as LogItem },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('not available yet')
    expect(wrapper.text()).not.toContain('$0.000')
    wrapper.unmount()
  })
})
