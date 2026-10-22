import { describe, expect, it } from 'vitest'

import { parseTopupInfo } from '@/api/topup'
import { nativeTopupInfo } from '@/api/__tests__/topupFixtures'
import { topupInputAmounts, validTopupAmount } from '@/utils/topupAmount'

// Methods as controller/topup.go sends them: admin PayMethods rows keep
// min_topup as text, the built-in Waffo rows carry their provider minimum and
// GMPay assets omit the field.
const info = parseTopupInfo({
  ...nativeTopupInfo,
  enable_waffo_topup: true,
  enable_waffo_pancake_topup: true,
  waffo_min_topup: 2,
  waffo_pancake_min_topup: 3,
  pay_methods: [
    { name: 'Alipay', type: 'alipay', min_topup: '10' },
    { name: 'WeChat', type: 'wxpay', min_topup: '' },
    { name: 'QQ', type: 'qqpay', min_topup: '0' },
    { name: 'Bank', type: 'bank' },
    { name: 'Waffo', type: 'waffo', min_topup: '5' },
    { name: 'Waffo Pancake', type: 'waffo_pancake', min_topup: '3' },
    { name: 'USDT Tron', type: 'gmpay:usdt.tron', icon: '', color: '' },
  ],
})
const usd = { quota_display_type: 'USD', quota_per_unit: 500000 }
const tokens = { quota_display_type: 'TOKENS', quota_per_unit: 500000 }

describe('top-up minimum per payment method', () => {
  it('raises the global minimum to the selected method minimum', () => {
    expect(topupInputAmounts(info, 'alipay', usd)?.minimum).toBe(10)
    expect(topupInputAmounts(info, 'wxpay', usd)?.minimum).toBe(1)
    const alipayTokens = topupInputAmounts(info, 'alipay', tokens)
    expect(alipayTokens?.minimum).toBe(5000000)
    expect(validTopupAmount(4500000, alipayTokens)).toBe(false)
    expect(validTopupAmount(5000000, alipayTokens)).toBe(true)
    expect(topupInputAmounts(info, 'gmpay:usdt.tron', tokens)?.minimum).toBe(
      500000
    )
  })

  it('treats a missing, blank or zero method minimum as none', () => {
    for (const method of ['wxpay', 'qqpay', 'bank', 'gmpay:usdt.tron'])
      expect(topupInputAmounts(info, method, usd)?.minimum).toBe(1)
    expect(
      topupInputAmounts({ ...info, min_topup: 4 }, 'bank', usd)?.minimum
    ).toBe(4)
    expect(
      topupInputAmounts({ ...info, min_topup: 20 }, 'alipay', usd)?.minimum
    ).toBe(20)
  })

  it('keeps Waffo and Pancake minimums unscaled and rounds token input to whole units', () => {
    expect(topupInputAmounts(info, 'waffo', usd)?.minimum).toBe(5)
    expect(topupInputAmounts(info, 'waffo_pancake', usd)?.minimum).toBe(3)
    expect(topupInputAmounts(info, 'waffo', tokens)?.minimum).toBe(500000)
    const raised = { ...info, waffo_min_topup: 600000 }
    expect(topupInputAmounts(raised, 'waffo', tokens)?.minimum).toBe(1000000)
  })

  it.each(['1e2', '-1', '10.5', 'ten', 10, null])(
    'rejects the non-empty invalid method minimum %j',
    (value) => {
      expect(() =>
        parseTopupInfo({
          ...nativeTopupInfo,
          pay_methods: [{ name: 'Alipay', type: 'alipay', min_topup: value }],
        })
      ).toThrow('Invalid API response')
    }
  )
})
