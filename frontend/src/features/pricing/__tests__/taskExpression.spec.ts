import { describe, expect, it } from 'vitest'
import {
  readTaskPrices,
  repriceTaskExpression,
  taskConditionExpression,
} from '../taskExpression'
import type { BillingUsageSchema } from '../types'

const schema: BillingUsageSchema = {
  seconds: { type: 'number', unit: 'second' },
}
const price = (value: string) => ({ seconds: value })

describe('repricing a task expression', () => {
  it('keeps a request rule multiplier', () => {
    const rule = '(header("speed") == "fast" ? 3 : 1)'
    const source = `(tier("base", u("seconds") * 0.4)) * ${rule}`
    expect(readTaskPrices(source, schema)).toEqual(price('0.4'))
    expect(repriceTaskExpression(source, price('0.5'), schema)).toBe(
      `(tier("base", u("seconds") * 0.5)) * ${rule}`
    )
  })

  it('keeps applied task conditions and the version prefix', () => {
    const once = taskConditionExpression(
      'v1:tier("base", u("seconds") * 0.4)',
      [{ field: 'seconds', operator: '>', value: '10' }],
      '2',
      schema
    )
    const twice = taskConditionExpression(
      once,
      [{ field: 'seconds', operator: '<', value: '60' }],
      '0.5',
      schema
    )
    expect(readTaskPrices(twice, schema)).toEqual(price('0.4'))
    expect(repriceTaskExpression(twice, price('0.5'), schema)).toBe(
      'v1:(tier("base", u("seconds") * 0.5)) * ((u("seconds") > 10) ? 2 : 1) * ((u("seconds") < 60) ? 0.5 : 1)'
    )
  })

  it('replaces a single-tier, blank or literal expression and keeps its prefix', () => {
    const cases: [string, string][] = [
      ['', 'tier("base", u("seconds") * 0.5)'],
      ['0', 'tier("base", u("seconds") * 0.5)'],
      ['tier("base", u("seconds") * 0.4)', 'tier("base", u("seconds") * 0.5)'],
      [
        'v1:tier("base", u("seconds") * 0.4)',
        'v1:tier("base", u("seconds") * 0.5)',
      ],
    ]
    for (const [source, expected] of cases)
      expect(repriceTaskExpression(source, price('0.5'), schema)).toBe(expected)
  })

  // Rebuilding these would drop a quantity, a division, a nested multiplier or
  // another tier, so a matching request would be billed less.
  it('refuses shapes it cannot rebuild without losing a term', () => {
    const base = 'tier("base", u("seconds") * 0.4)'
    const rule = '(header("speed") == "fast" ? 3 : 1)'
    for (const source of [
      `(${base} * ${rule}) / 1`,
      `${base} * (header("speed") == "fast" ? 3 * 2 : 1)`,
      `${base} * image_count * ${rule}`,
      `(u("seconds") > 10 ? tier("long", u("seconds") * 0.8) : ${base}) * ${rule}`,
      'u("seconds") * 0.4',
      'tier("base", u("seconds") * 0.4 + 1)',
      'not an expression (',
    ]) {
      expect(readTaskPrices(source, schema), source).toEqual({})
      expect(
        () => repriceTaskExpression(source, price('0.5'), schema),
        source
      ).toThrow('taskPriceUnsupported')
    }
  })
})

describe('provider and task price inheritance', () => {
  it('keeps chat counters and relay constants out of inherited task prices', async () => {
    const { taskExpressionCompatible } = await import('../taskExpression')
    const schema = {
      seconds: { type: 'number' as const, unit: 'second' as const },
    }
    expect(taskExpressionCompatible('p * 2 + c * 4', schema)).toBe(false)
    expect(taskExpressionCompatible('image_count * 2', schema)).toBe(false)
    expect(taskExpressionCompatible('tier("base", 2)', schema, true)).toBe(
      false
    )
    expect(
      taskExpressionCompatible('tier("base", u("seconds") * 2)', schema, true)
    ).toBe(true)
    expect(taskExpressionCompatible('u("missing")', schema)).toBe(false)
  })
})
