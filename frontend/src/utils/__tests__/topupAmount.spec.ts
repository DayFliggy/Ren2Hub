import { expect, it } from 'vitest'
import type { TopupInfo } from '@/api/topup'
import { topupInputAmounts, validTopupAmount } from '../topupAmount'
const info = {
  min_topup: 2,
  stripe_min_topup: 3,
  waffo_min_topup: 700000,
  waffo_pancake_min_topup: 1000000,
  amount_options: [10, 20],
} as TopupInfo
it.each(['USD', 'CNY', 'CUSTOM'])(
  'keeps native recharge quantities unchanged in %s',
  (quota_display_type) => {
    const value = topupInputAmounts(info, 'alipay', {
      quota_display_type,
      quota_per_unit: 500000,
    })
    expect(value).toMatchObject({ minimum: 2, step: 1, presets: [10, 20] })
    expect(validTopupAmount(1, value)).toBe(false)
    expect(validTopupAmount(2, value)).toBe(true)
  }
)
it('projects token presets once and keeps each provider minimum before aligning whole units', () => {
  const status = { quota_display_type: 'TOKENS', quota_per_unit: 500000 }
  expect(topupInputAmounts(info, 'alipay', status)).toMatchObject({
    minimum: 1000000,
    presets: [5000000, 10000000],
    step: 500000,
  })
  expect(topupInputAmounts(info, 'gmpay:usdt.tron', status)?.minimum).toBe(
    1000000
  )
  expect(topupInputAmounts(info, 'waffo', status)?.minimum).toBe(1000000)
  expect(topupInputAmounts(info, 'waffo_pancake', status)?.minimum).toBe(
    1000000
  )
  expect(
    validTopupAmount(1250000, topupInputAmounts(info, 'alipay', status))
  ).toBe(false)
})
it('refuses invalid or overflowing token unit configuration', () => {
  expect(
    topupInputAmounts(info, 'alipay', { quota_display_type: 'TOKENS' })
  ).toBeNull()
  expect(
    topupInputAmounts(info, 'alipay', {
      quota_display_type: 'TOKENS',
      quota_per_unit: 1e15,
    })
  ).toBeNull()
})
