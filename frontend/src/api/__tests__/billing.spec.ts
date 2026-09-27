import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  billingApi,
  parseBillingCharge,
  parseBillingEvent,
  parseBillingReservation,
  parseBillingAudit,
} from '@/api/billing'

const reservation = {
  request_id: 'request-1',
  user_id: 1,
  token_id: 2,
  state: 'active',
  funding_amount: '9007199254740993',
  token_amount: '0',
  version: 3,
  created_at: 1,
  updated_at: 2,
  last_error: '',
  manual_review_required: true,
}
const audit = {
  status: 'unconfirmed',
  attempts: [],
  replayed_charge: null,
  quoted_total_usd: null,
  customer_charge_usd: '0',
  customer_charge_quota: '0',
  customer_charge_confirmed: false,
  charge_difference_usd: null,
  charge_difference_quota: null,
  upstream_cost_usd: null,
  profit_usd: null,
  cost_status: 'unknown',
}
const upstream_cost = {
  attempt_count: 0,
  confirmed_attempt_count: 0,
  complete: false,
  upstream_cost_usd: null,
  records: [],
}
afterEach(() => vi.restoreAllMocks())

describe('billing API', () => {
  it('retains signed audit differences and never invents profit from unknown costs', () => {
    expect(
      parseBillingAudit(
        {
          ...audit,
          customer_charge_confirmed: true,
          charge_difference_usd: '-0.123456789012345678',
          charge_difference_quota: '-1',
        },
        '/audit'
      )
    ).toMatchObject({
      profit_usd: null,
      charge_difference_usd: '-0.123456789012345678',
      charge_difference_quota: '-1',
    })
    expect(() =>
      parseBillingAudit({ ...audit, profit_usd: '0' }, '/audit')
    ).toThrow()
  })
  it('posts immutable cost evidence without converting decimal amounts', async () => {
    const command = {
      event_id: 'cost-1',
      attempt_id: 'attempt-1',
      amount_usd: '0.123456789012345678',
      source: 'invoice' as const,
      reference: 'invoice-1',
    }
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      ...command,
      id: 1,
      request_id: 'request/1',
      actor_id: 2,
      created_at: 1,
    })
    expect((await billingApi.recordCost('request/1', command)).amount_usd).toBe(
      command.amount_usd
    )
    expect(post).toHaveBeenCalledWith(
      '/api/billing/reconciliations/request%2F1/cost',
      command
    )
  })
  it('keeps saved rule multipliers as decimal strings and rejects floating point projections', () => {
    const charge = {
      lines: [],
      final_quota: '1',
      base_usd: '0.000002',
      adjustments: [
        {
          kind: 'channel_multiplier',
          multiplier: '1',
          base_usd: '0.000002',
          amount_usd: '0',
          result_usd: '0.000002',
        },
      ],
      total_usd: '0.000002',
      unrounded_quota: '1',
      rounding_delta: '0',
      calculation_version: 'billing-v3',
      rounding_version: 'half-up-positive-minimum-v1',
      trace: {
        matched_tier: '',
        request_rules: [
          {
            cond: 'hour("UTC") < 6',
            multiplier: '0.123456789012345678',
            matched: true,
          },
        ],
      },
    }
    expect(
      parseBillingCharge(charge, '/billing')?.trace?.request_rules?.[0]
        ?.multiplier
    ).toBe('0.123456789012345678')
    for (const multiplier of [0.5, 'NaN', '1e9', '-1'])
      expect(() =>
        parseBillingCharge(
          {
            ...charge,
            trace: {
              ...charge.trace,
              request_rules: [{ ...charge.trace.request_rules[0], multiplier }],
            },
          },
          '/billing'
        )
      ).toThrow()
  })
  it('retains a confirmed image count without inventing missing trace evidence', () => {
    const charge = {
      lines: [],
      final_quota: '1000',
      base_usd: '0.002',
      adjustments: [
        {
          kind: 'channel_multiplier',
          multiplier: '1',
          base_usd: '0.002',
          amount_usd: '0',
          result_usd: '0.002',
        },
      ],
      total_usd: '0.002',
      unrounded_quota: '1000',
      rounding_delta: '0',
      calculation_version: 'billing-v3',
      rounding_version: 'half-up-positive-minimum-v1',
      trace: { matched_tier: '', image_count: 2 },
    }
    expect(parseBillingCharge(charge, '/billing')?.trace?.image_count).toBe(2)
    expect(
      parseBillingCharge({ ...charge, trace: { matched_tier: '' } }, '/billing')
        ?.trace?.image_count
    ).toBeUndefined()
    for (const image_count of [null, 0, -1, 1.5, 129, '2']) {
      expect(() =>
        parseBillingCharge(
          { ...charge, trace: { matched_tier: '', image_count } },
          '/billing'
        )
      ).toThrow('Invalid API response')
    }
  })
  it('retains immutable attempt evidence without presentation rounding', async () => {
    const attempts = [
      {
        id: 'attempt-1',
        channel_id: 2,
        quote_hash: 'quote-hash',
        created_at: 3,
        quote: {
          channel_ratio: '1.123456789',
          price_version: { prices: [{ price: '0.123456789' }] },
        },
      },
    ]
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      recovery: reservation,
      audit,
      upstream_cost,
      quote: null,
      attempts,
      usage: null,
      charge: null,
      events: [],
    })
    expect((await billingApi.detail('request-1')).attempts).toEqual(attempts)
    get.mockResolvedValue({
      recovery: reservation,
      audit,
      upstream_cost,
      quote: null,
      attempts: null,
      usage: null,
      charge: null,
      events: [],
    })
    await expect(billingApi.detail('request-1')).rejects.toThrow(
      'Invalid API response'
    )
  })
  it('keeps reconciliation actionable when a charge snapshot is unavailable', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      recovery: reservation,
      audit,
      upstream_cost,
      quote: null,
      attempts: [],
      usage: null,
      charge: { unavailable: true },
      events: [],
    })
    const detail = await billingApi.detail('request-1')
    expect(detail.charge).toBeNull()
    expect(detail.chargeUnavailable).toBe(true)
    expect(detail.recovery.request_id).toBe('request-1')
  })
  it('does not project missing or failed metrics into zero balances', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockRejectedValue(new Error('database unavailable'))
    await expect(billingApi.metrics()).rejects.toThrow('database unavailable')
    get.mockResolvedValue({
      price_versions: 0,
      quote_attempts: 0,
      prices_by_origin: [],
      pending: 0,
      reconciliations: 0,
      reserved_quota: '0',
      oldest_wait_seconds: 0,
      idempotency_conflicts: 0,
      recovery_failures: 0,
      debt_events: 0,
    })
    expect((await billingApi.metrics()).reserved_quota).toBe('0')
    get.mockResolvedValue({ pending: 0 })
    await expect(billingApi.metrics()).rejects.toThrow('Invalid API response')
  })
  it('keeps exact amounts and rejects malformed recovery evidence', () => {
    expect(
      parseBillingReservation(reservation, '/billing').funding_amount
    ).toBe('9007199254740993')
    expect(() =>
      parseBillingReservation(
        { ...reservation, funding_amount: 100 },
        '/billing'
      )
    ).toThrow()
    expect(() =>
      parseBillingReservation({ ...reservation, version: null }, '/billing')
    ).toThrow()
    expect(() => parseBillingEvent({ event_id: 'e' }, '/billing')).toThrow()
  })

  it('sends manual decisions with operation identity, version and exact quota', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const decision = {
      operation_id: 'stable-operation',
      expected_version: 3,
      action: 'settle' as const,
      quota: '123456789',
      reason: 'Verified upstream usage',
    }
    await billingApi.resolve('request/1', decision)
    await billingApi.resolve('request/1', decision)
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/api/billing/reconciliations/request%2F1/resolve',
      decision
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/api/billing/reconciliations/request%2F1/resolve',
      decision
    )
  })

  it('scopes token history queries and propagates request cancellation', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ items: [reservation], total: 1 })
    const controller = new AbortController()
    const result = await billingApi.reservations(20, 10, controller.signal, 2)
    expect(get).toHaveBeenCalledWith(
      '/api/billing/self/reservations',
      { offset: 20, limit: 10, token_id: 2 },
      { signal: controller.signal }
    )
    expect(result.items[0]?.token_id).toBe(2)
  })
})
