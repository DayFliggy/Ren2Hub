import { describe, expect, it } from 'vitest'
import { SYSTEM_SETTINGS_DOMAINS } from '@/constants/systemSettingsCatalog'
import { validateSetting } from '../settingValidation'

const fields = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
  domain.sections.flatMap((section) => section.fields)
)
function validate(key: string, value: unknown) {
  return validateSetting(
    fields.find((field) => field.key === key)!,
    value
  )
}

describe('payment setting validation', () => {
  it.each([
    'https://example.com/api/user/epay/notify',
    'https://example.com?x=1',
    'https://example.com#fragment',
    'https://user:pass@example.com',
    'https://example.com?',
    'https://example.com#',
    'https://example.com:0',
  ])('rejects callback values that are not public origins: %s', (value) => {
    expect(validate('CustomCallbackAddress', value)).toBe('url')
  })
  it.each(['', 'http://37.114.34.132:13000', 'https://example.com/'])(
    'accepts a callback origin or the site fallback: %s',
    (value) => {
      expect(validate('CustomCallbackAddress', value)).toBeNull()
    }
  )
  it.each([0, -1, Infinity, ''])('rejects unusable EPay price %s', (price) => {
    expect(validate('Price', price)).not.toBeNull()
  })
  it('accepts small positive prices and bounds minimum credits by the ledger limit', () => {
    expect(validate('Price', 1e-8)).toBeNull()
    expect(validate('MinTopUp', 18446744073709)).toBeNull()
    expect(validate('MinTopUp', 18446744073710)).toBe('range')
    expect(validate('MinTopUp', 1.5)).toBe('range')
  })
  it.each([
    { name: 'A', type: 'alipay', min_topup: '1e2' },
    { name: 'A', type: 'alipay', min_topup: '18446744073710' },
    { name: 'A', type: 'bad type' },
    { name: 'A', type: 123 },
  ])('rejects EPay records the server cannot interpret: %s', (method) => {
    expect(validate('PayMethods', JSON.stringify([method]))).not.toBeNull()
  })
  it('supports disabling EPay methods without altering settlement and accepts exact minimum strings', () => {
    expect(validate('PayMethods', '[]')).toBeNull()
    expect(
      validate(
        'PayMethods',
        '[{"name":"Alipay","type":"alipay","min_topup":"100"}]'
      )
    ).toBeNull()
  })
})
