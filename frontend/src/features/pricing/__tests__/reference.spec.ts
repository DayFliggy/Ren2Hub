import { describe, expect, it } from 'vitest'
import {
  expressionReferencePrices,
  publicPricingReferences,
  compareReferencePrices,
} from '../reference'
import { parsePricingModels } from '@/api/liveContracts'
import { PRICE_SORTS, type ReferencePrice } from '@/types/pricingReference'

const amounts = (prices: ReferencePrice[]) =>
  Object.fromEntries(prices.map((price) => [price.key, price.amount]))

describe('catalog reference prices', () => {
  it('reads exact first-tier literals, preserving conditions, zero and missing dimensions', () => {
    const expr =
      'len <= 1000 ? tier("first", p * 0.123456789012345678901 + c * 0) : tier("later", p * 9 + c * 8)'
    const prices = expressionReferencePrices(expr, '0.2')
    expect(amounts(prices)).toEqual({
      input: '0.0246913578024691357802',
      output: '0',
    })
    expect(prices[0]).toMatchObject({
      source: 'tier',
      label: 'first',
      condition: 'len <= 1000',
    })
    expect(
      amounts(
        expressionReferencePrices(
          'len < 10 ? tier("output", c * 4) : tier("input", p * 2)'
        )
      )
    ).toEqual({ output: '4' })
    expect(
      amounts(
        expressionReferencePrices(
          'tier("scientific", p * 1.234567890123456789e-4)'
        )
      )
    ).toEqual({ input: '0.0001234567890123456789' })
  })
  it('retains per-image units and exposes request rules without executing them', () => {
    const result = expressionReferencePrices(
      '(tier("image", fixed(0.01234567890123456789)) * image_count) * (header("quality") == "hd" ? 2 : 1)'
    )
    expect(result).toMatchObject([
      {
        key: 'request',
        amount: '0.01234567890123456789',
        unit: 'per_image',
        requestRules: expect.stringContaining('header'),
      },
    ])
    const time = expressionReferencePrices(
      'hour("UTC") < 12 ? tier("morning", p * 2) : tier("night", p * 1)'
    )
    expect(time[0]).toMatchObject({
      amount: '2',
      label: 'morning',
      condition: expect.stringContaining('hour("UTC") < 12'),
    })
  })
  it.each([
    'p * param("price")',
    'tier("dynamic", p * hour("UTC"))',
    'header("tier") == "x" ? tier("x",p * 1) : tier("y",p * 2)',
    'tier("bad", p * 1e-999)',
    'tier("negative underflow", p * -1e-999)',
  ])('does not invent a unit price for %s', (expression) => {
    expect(expressionReferencePrices(expression)).toEqual([])
  })

  it.each(PRICE_SORTS.filter((sort) => sort !== 'default' && sort !== 'name'))(
    'sorts zero and exact values in %s with absent prices always last',
    (sort) => {
      const key = sort.startsWith('call')
        ? 'request'
        : sort.startsWith('output')
          ? 'output'
          : 'input'
      const price = (amount: string): ReferencePrice[] => [
        {
          key,
          amount,
          unit: key === 'request' ? 'per_call' : 'per_million_tokens',
          source: 'configured',
        },
      ]
      const rows = [
        { id: 'none', prices: [] },
        { id: 'a', prices: price('1.0000000000000000001') },
        { id: 'b', prices: price('1.0000000000000000002') },
        { id: 'zero', prices: price('0') },
      ]
      rows.sort((a, b) => compareReferencePrices(a.prices, b.prices, sort))
      expect(rows.map((row) => row.id)).toEqual(
        sort.endsWith('desc')
          ? ['b', 'a', 'zero', 'none']
          : ['zero', 'a', 'b', 'none']
      )
    }
  )

  it('keeps a missing native first-tier dimension absent in both sort directions', () => {
    const [missing, priced] = parsePricingModels([
      {
        model_name: 'first-output-only',
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr:
          'len < 10 ? tier("first", c * 4) : tier("later", p * 0.1 + c * 9)',
      },
      {
        model_name: 'native',
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr: 'tier("native", p * 2 + c * 3)',
      },
    ])
    expect(amounts(publicPricingReferences(missing!))).toEqual({ output: '4' })
    for (const sort of ['input-asc', 'input-desc'] as const) {
      const rows = [missing!, priced!].sort((a, b) =>
        compareReferencePrices(
          publicPricingReferences(a),
          publicPricingReferences(b),
          sort
        )
      )
      expect(rows.map((row) => row.model_name)).toEqual([
        'native',
        'first-output-only',
      ])
    }
  })

  it('uses native numeric quota prices and applies the selected group once', () => {
    const [model] = parsePricingModels([
      {
        model_name: 'native',
        quota_type: 0,
        model_ratio: 0.5,
        completion_ratio: 3,
        cache_ratio: 0,
      },
    ])
    expect(amounts(publicPricingReferences(model!))).toEqual({
      input: '1',
      output: '3',
      cacheRead: '0',
    })
    expect(amounts(publicPricingReferences(model!, 0.2))).toEqual({
      input: '0.2',
      output: '0.6',
      cacheRead: '0',
    })
    expect(model!.model_ratio).toBe(0.5)
    const [call] = parsePricingModels([
      { model_name: 'image', quota_type: 1, model_price: 0.04 },
    ])
    expect(amounts(publicPricingReferences(call!, 2))).toEqual({
      request: '0.08',
    })
  })
  it('does not invent a task sample charge or reuse another plugin formula', () => {
    const [model] = parsePricingModels([
      {
        model_name: 'video',
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr: 'u("seconds") * 0.2',
        billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        billing_plugin_variants: [
          {
            plugin_key: 'other',
            plugin_name: 'Other',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("seconds") * 0.1',
          },
        ],
      },
    ])
    expect(publicPricingReferences(model!)).toEqual([])
  })
  it('rejects numeric strings outside the native public pricing contract', () => {
    expect(() =>
      parsePricingModels([
        { model_name: 'bad', quota_type: 0, model_ratio: '1' },
      ])
    ).toThrow()
  })
})
