import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  billingApi,
  parseBillingCharge,
  parseBillingEvent,
} from '@/api/billing'

const event = {
  event_id: 'settle:request-1',
  request_id: 'request-1',
  operation: 'settle',
  actor_id: 1,
  available_delta: '-9007199254740993',
  reserved_delta: '0',
  used_delta: '9007199254740993',
  token_available_delta: '-9007199254740993',
  token_reserved_delta: '0',
  token_used_delta: '9007199254740993',
  available_after: '0',
  reserved_after: '0',
  used_after: '9007199254740993',
  reason: '',
  created_at: 1,
}
afterEach(() => vi.restoreAllMocks())

describe('billing API', () => {
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
  it('keeps exact fund-flow amounts and rejects malformed events', () => {
    expect(parseBillingEvent(event, '/billing').used_delta).toBe(
      '9007199254740993'
    )
    expect(() =>
      parseBillingEvent({ ...event, used_delta: 100 }, '/billing')
    ).toThrow()
    expect(() =>
      parseBillingEvent({ ...event, created_at: null }, '/billing')
    ).toThrow()
    expect(() => parseBillingEvent({ event_id: 'e' }, '/billing')).toThrow()
  })

  it('scopes token history queries and propagates request cancellation', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ items: [event], total: 1 })
    const controller = new AbortController()
    const result = await billingApi.events(20, 10, controller.signal, 2)
    expect(get).toHaveBeenCalledWith(
      '/api/billing/self/events',
      { offset: 20, limit: 10, token_id: 2 },
      { signal: controller.signal }
    )
    expect(result.items[0]?.event_id).toBe(event.event_id)
    get.mockResolvedValue({ items: [event], total: -1 })
    await expect(billingApi.events(0, 10)).rejects.toThrow(
      'Invalid API response'
    )
  })
})
