import { describe, expect, it } from 'vitest'
import { computed } from 'vue'
import { formatQuota } from '../format'
import {
  addQuota,
  configureQuotaPerUnit,
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

  it('uses the quota unit published by the backend for both directions', () => {
    expect(quotaToDecimal('1234567', 6, 1_000_000)).toBe('1.234567')
    expect(decimalToQuota('1.234567', 1_000_000)).toBe('1234567')
    expect(quotaToDecimal('333333', 6, 333333)).toBe('1.000000')
    expect(decimalToQuota('1', 333333)).toBe('333333')
    expect(quotaToDecimal('5', 6, 1.25)).toBe('4.000000')
    expect(decimalToQuota('4', 1.25)).toBe('5')
    expect(decimalToQuota('10000000', 1e-7)).toBe('1')
    expect(decimalToQuota('0.000001', 1e21)).toBe('1000000000000000')
    expect(() => decimalToQuota('1', 1.25)).toThrow('Fractional quota')
  })

  it('updates default input conversion when public status changes quota units', () => {
    const submittedQuota = computed(() => decimalToQuota('1.25'))
    expect(submittedQuota.value).toBe('625000')
    configureQuotaPerUnit(1_000_000)
    expect(submittedQuota.value).toBe('1250000')
    configureQuotaPerUnit(500_000)
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
      if (value !== 1)
        expect(() => requiredQuota(value, '/billing')).toThrow(
          'Invalid API response'
        )
    }
    expect(requiredQuota(1, '/native')).toBe('1')
    expect(() => requiredQuota('1', '/native')).toThrow()
    expect(() => addQuota('9223372036854775807', '1')).toThrow('overflow')
    expect(() => decimalToQuota('0.000001')).toThrow('Fractional quota')
    expect(() => decimalToQuota('18446744073709.551616')).toThrow('overflow')
    expect(() => formatQuota(Number.MAX_SAFE_INTEGER + 1)).toThrow(
      'Invalid quota'
    )
  })
})
