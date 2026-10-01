import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import LogBillingDetail from '../LogBillingDetail.vue'
import { parseBillingCharge } from '@/api/billing'
import type { LogItem } from '@/types/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
describe('saved billing detail', () => {
  it('renders newapi-c71eefbc-v1 charge with lines, adjustments and trace', () => {
    const log = {
      model: 'customer-model',
      other: {
        price_version_id: 15,
        requested_service_tier: 'priority',
        actual_service_tier: 'default',
        price_scope: 'channel_model',
        price_channel_id: 9,
        quote_hash: 'frozen-quote-hash',
        upstream_model_name: 'provider-model',
        response_model_name: 'unexpected-model',
        charge_result: {
          lines: [
            {
              quantity: 'expression',
              count: '1',
              unit: 'per_call',
              unit_price: '0.01',
              subtotal_usd: '0.01',
            },
            {
              quantity: 'tool:web_search',
              count: '2',
              unit: 'per_call',
              unit_price: '0.005',
              subtotal_usd: '0.01',
            },
          ],
          base_usd: '0.02',
          adjustments: [
            {
              kind: 'channel_multiplier',
              multiplier: '0.9',
              base_usd: '0.02',
              amount_usd: '-0.002',
              result_usd: '0.018',
            },
          ],
          total_usd: '0.018',
          unrounded_quota: '9000',
          final_quota: '9000',
          rounding_delta: '0',
          calculation_version: 'newapi-c71eefbc-v1',
          rounding_version: 'newapi-v1',
          trace: {
            matched_tier: 'standard',
            image_count: 3,
            request_rules: [
              { cond: 'weekday() < 5', multiplier: '0.9', matched: true },
              { cond: 'hour("UTC") < 8', multiplier: '0.8', matched: false },
            ],
          },
        },
      },
    } as unknown as LogItem
    expect(
      parseBillingCharge(
        (log.other as unknown as { charge_result: unknown }).charge_result,
        'fixture'
      )
    ).not.toBeNull()
    const wrapper = mount(LogBillingDetail, {
      props: { log },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('provider-model')
    expect(wrapper.text()).toContain('priority')
    expect(wrapper.text()).toContain('Reported service tier (audit only)')
    expect(wrapper.text()).toContain('channel_model')
    expect(wrapper.text()).toContain('frozen-quote-hash')
    expect(wrapper.text()).toContain('unexpected-model')
    expect(wrapper.text()).toContain('Response model differs')
    expect(wrapper.text()).toContain('$0.018')
    expect(wrapper.text()).toContain('-0.002 USD')
    expect(wrapper.text()).toContain('9000 quota')
    expect(wrapper.text()).toContain('standard')
    expect(wrapper.text()).toContain('Matched')
    expect(wrapper.text()).toContain('Not matched')
    expect(wrapper.text()).toContain(
      `${i18n.global.t('billing.quantities.images')}: 3`
    )
    expect(wrapper.text()).toContain('newapi-c71eefbc-v1')
    expect(wrapper.text()).not.toContain('not available yet')
    wrapper.unmount()
  })

  it('renders historical billing-v3 charge', () => {
    const log = {
      model: 'test-model',
      other: {
        price_version_id: 12,
        price_scope: 'global_model',
        quote_hash: 'legacy-hash',
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
          base_usd: '0.2',
          adjustments: [
            {
              kind: 'channel_multiplier',
              multiplier: '1',
              base_usd: '0.2',
              amount_usd: '0',
              result_usd: '0.2',
            },
          ],
          total_usd: '0.2',
          unrounded_quota: '100000',
          rounding_delta: '0',
          calculation_version: 'billing-v3',
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
    expect(wrapper.text()).toContain('$0.200')
    expect(wrapper.text()).toContain('night')
    expect(wrapper.text()).toContain('billing-v3')
    expect(wrapper.text()).not.toContain('not available yet')
    wrapper.unmount()
  })

  it('does not invent a price for missing saved evidence', () => {
    const wrapper = mount(LogBillingDetail, {
      props: { log: { model: 'test', other: {} } as LogItem },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('not available yet')
    expect(wrapper.text()).not.toContain('$0.000')
    expect(wrapper.text()).not.toContain('invalid')
    wrapper.unmount()
  })

  it('shows invalid message for malformed charge evidence', () => {
    const wrapper = mount(LogBillingDetail, {
      props: {
        log: {
          model: 'test',
          other: {
            charge_result: {
              lines: [],
              base_usd: '0.5',
              adjustments: [],
              total_usd: '0.5',
              unrounded_quota: '250000',
              final_quota: '250000',
              rounding_delta: '0',
              calculation_version: 'unknown-version',
              rounding_version: 'newapi-v1',
            },
          },
        } as unknown as LogItem,
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('invalid')
    expect(wrapper.text()).not.toContain('$0.500')
    expect(wrapper.text()).not.toContain('not available yet')
    wrapper.unmount()
  })

  it('distinguishes malformed falsy evidence from absent evidence without stale state', async () => {
    const wrapper = mount(LogBillingDetail, {
      props: { log: { model: 'test', other: {} } as LogItem },
      global: { plugins: [i18n] },
    })
    for (const charge_result of [false, 0, '', [], {}]) {
      await wrapper.setProps({
        log: { model: 'test', other: { charge_result } } as unknown as LogItem,
      })
      expect(wrapper.text()).toContain('Saved billing evidence is invalid')
      expect(wrapper.text()).not.toContain('not available yet')
      expect(wrapper.text()).not.toContain('$0.000')
    }
    for (const other of [{}, { charge_result: null }]) {
      await wrapper.setProps({
        log: { model: 'test', other } as unknown as LogItem,
      })
      expect(wrapper.text()).toContain('not available yet')
      expect(wrapper.text()).not.toContain('invalid')
    }
    wrapper.unmount()
  })

  it('renders an actual Go expression result with an exponent trace and a saved zero charge', async () => {
    // Captured from billing.Calculate for p * (hour("UTC") < 24 ? 0.000001 : 1), p=100.
    const charge_result = {
      base_usd: '0.00000000009999999999999999',
      adjustments: [
        {
          kind: 'channel_multiplier',
          multiplier: '1',
          base_usd: '0.00000000009999999999999999',
          amount_usd: '0',
          result_usd: '0.00000000009999999999999999',
        },
      ],
      lines: [
        {
          quantity: 'expression',
          count: '1',
          unit: 'per_call',
          unit_price: '0.00000000009999999999999999',
          subtotal_usd: '0.00000000009999999999999999',
        },
      ],
      total_usd: '0.00000000009999999999999999',
      unrounded_quota: '0.000049999999999999996',
      final_quota: '0',
      rounding_delta: '-0.000049999999999999996',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
      expression_version: 1,
      trace: {
        billing_unit: 'token',
        matched_tier: '',
        request_rules: [
          { cond: 'hour("UTC") < 24', multiplier: '1e-06', matched: true },
        ],
      },
    }
    const wrapper = mount(LogBillingDetail, {
      props: {
        log: {
          model: 'test',
          other: { charge_result: false },
        } as unknown as LogItem,
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('invalid')
    await wrapper.setProps({
      log: { model: 'test', other: { charge_result } } as unknown as LogItem,
    })
    expect(wrapper.text()).toContain('1e-06')
    expect(wrapper.text()).toContain('$0.000')
    expect(wrapper.text()).toContain('0 quota')
    expect(wrapper.text()).not.toContain('invalid')
    expect(wrapper.text()).not.toContain('not available yet')
    wrapper.unmount()
  })
})
