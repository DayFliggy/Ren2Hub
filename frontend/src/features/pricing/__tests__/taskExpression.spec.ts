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

  it('replaces an expression without multipliers', () => {
    for (const source of [
      '',
      'u("seconds") * 0.4',
      'tier("base", u("seconds") * 0.4)',
    ]) {
      expect(repriceTaskExpression(source, price('0.5'), schema)).toBe(
        'tier("base", u("seconds") * 0.5)'
      )
    }
  })
})
