import { describe, expect, it } from 'vitest'
import { expressionDiff, pricingDiff } from '../diff'

describe('pricing source differences', () => {
  it('keeps absence distinct from exact zero and marks removed fields', () => {
    const fields = pricingDiff(
      { ModelPrice: '0', CacheRatio: '0.123456789012345678' },
      { ModelRatio: '0', CacheRatio: '0.123456789012345679' }
    )
    expect(fields.find((field) => field.key === 'ModelPrice')).toMatchObject({
      change: 'removed',
      candidate: null,
      current: [{ text: '0', changed: true }],
    })
    expect(fields.find((field) => field.key === 'ModelRatio')).toMatchObject({
      change: 'added',
      current: null,
      candidate: [{ text: '0', changed: true }],
    })
    expect(
      fields.find((field) => field.key === 'CacheRatio')?.candidate
    ).toEqual([{ text: '0.123456789012345679', changed: true }])
  })
  it('highlights separated expression edits without modifying surrounding source', () => {
    const before = 'tier("first", p * 0.000000000000001 + c * 2)'
    const after = 'tier("second", p * 0.000000000000001 + c * 3)'
    const [left, right] = expressionDiff(before, after)
    expect(left.map((part) => part.text).join('')).toBe(before)
    expect(right.map((part) => part.text).join('')).toBe(after)
    expect(
      left.filter((part) => part.changed).map((part) => part.text)
    ).toEqual(['first', '2'])
    expect(
      right.filter((part) => part.changed).map((part) => part.text)
    ).toEqual(['second', '3'])
  })
  it('compares plugin expressions by plugin and preserves long source exactly', () => {
    const fields = pricingDiff(
      { 'billing_setting.plugin_billing_expr': { a: 'u("seconds")', b: '0' } },
      { 'billing_setting.plugin_billing_expr': { b: '0', c: '1' } }
    )
    expect(fields.map((field) => field.change)).toEqual([
      'removed',
      'unchanged',
      'added',
    ])
    const before = 'u("seconds") + '.repeat(200) + '0'
    const after = 'u("seconds") + '.repeat(200) + '1'
    const [left, right] = expressionDiff(before, after)
    expect(left.map((part) => part.text).join('')).toBe(before)
    expect(right.map((part) => part.text).join('')).toBe(after)
  })
})
