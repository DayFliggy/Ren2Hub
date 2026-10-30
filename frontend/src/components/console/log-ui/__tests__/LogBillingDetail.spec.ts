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

  // The public object written by service/usage_discount_log.go.
  const publicDiscount = {
    status: 'ok',
    factor: '0.97',
    applied_rate: '0.03',
    fixed_rate: '0.01',
    site_rate: '0',
    day_rate: '0',
    week_rate: '0.01',
    month_rate: '0.02',
    cap_applied: false,
    winner_window: 'month',
    base_channel_ratio: '1.5',
  }
  function render(other: Record<string, unknown>) {
    return mount(LogBillingDetail, {
      props: {
        log: {
          model: 'm',
          quota: '1000',
          other: JSON.stringify(other),
        } as LogItem,
      },
      global: { plugins: [i18n] },
    })
  }

  it('explains the effective channel ratio with the usage discount parts', () => {
    const evidence = { channel_ratio: '1.455', usage_discount: publicDiscount }
    const wrapper = render(evidence)
    const summary = wrapper.get('[data-log-usage-discount]').text()

    expect(summary).toContain('Base channel ratio1.5×')
    expect(summary).toContain('Discount factor0.97×')
    expect(summary).toContain('Applied discount3%')
    expect(summary).toContain(
      'Fixed 1% · Site 0% · Day 0% · Week 1% · Month 2%'
    )
    expect(summary).toContain('Winning windowMonth')
    expect(summary).toContain('CapNot reached')
    // Users never see administrator fields.
    expect(wrapper.find('[data-log-usage-discount-admin]').exists()).toBe(false)
    // The raw evidence stays complete.
    expect(JSON.parse(wrapper.get('pre').text())).toEqual(evidence)
    wrapper.unmount()
  })

  it('adds the administrator projection, including shadow-only decisions', () => {
    const wrapper = render({
      usage_discount: publicDiscount,
      admin_info: {
        usage_discount: {
          status: 'ok',
          raw_rate: '0.06',
          cap_rate: '0.05',
          config_version: 4,
          shadow_rates: { site: '0.02' },
        },
      },
    })
    const admin = wrapper.get('[data-log-usage-discount-admin]').text()
    expect(admin).toContain('Uncapped discount6%')
    expect(admin).toContain('Discount cap5%')
    expect(admin).toContain('Configuration version4')
    expect(admin).toContain('Shadow ratesSite 2%')
    wrapper.unmount()

    // Pure shadow periods write only the administrator object.
    const shadow = render({
      admin_info: {
        usage_discount: {
          status: 'ok',
          raw_rate: '0.01',
          cap_rate: '0.05',
          config_version: 0,
          shadow_rates: { day: '0.01', month: '0' },
        },
      },
    })
    expect(shadow.get('[data-log-usage-discount]').text()).toContain(
      'StatusComputed'
    )
    expect(shadow.text()).not.toContain('Base channel ratio')
    expect(shadow.get('[data-log-usage-discount-admin]').text()).toContain(
      'Day 1% · Month 0%'
    )
    shadow.unmount()
  })

  it('shows no summary for malformed discount evidence and keeps the raw JSON', () => {
    const evidence = {
      usage_discount: { ...publicDiscount, factor: 0.97 },
    }
    const wrapper = render(evidence)
    expect(wrapper.find('[data-log-usage-discount]').exists()).toBe(false)
    expect(JSON.parse(wrapper.get('pre').text())).toEqual(evidence)
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
