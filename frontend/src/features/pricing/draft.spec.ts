import { afterEach, describe, expect, it } from 'vitest'
import { configureQuotaPerUnit } from '@/utils/quota'
import {
  fillPriceDraft,
  initialPricingDraft,
  pricingChanges,
  pricingJSON,
  parsePricingJSON,
  tokenPrices,
  tokenPricing,
  validatePricing,
} from './draft'
import { PRICING_KEYS, type ModelPricingConfig } from '@/api/pricingConfig'
import {
  parseVisualBillingDocument,
  serializeVisualBillingDocument,
} from './lib/billing-expression/visual'
import {
  buildRequestRuleExpr,
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from './lib/billing-expr'
const config: ModelPricingConfig = {
  entries: [
    {
      model_name: 'model',
      version: 'v1',
      configured: { ModelRatio: 1, CompletionRatio: 2 },
      effective: { ModelRatio: 1, CompletionRatio: 2 },
    },
  ],
  options: Object.fromEntries(
    PRICING_KEYS.map((key) => [key, '{}'])
  ) as ModelPricingConfig['options'],
  empty_version: 'empty',
}
describe('model pricing draft contracts', () => {
  afterEach(() => configureQuotaPerUnit(500_000))
  it.each([500_000, 1_000_000, 100_000, 125_000.5])(
    'round trips token prices using quota_per_unit=%s',
    (unit) => {
      configureQuotaPerUnit(unit)
      const ratio = String(unit / 100_000)
      const value = { ModelRatio: ratio, CompletionRatio: '2', CacheRatio: '0' }
      expect(tokenPrices(value)).toMatchObject({
        input: '10',
        CompletionRatio: '20',
        CacheRatio: '0',
      })
      expect(tokenPricing(tokenPrices(value))).toEqual(value)
      expect(tokenPrices({})).not.toHaveProperty('CompletionRatio', '0')
      expect(tokenPricing(tokenPrices({ ModelRatio: '0' }))).toEqual({
        ModelRatio: '0',
      })
    }
  )
  it('starts empty and changes only explicitly imported models', () => {
    expect(initialPricingDraft({ ...config, entries: [] })).toEqual({})
    const source = {
      model_name: 'new',
      provider: 'provider',
      pricing: { ModelPrice: 0 },
    }
    const before = initialPricingDraft(config)
    const after = fillPriceDraft(before, [source])
    expect(before.new).toBeUndefined()
    expect(after.new).toEqual({ ModelPrice: 0 })
    expect(pricingChanges(config, after)).toEqual([
      {
        model_name: 'new',
        expected_version: 'empty',
        pricing: { ModelPrice: 0 },
      },
    ])
  })
  it('treats arbitrary model IDs as data without mutating object prototypes', () => {
    const raw = pricingJSON({})
    raw.ModelPrice = '{"__proto__":1,"constructor":0}'
    const result = parsePricingJSON(raw)
    expect(Object.hasOwn(result, '__proto__')).toBe(true)
    expect(Object.hasOwn(result, 'constructor')).toBe(true)
    expect(Object.prototype).not.toHaveProperty('ModelPrice')
  })
  it('clears stored prices without restoring defaults and retains original versions', () => {
    expect(pricingChanges(config, {})).toEqual([
      { model_name: 'model', expected_version: 'v1', pricing: {}, reset: true },
    ])
    expect(pricingChanges(config, initialPricingDraft(config))).toEqual([])
  })
  it('rejects unsupported candidates and conflicting same-name providers atomically', () => {
    const draft = initialPricingDraft(config),
      source = {
        model_name: 'a',
        provider: 'one',
        pricing: { ModelPrice: 1 },
      }
    expect(() =>
      fillPriceDraft(draft, [source, { ...source, provider: 'two' }])
    ).toThrow('duplicate')
    expect(() =>
      fillPriceDraft(draft, [
        source,
        { ...source, model_name: 'b', unsupported_reason: 'tier' },
      ])
    ).toThrow('unsupportedPrice')
    expect(draft).toEqual(initialPricingDraft(config))
  })
  it('round trips JSON as numbers and preserves plugin expression source', () => {
    const plugins = { plugin: 'v2:u("seconds") * 0.123456789012345678' }
    const draft = {
      model: {
        ModelPrice: '0.000000123456789012',
        'billing_setting.plugin_billing_expr': plugins,
      },
    }
    const raw = pricingJSON(draft)
    expect(JSON.parse(raw.ModelPrice)).toEqual({ model: 1.23456789012e-7 })
    expect(parsePricingJSON(raw)).toEqual({
      model: {
        ModelPrice: 1.23456789012e-7,
        'billing_setting.plugin_billing_expr': plugins,
      },
    })
  })
  it('rejects JSON prices the backend would refuse', () => {
    for (const value of ['"1"', '-1', 'null']) {
      const raw = pricingJSON({})
      raw.ModelRatio = `{"model":${value}}`
      expect(() => parsePricingJSON(raw)).toThrow('invalid')
    }
  })
  it('compares draft decimal text with stored numbers by value', () => {
    expect(
      pricingChanges(config, {
        model: { ModelRatio: '1.0', CompletionRatio: '2' },
      })
    ).toEqual([])
    expect(
      pricingChanges(config, {
        model: { ModelRatio: '1.5', CompletionRatio: '2' },
      })
    ).toHaveLength(1)
  })
  it('distinguishes empty expressions from explicit free pricing', () => {
    expect(() =>
      validatePricing({
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': '',
      })
    ).toThrow('exprRequired')
    expect(() =>
      validatePricing({
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': '0',
      })
    ).not.toThrow()
    expect(() => validatePricing({ ModelPrice: '0' })).not.toThrow()
    for (const ModelPrice of ['-1', ' ', '', 'abc', Infinity, -1])
      expect(() => validatePricing({ ModelPrice })).toThrow('invalid')
  })
  it('round trips token dimensions, zero cache, and dependent audio prices', () => {
    const value = {
      ModelRatio: '2',
      CompletionRatio: '3',
      CacheRatio: '0',
      AudioRatio: '2',
      AudioCompletionRatio: '4',
    }
    expect(tokenPrices(value)).toMatchObject({
      input: '4',
      CompletionRatio: '12',
      CacheRatio: '0',
      AudioRatio: '8',
      AudioCompletionRatio: '32',
    })
    expect(tokenPricing(tokenPrices(value))).toEqual(value)
    expect(() => tokenPricing({ input: '0', CompletionRatio: '1' })).toThrow(
      'dependency'
    )
  })
  it('preserves upstream visual tiers and request conditions', () => {
    const source =
      'len <= 200000 ? tier("normal", p * 3 + c * 15) : tier("long", p * 6 + c * 22.5)'
    const document = parseVisualBillingDocument(source)
    expect(document).not.toBeNull()
    expect(serializeVisualBillingDocument(document!)).toEqual({
      ok: true,
      source,
    })
    const rules = buildRequestRuleExpr([
      {
        conditions: [
          {
            source: 'param',
            path: 'service_tier',
            mode: 'eq',
            value: 'priority',
          },
          { source: 'header', path: 'x-plan', mode: 'eq', value: 'pro' },
        ],
        multiplier: '2',
      },
    ])
    expect(rules).toContain('param(')
    expect(rules).toContain('header(')
    expect(
      splitBillingExprAndRequestRules(combineBillingExpr(source, rules))
        .billingExpr
    ).toContain('len <= 200000')
  })
})
