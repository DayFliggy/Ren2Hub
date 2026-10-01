import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { billingApi, parseBillingEvent } from '@/api/billing'

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
    for (const field of [
      'available_delta',
      'used_delta',
      'token_available_delta',
      'token_used_delta',
    ] as const) {
      expect(
        parseBillingEvent(
          { ...event, [field]: '-9223372036854775808' },
          '/billing'
        )[field]
      ).toBe('-9223372036854775808')
      for (const invalid of [
        '9223372036854775808',
        '-9223372036854775809',
        '1e6',
        '0.1',
        100,
      ])
        expect(() =>
          parseBillingEvent({ ...event, [field]: invalid }, '/billing')
        ).toThrow('Invalid API response')
    }
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
