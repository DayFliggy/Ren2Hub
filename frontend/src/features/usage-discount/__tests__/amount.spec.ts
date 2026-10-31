import { afterEach, describe, expect, it } from 'vitest'
import {
  amountToQuota,
  percentToRate,
  quotaToAmount,
  rateToPercent,
} from '@/features/usage-discount/amount'
import { QUOTA_PER_USD, configureQuotaPerUnit } from '@/utils/quota'

afterEach(() => configureQuotaPerUnit(Number(QUOTA_PER_USD)))

describe('usage discount amounts', () => {
  it('converts $25 and the 12500000 quota threshold both ways', () => {
    expect(amountToQuota('25')).toBe('12500000')
    expect(quotaToAmount('12500000')).toBe('25')
    expect(amountToQuota('0.5')).toBe('250000')
    expect(quotaToAmount('250000')).toBe('0.5')
    expect(amountToQuota('0')).toBe('0')
    expect(quotaToAmount('0')).toBe('0')
  })

  it('keeps thresholds beyond Number.MAX_SAFE_INTEGER exact', () => {
    // 18 nines is the largest threshold the server accepts.
    const largest = '999999999999999999'
    expect(BigInt(largest)).toBeGreaterThan(BigInt(Number.MAX_SAFE_INTEGER))
    expect(quotaToAmount(largest)).toBe('1999999999999.999998')
    expect(amountToQuota('1999999999999.999998')).toBe(largest)
    // 2^53 + 1: a JSON number or float conversion would read 2^53.
    const unsafe = '9007199254740993'
    expect(Number(unsafe).toString()).not.toBe(unsafe)
    expect(quotaToAmount(unsafe)).toBe('18014398509.481986')
    expect(amountToQuota('18014398509.481986')).toBe(unsafe)
  })

  it('rejects an amount that is not a whole quota under the site unit', () => {
    configureQuotaPerUnit(300000)
    expect(() => amountToQuota('0.000001')).toThrow('errors.amountFractional')
    expect(amountToQuota('10')).toBe('3000000')
  })

  it('rejects malformed and oversized amounts', () => {
    for (const text of ['', '-1', '1e3', '1.', '.5', '1.0000001', 'abc'])
      expect(() => amountToQuota(text), text).toThrow('errors.amountInvalid')
    // $2 trillion is 10^18 quota: 19 digits.
    expect(() => amountToQuota('2000000000000')).toThrow(
      'errors.amountTooLarge'
    )
  })

  it('shows rates as exact percents', () => {
    expect(rateToPercent('0.05')).toBe('5')
    expect(rateToPercent('0.0125')).toBe('1.25')
    expect(rateToPercent('0')).toBe('0')
    expect(percentToRate('1.25')).toBe('0.0125')
    expect(percentToRate('50')).toBe('0.5')
  })
})
