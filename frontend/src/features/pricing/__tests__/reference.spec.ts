import { describe, expect, it } from 'vitest'
import {
  expressionReferencePrices,
  publicPricingReferences,
  compareReferencePrices,
} from '../reference'
import { parsePricingModels } from '@/api/liveContracts'
import { parseUsageReference } from '@/api/pricingReference'
import { PRICE_SORTS, type ReferencePrice } from '@/types/pricingReference'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { createModelMarketFixture } from '../../../../tests/fixtures/modelMarket'

const amounts = (prices: ReferencePrice[]) =>
  Object.fromEntries(prices.map((price) => [price.key, price.amount]))

describe('catalog reference prices', () => {
  it('reads exact first-tier literals, preserving conditions, zero and missing dimensions', () => {
    const expr =
      'len <= 1000 ? tier("first", p * 0.123456789012345678901 + c * 0) : tier("later", p * 9 + c * 8)'
    const prices = expressionReferencePrices(expr, false, undefined, '0.2')
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
          'len < 10 ? tier("output", c * 4) : tier("input", p * 2)',
          false
        )
      )
    ).toEqual({ output: '4' })
    expect(
      amounts(
        expressionReferencePrices(
          'tier("scientific", p * 1.234567890123456789e-4)',
          false
        )
      )
    ).toEqual({ input: '0.0001234567890123456789' })
  })
  it('retains per-image units and exposes request rules without executing them', () => {
    const result = expressionReferencePrices(
      '(tier("image", fixed(0.01234567890123456789)) * image_count) * (header("quality") == "hd" ? 2 : 1)',
      false
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
      'hour("UTC") < 12 ? tier("morning", p * 2) : tier("night", p * 1)',
      false
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
    expect(expressionReferencePrices(expression, false)).toEqual([])
  })
  it('applies the channel multiplier once to the server first-example result', () => {
    const fixture = createModelMarketFixture()
    const raw = fixture.models.find((model) => model.id === 'video-preview')!
      .baselines[0]!
    raw.usage_reference = {
      status: 'available',
      amount_usd: '0.1234567890123456789',
      label: 'first 5s',
      facts: { seconds: '5' },
      plugin_key: 'seconds',
      plugin_name: 'Seconds',
    }
    const model = buildModelMarketCatalog(fixture).models.find(
      (model) => model.id === 'video-preview'
    )!
    const offer = model.offers.find(
      (offer) => offer.pricing.pluginKey === 'seconds'
    )!
    expect(offer.baseline.referencePrices?.[0]?.amount).toBe(
      '0.1234567890123456789'
    )
    expect(offer.pricing.referencePrices?.[0]).toMatchObject({
      amount: '0.02469135780246913578',
      unit: 'per_example',
      label: 'first 5s',
      pluginName: 'Seconds',
    })
    expect(raw.usage_reference.amount_usd).toBe('0.1234567890123456789')
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
  it('uses exact public source values and does not borrow another plugin example', () => {
    const [price] = parsePricingModels([
      {
        model_name: 'test',
        quota_type: 0,
        billing_mode: 'ratio',
        model_ratio: 1,
        pricing_values: {
          ModelRatio: '0.123456789012345678901',
          CompletionRatio: '2',
        },
      },
    ])
    expect(amounts(publicPricingReferences(price!))).toEqual({
      input: '0.246913578024691357802',
      output: '0.493827156049382715604',
    })
    const [task] = parsePricingModels([
      {
        model_name: 'task',
        quota_type: 0,
        billing_mode: 'unconfigured',
        pricing_values: {},
        billing_plugin_variants: [
          {
            plugin_key: 'alpha',
            plugin_name: 'Alpha',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("seconds")',
            usage_reference: {
              status: 'unavailable',
              reason: 'missing_example',
            },
          },
          {
            plugin_key: 'beta',
            plugin_name: 'Beta',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("seconds")',
            usage_reference: {
              status: 'available',
              amount_usd: '1',
              label: 'beta first',
              facts: { seconds: '1' },
            },
          },
        ],
      },
    ])
    expect(publicPricingReferences(task!)).toEqual([])
    task!.billing_plugin_variants![0]!.usage_reference = {
      status: 'available',
      amount_usd: '2',
      label: 'alpha first',
      facts: { seconds: '2' },
      plugin_name: 'Alpha',
    }
    expect(publicPricingReferences(task!)).toMatchObject([
      { amount: '2', pluginName: 'Alpha', label: 'alpha first' },
    ])
  })
  it('keeps one configured token price alongside the first compatible task example without minimizing plugin totals', () => {
    const [mixed] = parsePricingModels([
      {
        model_name: 'mixed',
        quota_type: 0,
        billing_mode: 'ratio',
        model_ratio: 0.5,
        completion_ratio: 3,
        pricing_values: { ModelRatio: '0.5', CompletionRatio: '3' },
        billing_plugin_variants: [
          {
            plugin_key: 'incompatible',
            plugin_name: 'Incompatible',
            billing_mode: 'unconfigured',
            billing_expr: 'u("absent")',
          },
          {
            plugin_key: 'inherited',
            plugin_name: 'Inherited',
            billing_mode: 'ratio',
            billing_expr: '',
          },
          {
            plugin_key: 'selected',
            plugin_name: 'Selected',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("seconds")',
            usage_reference: {
              status: 'available',
              amount_usd: '0.7',
              label: 'first 7s',
              facts: { seconds: '7' },
            },
          },
          {
            plugin_key: 'cheaper',
            plugin_name: 'Cheaper',
            billing_mode: 'tiered_expr',
            billing_expr: 'u("tokens")',
            usage_reference: {
              status: 'available',
              amount_usd: '0.1',
              label: 'first 10 tokens',
              facts: { tokens: '10' },
            },
          },
        ],
      },
    ])
    const prices = publicPricingReferences(mixed!)
    expect(amounts(prices)).toEqual({ input: '1', output: '3', request: '0.7' })
    expect(prices[0]).toMatchObject({
      source: 'configured',
      unit: 'per_million_tokens',
    })
    expect(prices[2]).toMatchObject({
      source: 'usage_example',
      unit: 'per_example',
      pluginName: 'Selected',
      label: 'first 7s',
    })
    const other = prices.map((price) => ({
      ...price,
      amount: { input: '2', output: '1', request: '0.5' }[price.key]!,
    }))
    for (const sort of PRICE_SORTS.filter(
      (sort) => sort !== 'default' && sort !== 'name'
    )) {
      const rows = [
        { id: 'mixed', prices },
        { id: 'other', prices: other },
      ].sort((a, b) => compareReferencePrices(a.prices, b.prices, sort))
      const mixedFirst =
        sort === 'input-asc' || sort === 'output-desc' || sort === 'call-desc'
      expect(rows.map((row) => row.id)).toEqual(
        mixedFirst ? ['mixed', 'other'] : ['other', 'mixed']
      )
      expect(
        rows
          .find((row) => row.id === 'mixed')!
          .prices.find((price) => price.key === 'request')!.amount
      ).toBe('0.7')
    }
    mixed!.billing_plugin_variants![2]!.usage_reference = {
      status: 'unavailable',
      reason: 'missing_example',
    }
    expect(amounts(publicPricingReferences(mixed!))).toEqual({
      input: '1',
      output: '3',
    })
  })
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
  it('rejects malformed reference values and keeps old catalog display data without inventing an exact sort key', () => {
    for (const value of [
      null,
      { status: 'available', amount_usd: 1, label: 'x', facts: {} },
      { status: 'unavailable', reason: 'missing_example', amount_usd: '0' },
      { status: 'available', amount_usd: '0', label: 'x', facts: { count: 1 } },
    ])
      expect(() => parseUsageReference(value, 'test')).toThrow()
    const [price] = parsePricingModels([
      { model_name: 'old', quota_type: 0, model_ratio: 1 },
    ])
    expect(publicPricingReferences(price!)).toEqual([])
  })
})
