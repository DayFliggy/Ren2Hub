import { describe, expect, it } from 'vitest'
import { formatQuota } from '../format'
import {
  addQuota,
  decimalToQuota,
  isQuota,
  quotaToDecimal,
  sumQuota,
} from '../quota'
import { requiredQuota } from '@/api/contracts'

describe('exact quota contracts', () => {
  it('preserves int64 balances beyond JavaScript integer precision', () => {
    expect(addQuota('9007199254740993', '1')).toBe('9007199254740994')
    expect(sumQuota(['9007199254740993', '-9007199254740992'])).toBe('1')
    expect(quotaToDecimal('9223372036854775807')).toBe('18446744073709.551614')
    expect(decimalToQuota('18446744073709.551614')).toBe('9223372036854775807')
    expect(formatQuota('9007199254740993', 6)).toBe('$18,014,398,509.482')
  })

  it('formats debt and rounds display amounts without altering stored quota', () => {
    expect(formatQuota('-2500')).toBe('$-0.01')
    expect(formatQuota('2499')).toBe('$0.00')
    expect(formatQuota('2500')).toBe('$0.01')
    expect(quotaToDecimal('1')).toBe('0.000002')
    expect(decimalToQuota('-0.000002')).toBe('-1')
  })

  it('rejects noncanonical wire amounts, overflow and fractional quota input', () => {
    for (const value of [
      1,
      null,
      '01',
      '-0',
      '+1',
      '1.0',
      '1e3',
      '9223372036854775808',
    ]) {
      expect(isQuota(value)).toBe(false)
      expect(() => requiredQuota(value, '/billing')).toThrow(
        'Invalid API response'
      )
    }
    expect(() => addQuota('9223372036854775807', '1')).toThrow('overflow')
    expect(() => decimalToQuota('0.000001')).toThrow('Fractional quota')
    expect(() => decimalToQuota('18446744073709.551616')).toThrow('overflow')
    expect(() => formatQuota(Number.MAX_SAFE_INTEGER + 1)).toThrow(
      'Invalid quota'
    )
  })
})
