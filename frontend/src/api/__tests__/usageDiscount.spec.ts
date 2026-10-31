import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import {
  isUsageDiscountConflict,
  parseUsageDiscountEstimate,
  parseUsageDiscountSelf,
  parseUsageDiscountSettings,
  parseUsageDiscountStatus,
  usageDiscountApi,
} from '@/api/usageDiscount'
import { buildConfig, draftFromConfig } from '@/features/usage-discount/draft'
// Generated from the real handlers by controller/frontend_contract_test.go.
import configResponse from '../../../tests/fixtures/native/usage-discount.get.json'
import patchRequest from '../../../tests/fixtures/native/usage-discount.patch.request.json'
import statusResponse from '../../../tests/fixtures/native/usage-discount-status.get.json'
import estimateResponse from '../../../tests/fixtures/native/usage-discount-estimate.post.json'
import selfResponse from '../../../tests/fixtures/native/usage-discount-self.get.json'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), patch: vi.fn(), post: vi.fn() },
}))

/**
 * A deep copy of a fixture with the field at a dotted path replaced; undefined
 * removes it. Array indexes are path segments.
 */
function changed(fixture: unknown, path: string, value: unknown) {
  const copy = structuredClone(fixture) as Record<string, unknown>
  const keys = path.split('.')
  let target = copy
  for (const key of keys.slice(0, -1))
    target = target[key] as Record<string, unknown>
  const last = keys.at(-1)!
  if (value === undefined) delete target[last]
  else target[last] = value
  return copy
}

describe('usage discount adapter', () => {
  afterEach(() => vi.clearAllMocks())

  it('reads the generated configuration with rates and thresholds as strings', async () => {
    vi.mocked(api.get).mockResolvedValue(configResponse.data)
    const settings = await usageDiscountApi.load()

    expect(api.get).toHaveBeenCalledWith(
      '/api/option/usage_discount',
      undefined,
      {
        signal: undefined,
      }
    )
    expect(settings.version).toBe(0)
    expect(settings.config.cap_rate).toBe('0.05')
    expect(settings.config.personal.month.tiers[0]).toEqual({
      threshold_quota: '12500000',
      rate: '0.01',
    })
    expect(settings.config.excluded_user_ids).toEqual([1])
  })

  it('saves exactly the generated PATCH body: expected_version and config, no version inside', async () => {
    const settings = parseUsageDiscountSettings(configResponse.data)
    const draft = draftFromConfig(settings.config)
    // The edit the backend contract test makes: 1% fixed, month tiers on.
    draft.fixed = '1'
    draft.tables.month.mode = 'on'
    const built = buildConfig(draft)
    if (!built.ok) throw new Error('draft rejected')
    vi.mocked(api.patch).mockResolvedValue({
      ...configResponse.data,
      version: 1,
      created_at: 1793300000,
      created_by: 1,
      config: patchRequest.config,
    })

    const saved = await usageDiscountApi.save(settings.version, built.config)

    expect(api.patch).toHaveBeenCalledTimes(1)
    const [url, body] = vi.mocked(api.patch).mock.calls[0]!
    expect(url).toBe('/api/option/usage_discount')
    // Deep equality with the generated request: no extra fields and every
    // rate and threshold a string, as the strict decoder requires.
    expect(body).toEqual(patchRequest)
    expect(saved.version).toBe(1)
  })

  it('reads the generated status, estimate and self views', () => {
    const status = parseUsageDiscountStatus(statusResponse.data)
    expect(status.health.hourly_write_failures).toEqual({
      count: 2,
      last_at: 1793300000,
    })
    expect(status.earliest_hour_utc).toBeNull()
    expect(status.site_month_quota).toBe('0')

    const estimate = parseUsageDiscountEstimate(estimateResponse.data)
    expect(estimate).toMatchObject({
      from_date: '2026-09-30',
      to_date: '2026-10-29',
      days: 30,
      revenue_quota: '0',
      discount_share: '0',
      history_complete: false,
    })

    const self = parseUsageDiscountSelf(selfResponse.data)
    expect(self).toMatchObject({
      status: 'ok',
      factor: '0.98',
      global_factor: '0.99',
      winner_window: 'month',
      business_date: '2026-10-30',
    })
    expect(self.windows[0]).toMatchObject({
      window: 'month',
      quota: '12500000',
    })
  })

  it('sends the draft config as the estimate body', async () => {
    vi.mocked(api.post).mockResolvedValue(estimateResponse.data)
    const config = parseUsageDiscountSettings(configResponse.data).config
    await usageDiscountApi.estimate(config)
    expect(api.post).toHaveBeenCalledWith(
      '/api/option/usage_discount/estimate',
      { config },
      { signal: undefined }
    )
  })

  it.each([
    ['a rate sent as a JSON number', 'config.cap_rate', 0.05],
    ['a rate in exponent form', 'config.fixed_rate', '5e-2'],
    ['a rate with five decimals', 'config.cap_rate', '0.05001'],
    [
      'a threshold sent as a JSON number',
      'config.personal.day.tiers.0.threshold_quota',
      2500000,
    ],
    [
      'a threshold with 19 digits',
      'config.personal.day.tiers.0.threshold_quota',
      '1000000000000000000',
    ],
    ['an unknown mode', 'config.site.mode', 'auto'],
    // The editor would drop an unknown field on the next save.
    ['an unknown config field', 'config.version', 0],
    ['an unknown tier field', 'config.personal.week.tiers.0.label', ''],
    ['another schema version', 'config.schema_version', 2],
    ['a non-positive excluded id', 'config.excluded_user_ids.0', 0],
  ])('rejects a configuration with %s', (_name, path, value) => {
    expect(() =>
      parseUsageDiscountSettings(changed(configResponse.data, path, value))
    ).toThrow(ApiError)
  })

  it.each([
    ['status', 'site_tier_rate', 0],
    ['status', 'health.cleanup_failures', undefined],
    ['estimate', 'discount_share', 0],
    // int64 quota totals arrive as JSON numbers.
    ['estimate', 'revenue_quota', '0'],
    ['self', 'factor', 0.98],
    ['self', 'status', 'active'],
    ['self', 'winner_window', 'year'],
  ] as const)('rejects a malformed %s view: %s', (view, path, value) => {
    const cases = {
      status: [parseUsageDiscountStatus, statusResponse.data],
      estimate: [parseUsageDiscountEstimate, estimateResponse.data],
      self: [parseUsageDiscountSelf, selfResponse.data],
    } as const
    const [parse, fixture] = cases[view]
    expect(() => parse(changed(fixture, path, value))).toThrow(ApiError)
  })

  it('accepts a self view without the optional winner window and business date', () => {
    const self = parseUsageDiscountSelf(
      changed(
        changed(selfResponse.data, 'winner_window', undefined),
        'business_date',
        undefined
      )
    )
    expect(self).not.toHaveProperty('winner_window')
    expect(self).not.toHaveProperty('business_date')
  })

  it('names only the version conflict a conflict', () => {
    expect(
      isUsageDiscountConflict(
        new ApiError('usage_discount_version_conflict', {
          status: 409,
          code: 'usage_discount_version_conflict',
        })
      )
    ).toBe(true)
    // The transport also answers 409 for a changed login session.
    expect(
      isUsageDiscountConflict(
        new ApiError('session changed', {
          status: 409,
          code: 'AUTH_SESSION_CHANGED',
        })
      )
    ).toBe(false)
    expect(
      isUsageDiscountConflict(
        new ApiError('bad', {
          status: 400,
          code: 'usage_discount_invalid_config',
        })
      )
    ).toBe(false)
  })
})
