import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  billingApi,
  parseBillingEvent,
  parseBillingCharge,
  type BillingCharge,
} from '@/api/billing'
import { formatDecimal, formatDecimalUSD } from '@/utils/decimal'

const emptyCharge = {
  lines: [],
  base_usd: '0',
  adjustments: [],
  total_usd: '0',
  unrounded_quota: '0',
  final_quota: '0',
  rounding_delta: '0',
  calculation_version: 'newapi-c71eefbc-v1',
  rounding_version: 'newapi-v1',
}

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

  it('parses complete newapi-c71eefbc-v1 charge with lines, adjustments and trace', () => {
    const rawCharge = {
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
          multiplier: '1.2',
          base_usd: '0.02',
          amount_usd: '0.004',
          result_usd: '0.024',
        },
      ],
      total_usd: '0.024',
      unrounded_quota: '12000',
      final_quota: '12000',
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
    }
    const charge = parseBillingCharge(rawCharge, '/test')
    expect(charge.lines).toHaveLength(2)
    expect(charge.lines[0]!.quantity).toBe('expression')
    expect(charge.lines[1]!.count).toBe('2')
    expect(charge.adjustments).toHaveLength(1)
    expect(charge.adjustments[0]!.multiplier).toBe('1.2')
    expect(charge.final_quota).toBe('12000')
    expect(charge.calculation_version).toBe('newapi-c71eefbc-v1')
    expect(charge.rounding_version).toBe('newapi-v1')
    expect(charge.trace?.matched_tier).toBe('standard')
    expect(charge.trace?.image_count).toBe(3)
    expect(charge.trace?.request_rules).toHaveLength(2)
    expect(charge.trace?.request_rules![0]!.matched).toBe(true)
  })

  it('parses historical billing-v3 charge with half-up-positive-minimum-v1 rounding', () => {
    const legacy = {
      lines: [
        {
          quantity: 'images',
          count: '2',
          unit: 'per_image',
          unit_price: '0.1',
          subtotal_usd: '0.2',
        },
      ],
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
      final_quota: '100000',
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
    }
    const charge = parseBillingCharge(legacy, '/test')
    expect(charge.calculation_version).toBe('billing-v3')
    expect(charge.rounding_version).toBe('half-up-positive-minimum-v1')
    expect(charge.lines[0]!.unit_price).toBe('0.1')
  })

  it('accepts charge without optional trace', () => {
    const minimal = {
      lines: [],
      base_usd: '0',
      adjustments: [],
      total_usd: '0',
      unrounded_quota: '0',
      final_quota: '0',
      rounding_delta: '0',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
    }
    const charge = parseBillingCharge(minimal, '/test')
    expect(charge.trace).toBeUndefined()
    expect(charge.lines).toEqual([])
    expect(charge.adjustments).toEqual([])
  })

  it('rejects unknown calculation or rounding version', () => {
    const base = {
      lines: [],
      base_usd: '0',
      adjustments: [],
      total_usd: '0',
      unrounded_quota: '0',
      final_quota: '0',
      rounding_delta: '0',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
    }
    expect(() =>
      parseBillingCharge(
        { ...base, calculation_version: 'unknown-v999' },
        '/test'
      )
    ).toThrow('Invalid API response')
    expect(() =>
      parseBillingCharge({ ...base, rounding_version: 'future-v2' }, '/test')
    ).toThrow('Invalid API response')
    expect(() =>
      parseBillingCharge({ ...base, calculation_version: '' }, '/test')
    ).toThrow('Invalid API response')
    expect(() =>
      parseBillingCharge(
        { ...base, calculation_version: 'billing-v3' },
        '/test'
      )
    ).toThrow('Invalid API response')
  })

  it('accepts the current violation-fee calculator and its saved rounding version', () => {
    const charge = parseBillingCharge(
      {
        ...emptyCharge,
        lines: [
          {
            quantity: 'violation_fee',
            count: '1',
            unit: 'per_call',
            unit_price: '0.01',
            subtotal_usd: '0.01',
          },
        ],
        base_usd: '0.01',
        adjustments: [
          {
            kind: 'channel_multiplier',
            multiplier: '1',
            base_usd: '0.01',
            amount_usd: '0',
            result_usd: '0.01',
          },
        ],
        total_usd: '0.01',
        unrounded_quota: '5000',
        final_quota: '5000',
        rounding_version: 'half-up-positive-minimum-v1',
      },
      '/test'
    )
    expect(charge.final_quota).toBe('5000')
    expect(charge.calculation_version).toBe('newapi-c71eefbc-v1')
    expect(charge.rounding_version).toBe('half-up-positive-minimum-v1')
  })

  it('accepts backend trace multipliers serialized by Go with exponents without changing them', () => {
    for (const multiplier of ['1e-06', '1e+06']) {
      const charge = parseBillingCharge(
        {
          ...emptyCharge,
          trace: {
            request_rules: [
              { cond: 'hour("UTC") < 24', multiplier, matched: true },
            ],
          },
        },
        '/test'
      )
      expect(charge.trace?.request_rules?.[0]?.multiplier).toBe(multiplier)
    }
    for (const multiplier of [
      '-1e-06',
      '01e+06',
      '1e+999',
      '1e-999',
      'Infinity',
      'NaN',
      1e-6,
    ]) {
      expect(() =>
        parseBillingCharge(
          {
            ...emptyCharge,
            trace: {
              request_rules: [{ cond: 'x', multiplier, matched: true }],
            },
          },
          '/test'
        )
      ).toThrow('Invalid API response')
    }
  })

  it('rejects charge with missing required fields or invalid amounts', () => {
    const valid = {
      lines: [],
      base_usd: '0.5',
      adjustments: [],
      total_usd: '0.5',
      unrounded_quota: '250000',
      final_quota: '250000',
      rounding_delta: '0',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
    }
    expect(() =>
      parseBillingCharge({ ...valid, lines: null }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, adjustments: null }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, base_usd: 100 }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, total_usd: '1e6' }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, total_usd: '-0.5' }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, final_quota: '-100' }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, final_quota: 100 }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...valid, calculation_version: null }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge(
        {
          ...valid,
          lines: [{ quantity: 'test', count: 'invalid', unit: 'call' }],
        },
        '/test'
      )
    ).toThrow()
    expect(() =>
      parseBillingCharge(
        {
          ...valid,
          lines: [
            {
              quantity: 'p',
              count: '-100',
              unit: 'per_million_tokens',
              unit_price: '0.01',
              subtotal_usd: '0.01',
            },
          ],
        },
        '/test'
      )
    ).toThrow()
  })

  it('accepts negative rounding_delta and adjustment amount_usd', () => {
    const withNegatives = {
      lines: [],
      base_usd: '0.001',
      adjustments: [
        {
          kind: 'discount',
          multiplier: '0.901',
          base_usd: '0.001',
          amount_usd: '-0.000099',
          result_usd: '0.000901',
        },
      ],
      total_usd: '0.000901',
      unrounded_quota: '450.5',
      final_quota: '450',
      rounding_delta: '-0.5',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
    }
    const charge = parseBillingCharge(withNegatives, '/test')
    expect(charge.rounding_delta).toBe('-0.5')
    expect(charge.adjustments[0]!.amount_usd).toBe('-0.000099')
    expect(formatDecimal(charge.rounding_delta)).toBe('-0.500')
    expect(formatDecimal(charge.adjustments[0]!.amount_usd)).toBe('-0.000')
  })

  it('rejects charge with malformed trace structure', () => {
    const base = {
      lines: [],
      base_usd: '0',
      adjustments: [],
      total_usd: '0',
      unrounded_quota: '0',
      final_quota: '0',
      rounding_delta: '0',
      calculation_version: 'newapi-c71eefbc-v1',
      rounding_version: 'newapi-v1',
    }
    expect(() =>
      parseBillingCharge(
        { ...base, trace: { image_count: 'not_number' } },
        '/test'
      )
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...base, trace: { image_count: -1 } }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...base, trace: { image_count: 129 } }, '/test')
    ).toThrow()
    expect(() =>
      parseBillingCharge({ ...base, trace: { image_count: 0 } }, '/test')
    ).toThrow()
    for (const image_count of [1, 128]) {
      expect(
        parseBillingCharge({ ...base, trace: { image_count } }, '/test').trace
          ?.image_count
      ).toBe(image_count)
    }
    for (const matched of [undefined, null, 'true', 1]) {
      expect(() =>
        parseBillingCharge(
          {
            ...base,
            trace: { request_rules: [{ cond: 'x', multiplier: '1', matched }] },
          },
          '/test'
        )
      ).toThrow('Invalid API response')
    }
    for (const request_rules of [null, {}, 'rules', [null]]) {
      expect(() =>
        parseBillingCharge({ ...base, trace: { request_rules } }, '/test')
      ).toThrow('Invalid API response')
    }
    expect(() =>
      parseBillingCharge(
        { ...base, trace: { request_rules: [{ cond: 'x' }] } },
        '/test'
      )
    ).toThrow()
    expect(() =>
      parseBillingCharge(
        {
          ...base,
          trace: {
            request_rules: [
              {
                cond: 'x',
                multiplier: '1',
                matched: 'maybe' as unknown as boolean,
              },
            ],
          },
        },
        '/test'
      )
    ).toThrow()
    expect(() =>
      parseBillingCharge(
        { ...base, trace: { request_rules: 'not_array' } },
        '/test'
      )
    ).toThrow()
  })

  it('preserves exact decimal precision for charge amounts up to 128 characters', () => {
    const exact = '0.' + '1234567890'.repeat(12) + '123456'
    expect(exact).toHaveLength(128)
    const charge: BillingCharge = parseBillingCharge(
      {
        lines: [
          {
            quantity: 'p',
            count: '1000',
            unit: 'per_million_tokens',
            unit_price:
              '0.00123456789012345678901234567890123456789012345678901234567890',
            subtotal_usd:
              '0.00000123456789012345678901234567890123456789012345678901234567890',
          },
        ],
        base_usd:
          '0.00000123456789012345678901234567890123456789012345678901234567890',
        adjustments: [
          {
            kind: 'channel_multiplier',
            multiplier: '1',
            base_usd:
              '0.00000123456789012345678901234567890123456789012345678901234567890',
            amount_usd: '0',
            result_usd:
              '0.00000123456789012345678901234567890123456789012345678901234567890',
          },
        ],
        total_usd: exact,
        unrounded_quota:
          '0.617283945061728395061728395061728395061728395061728395061728395',
        final_quota: '1',
        rounding_delta:
          '0.382716054938271604938271604938271604938271604938271604938271605',
        calculation_version: 'newapi-c71eefbc-v1',
        rounding_version: 'newapi-v1',
      },
      '/test'
    )
    expect(charge.lines[0]!.unit_price).toBe(
      '0.00123456789012345678901234567890123456789012345678901234567890'
    )
    expect(charge.total_usd).toBe(exact)
    expect(formatDecimalUSD(charge.total_usd)).toBe('$0.123')
    expect(() =>
      parseBillingCharge({ ...emptyCharge, total_usd: exact + '7' }, '/test')
    ).toThrow('Invalid API response')
    for (const total_usd of ['00.1', '01', '-0.1', ' 1', '1.']) {
      expect(() =>
        parseBillingCharge({ ...emptyCharge, total_usd }, '/test')
      ).toThrow('Invalid API response')
    }
    expect(charge.unrounded_quota).toBe(
      '0.617283945061728395061728395061728395061728395061728395061728395'
    )
    expect(charge.rounding_delta).toBe(
      '0.382716054938271604938271604938271604938271604938271604938271605'
    )
  })
})
