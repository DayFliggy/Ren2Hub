import { describe, expect, it } from 'vitest'
import {
  formatDecimal,
  formatDecimalUSD,
  formatUnitPriceUSD,
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
  it('marks nonzero unit prices below one thousandth without displaying extra digits', () => {
    expect(formatUnitPriceUSD('0')).toBe('$0.000')
    expect(formatUnitPriceUSD('0.0009999')).toBe('<$0.001')
    expect(formatUnitPriceUSD('0.001')).toBe('$0.001')
  })
  it('round-trips integer quota beyond the JavaScript safe integer range', () => {
    expect(quotaToDecimal('9007199254740993')).toBe('18014398509.481986')
    expect(decimalToQuota('18014398509.481986')).toBe('9007199254740993')
  })
})
