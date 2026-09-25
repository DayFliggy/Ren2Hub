import { describe, expect, it } from 'vitest'
import {
  formatBillingEvidence,
  formatDecimal,
  formatDecimalUSD,
  divideDecimal,
} from '../decimal'
import { decimalToQuota, quotaToDecimal } from '../quota'
import { formatQuota } from '../format'

describe('billing presentation precision', () => {
  it('converts local currency without binary floating point', () => {
    expect(divideDecimal('7.123456789123456789', '7')).toBe(
      '1.017636684160493827'
    )
    expect(divideDecimal('1', '3')).toBe('0.333333333333333333')
    expect(() => divideDecimal('1', '0')).toThrow('Zero exchange rate')
  })
  it('limits read-only money and fee displays to three fractional digits', () => {
    expect(formatDecimalUSD('0.0005', 8)).toBe('$0.001')
    expect(formatDecimalUSD('0.000499999', 8)).toBe('$0.000')
    expect(formatDecimal('-0.0005')).toBe('-0.001')
    expect(formatQuota('9007199254740993', 6)).toBe('$18,014,398,509.482')
  })
  it('rounds an evidence projection without changing saved values or integer quota', () => {
    const evidence = {
      unit_price: '1.123456789',
      quota: '9007199254740993',
      attributes: { duration: 0.123456 },
      expression: 'p * 0.000000123456',
    }
    expect(JSON.parse(formatBillingEvidence(evidence))).toEqual({
      unit_price: '1.123',
      quota: '9007199254740993',
      attributes: { duration: 0.123 },
      expression: 'p * 0.000000123456',
    })
    expect(evidence.unit_price).toBe('1.123456789')
    expect(evidence.attributes.duration).toBe(0.123456)
    expect(quotaToDecimal('9007199254740993')).toBe('18014398509.481986')
    expect(decimalToQuota('18014398509.481986')).toBe('9007199254740993')
  })
  it('preserves constant expressions and numeric-looking identifiers in evidence', () => {
    const evidence = {
      expression: '0.000000123456',
      model: '1.234567',
      id: '2.345678',
      request_id: '3.456789',
      price: '1.234567',
    }
    expect(JSON.parse(formatBillingEvidence(evidence))).toEqual({
      ...evidence,
      price: '1.235',
    })
  })
})
