import { describe, expect, it } from 'vitest'
import { adjustPrice } from '../priceAdjustment'

// Ported from upstream pricing-sync.test.tsx "sync price adjustments" with
// the local pricing keys.
describe('exchange-rate price adjustment', () => {
  it('keeps source prices by default and scales only the base amount of legacy prices', () => {
    const source = {
      ModelRatio: 3.5,
      CompletionRatio: 4,
      CacheRatio: 0,
      AudioRatio: 2,
      AudioCompletionRatio: 3,
    }
    expect(adjustPrice(source, 'keep', null)).toEqual({ values: source })
    expect(adjustPrice(source, 'divide', 7)).toEqual({
      values: { ...source, ModelRatio: 0.5 },
    })
    expect(adjustPrice(source, 'multiply', 7)).toEqual({
      values: { ...source, ModelRatio: 24.5 },
    })
    expect(source.ModelRatio).toBe(3.5)
    expect(adjustPrice({ ModelPrice: 0 }, 'divide', 7)).toEqual({
      values: { ModelPrice: 0 },
    })
    expect(adjustPrice({ ModelPrice: '14' }, 'divide', 7)).toEqual({
      values: { ModelPrice: 2 },
    })
    expect(adjustPrice({ ModelPrice: '1' }, 'divide', 3)).toEqual({
      values: { ModelPrice: '0.333333333333333333' },
    })
  })

  it('preserves tier thresholds, explicit zero prices, version prefixes and request multipliers', () => {
    const source =
      'v1:(len <= 128000 ? tier("short", p * 7 + c * 21 + cr * 0) : tier("long", fixed(0.7))) * (header("speed") == "fast" ? 3 : 1)'
    expect(
      adjustPrice(
        {
          'billing_setting.billing_mode': 'tiered_expr',
          'billing_setting.billing_expr': source,
        },
        'divide',
        7
      )
    ).toEqual({
      values: {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr':
          'v1:(len <= 128000 ? tier("short", p * 1 + c * 3 + cr * 0) : tier("long", fixed(0.1))) * (header("speed") == "fast" ? 3 : 1)',
      },
    })
  })

  it('scales per-image fixed prices without touching image_count', () => {
    const values = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr':
        'tier("images", fixed(0.28)) * image_count',
    }
    expect(adjustPrice(values, 'divide', 7)).toEqual({
      values: {
        ...values,
        'billing_setting.billing_expr':
          'tier("images", fixed(0.04)) * image_count',
      },
    })
  })

  it('rejects unsupported expressions, invalid rates and overflow without changing source prices', () => {
    for (const rate of [null, 0, -1, Number.NaN, Infinity]) {
      expect(adjustPrice({ ModelPrice: 1 }, 'divide', rate)).toEqual({
        error: 'rate',
      })
    }
    for (const expr of [
      'max(p * 7, 10)',
      'tier("x", p * 7) + 5',
      'tier("x", p * 7) * max(1, param("factor"))',
      'tier("x", p * 7) > 1 ? tier("a", p) : tier("b", p)',
    ]) {
      expect(
        adjustPrice(
          {
            'billing_setting.billing_mode': 'tiered_expr',
            'billing_setting.billing_expr': expr,
          },
          'divide',
          7
        )
      ).toEqual({ error: 'unsupported' })
    }
    expect(
      adjustPrice({ ModelPrice: Number.MAX_VALUE }, 'multiply', 7)
    ).toEqual({ error: 'range' })
    expect(adjustPrice({ ModelPrice: Number.MIN_VALUE }, 'divide', 7)).toEqual({
      error: 'range',
    })
    expect(adjustPrice({ CompletionRatio: 2 }, 'divide', 7)).toEqual({
      error: 'unset',
    })
  })
})
