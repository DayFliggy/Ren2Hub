import { afterEach, describe, expect, it } from 'vitest'
import { parseTopupDiscountSettings } from '@/api/topupDiscount'
import {
  buildConfig,
  draftFromConfig,
  draftKey,
  type DraftErrors,
  type TopupDiscountDraft,
} from '@/features/topup-discount/draft'
import { QUOTA_PER_USD, configureQuotaPerUnit } from '@/utils/quota'
import configResponse from '../../../../tests/fixtures/native/topup-discount.get.json'

const loaded = parseTopupDiscountSettings(configResponse.data).config

function draft(edit: (draft: TopupDiscountDraft) => void = () => {}) {
  const value = draftFromConfig(loaded)
  edit(value)
  return value
}

function errorsOf(edit: (draft: TopupDiscountDraft) => void): DraftErrors {
  const result = buildConfig(draft(edit))
  if (result.ok) throw new Error('the draft was accepted')
  return result.errors
}

afterEach(() => configureQuotaPerUnit(Number(QUOTA_PER_USD)))

describe('top-up discount draft', () => {
  it('turns the loaded configuration back into the same configuration', () => {
    expect(buildConfig(draft())).toEqual({ ok: true, config: loaded })
    // Percents and USD are what the editor shows.
    expect(draft().cap).toBe('5')
    expect(draft().tables.month.tiers[0]).toMatchObject({
      amount: '25',
      percent: '1',
    })
    expect(draftKey(draft())).toBe(draftKey(draft()))
  })

  it('accepts at most two percent decimals, the four rate decimals the server allows', () => {
    const result = buildConfig(draft((value) => (value.fixed = '1.25')))
    expect(result.ok && result.config.fixed_rate).toBe('0.0125')
    expect(errorsOf((value) => (value.fixed = '1.255')).fixed?.key).toBe(
      'errors.percentInvalid'
    )
    for (const text of ['', '-1', '01', '1e2', 'abc'])
      expect(errorsOf((value) => (value.fixed = text)).fixed?.key, text).toBe(
        'errors.percentInvalid'
      )
  })

  it('caps the cap at 50% and every other rate at the cap', () => {
    expect(buildConfig(draft((value) => (value.cap = '50'))).ok).toBe(true)
    expect(errorsOf((value) => (value.cap = '50.01')).cap).toEqual({
      key: 'errors.capTooHigh',
      params: { max: '50' },
    })
    expect(errorsOf((value) => (value.fixed = '5.01')).fixed).toEqual({
      key: 'errors.overCap',
      params: { max: '5' },
    })
    // The fixture's highest month tier is exactly the 5% cap.
    expect(buildConfig(draft((value) => (value.fixed = '5'))).ok).toBe(true)
    const errors = errorsOf((value) => (value.cap = '4'))
    expect(errors['month.4.percent']?.key).toBe('errors.overCap')
    expect(errors['month.3.percent']).toBeUndefined()
  })

  it('allows at most 10 tiers per table', () => {
    const tiers = (count: number) =>
      Array.from({ length: count }, (_, index) => ({
        amount: String(index + 1),
        percent: '1',
      }))
    expect(
      buildConfig(draft((value) => (value.tables.site.tiers = tiers(10)))).ok
    ).toBe(true)
    expect(
      errorsOf((value) => (value.tables.site.tiers = tiers(11))).site
    ).toEqual({ key: 'errors.tooManyTiers', params: { max: 10 } })
  })

  it('requires strictly increasing thresholds and names the offending row', () => {
    const equal = errorsOf((value) => {
      value.tables.day.tiers[2]!.amount = value.tables.day.tiers[1]!.amount
    })
    expect(equal).toEqual({ 'day.2.amount': { key: 'errors.thresholdOrder' } })
    const lower = errorsOf(
      (value) => (value.tables.week.tiers[1]!.amount = '1')
    )
    expect(lower['week.1.amount']?.key).toBe('errors.thresholdOrder')
    expect(lower['week.0.amount']).toBeUndefined()
  })

  it('requires non-decreasing rates and accepts equal neighbours', () => {
    expect(
      buildConfig(draft((value) => (value.tables.day.tiers[1]!.percent = '1')))
        .ok
    ).toBe(true)
    expect(
      errorsOf((value) => (value.tables.day.tiers[1]!.percent = '0.5'))
    ).toEqual({ 'day.1.percent': { key: 'errors.rateOrder' } })
  })

  it('rejects thresholds that are negative, fractional quota or longer than 18 digits', () => {
    expect(
      errorsOf(
        (value) => (value.tables.site.tiers = [{ amount: '-1', percent: '1' }])
      )['site.0.amount']?.key
    ).toBe('errors.amountInvalid')
    expect(
      errorsOf(
        (value) =>
          (value.tables.site.tiers = [
            { amount: '2000000000000', percent: '1' },
          ])
      )['site.0.amount']?.key
    ).toBe('errors.amountTooLarge')
    configureQuotaPerUnit(300000)
    expect(
      errorsOf(
        (value) =>
          (value.tables.site.tiers = [{ amount: '0.000001', percent: '1' }])
      )['site.0.amount']?.key
    ).toBe('errors.amountFractional')
  })

  it('keeps an untouched loaded threshold exact under a custom quota unit', () => {
    configureQuotaPerUnit(300000)
    // 2500000 quota is $8.333…; the USD text is rounded for display.
    const untouched = buildConfig(draft())
    expect(untouched.ok && untouched.config.personal.day.tiers[0]).toEqual({
      threshold_quota: '2500000',
      rate: '0.01',
    })
    // Typing the rounded text is a new value, and it is not a whole quota.
    const errors = errorsOf((value) => {
      const tier = value.tables.day.tiers[0]!
      value.tables.day.tiers[0] = { amount: tier.amount, percent: tier.percent }
    })
    expect(errors['day.0.amount']?.key).toBe('errors.amountFractional')
  })

  it('accepts an empty table and a table without tiers in any mode', () => {
    const result = buildConfig(
      draft((value) => {
        value.tables.site = { mode: 'on', tiers: [] }
        value.tables.day.tiers = []
      })
    )
    expect(result.ok && result.config.site).toEqual({ mode: 'on', tiers: [] })
  })

  it('reads excluded user ids as unique positive integers, at most 100', () => {
    const result = buildConfig(
      draft((value) => (value.excluded = ' 3, 7\n12 '))
    )
    expect(result.ok && result.config.excluded_user_ids).toEqual([3, 7, 12])
    const none = buildConfig(draft((value) => (value.excluded = '')))
    expect(none.ok && none.config.excluded_user_ids).toEqual([])
    expect(errorsOf((value) => (value.excluded = '1, 0')).excluded).toEqual({
      key: 'errors.excludedInvalid',
      params: { id: '0' },
    })
    expect(errorsOf((value) => (value.excluded = '1, -2')).excluded?.key).toBe(
      'errors.excludedInvalid'
    )
    expect(errorsOf((value) => (value.excluded = '1, 1.5')).excluded?.key).toBe(
      'errors.excludedInvalid'
    )
    expect(errorsOf((value) => (value.excluded = '4 4')).excluded).toEqual({
      key: 'errors.excludedDuplicate',
      params: { id: '4' },
    })
    const ids = (count: number) =>
      Array.from({ length: count }, (_, index) => index + 1).join(',')
    expect(buildConfig(draft((value) => (value.excluded = ids(100)))).ok).toBe(
      true
    )
    expect(errorsOf((value) => (value.excluded = ids(101))).excluded).toEqual({
      key: 'errors.tooManyExcluded',
      params: { max: 100 },
    })
  })
})
