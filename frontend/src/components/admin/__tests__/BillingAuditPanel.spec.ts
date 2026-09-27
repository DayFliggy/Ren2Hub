import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import BillingAuditPanel from '../BillingAuditPanel.vue'
import {
  billingApi,
  type BillingDetail,
  type BillingUpstreamCost,
} from '@/api/billing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

function detailFixture(): BillingDetail {
  return {
    recovery: {
      request_id: 'request-1',
      user_id: 1,
      token_id: 2,
      state: 'settled',
      funding_amount: '1',
      token_amount: '1',
      version: 1,
      created_at: 1,
      updated_at: 1,
      last_error: '',
      manual_review_required: false,
    },
    quote: null,
    attempts: [],
    usage: null,
    charge: null,
    chargeUnavailable: false,
    events: [],
    audit: {
      status: 'verified',
      attempts: [
        {
          id: 'attempt-1',
          channel_id: 9,
          request_fingerprint: 'fp',
          price_scope: 'global_model',
          price_version_id: 1,
          price_version_hash: 'version-hash',
          quote_hash: 'quote-hash',
          origin: 'manual',
          source_hash: '',
        },
      ],
      replayed_charge: null,
      quoted_total_usd: '1',
      customer_charge_usd: '1',
      customer_charge_quota: '500000',
      customer_charge_confirmed: true,
      charge_difference_usd: '0',
      charge_difference_quota: '0',
      upstream_cost_usd: null,
      profit_usd: null,
      cost_status: 'unknown',
    },
    upstream_cost: {
      attempt_count: 1,
      confirmed_attempt_count: 0,
      complete: false,
      upstream_cost_usd: null,
      records: [],
    },
  }
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('confirmed upstream cost registration', () => {
  it('keeps unknown profit unknown and retries identical evidence with the same event identity', async () => {
    vi.stubGlobal('crypto', {
      getRandomValues: crypto.getRandomValues.bind(crypto),
    })
    const detail = detailFixture()
    const record: BillingUpstreamCost = {
      id: 1,
      request_id: 'request-1',
      actor_id: 1,
      created_at: 1,
      event_id: 'event',
      attempt_id: 'attempt-1',
      amount_usd: '0.123456789012345678',
      source: 'invoice',
      reference: 'invoice-1',
    }
    const post = vi
      .spyOn(billingApi, 'recordCost')
      .mockRejectedValueOnce(new Error('response lost'))
      .mockResolvedValue(record)
    const confirmed: BillingDetail = {
      ...detail,
      audit: {
        ...detail.audit,
        upstream_cost_usd: record.amount_usd,
        profit_usd: '0.876543210987654322',
        cost_status: 'confirmed',
      },
      upstream_cost: {
        attempt_count: 1,
        confirmed_attempt_count: 1,
        complete: true,
        upstream_cost_usd: record.amount_usd,
        records: [record],
      },
    }
    vi.spyOn(billingApi, 'detail').mockResolvedValue(confirmed)
    const wrapper = mount(BillingAuditPanel, {
      props: { detail },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-audit-amount=profit_usd]').text()).toBe(
      'Not confirmed'
    )
    await wrapper.get('[data-cost-attempt]').setValue('attempt-1')
    await wrapper.get('[data-cost-amount]').setValue(record.amount_usd)
    await wrapper.get('[data-cost-reference]').setValue(record.reference)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[0]).toEqual(post.mock.calls[1])
    expect(post.mock.calls[0]?.[1].amount_usd).toBe(record.amount_usd)
    expect(post.mock.calls[0]?.[1].event_id).toMatch(/^[0-9a-f]{32}$/)
    expect(wrapper.emitted('updated')?.[0]).toEqual([confirmed])
    await wrapper.setProps({ detail: confirmed })
    expect(wrapper.get('[data-audit-amount=profit_usd]').text()).toBe(
      '0.877 USD'
    )
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })
  it('rejects numeric exponent amounts and cannot register an unknown attempt', async () => {
    const post = vi.spyOn(billingApi, 'recordCost')
    const wrapper = mount(BillingAuditPanel, {
      props: { detail: detailFixture() },
      global: { plugins: [i18n] },
    })
    await wrapper.get('[data-cost-amount]').setValue('1e3')
    await wrapper.get('[data-cost-reference]').setValue('invoice')
    await wrapper.get('[data-cost-attempt]').setValue('attempt-1')
    await wrapper.get('form').trigger('submit')
    expect(post).not.toHaveBeenCalled()
    expect(
      wrapper.get('[data-cost-submit]').attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })
})
