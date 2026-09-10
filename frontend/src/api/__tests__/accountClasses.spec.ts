import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  accountClassesApi,
  parseAccountClass,
  parseLimitOverride,
} from '@/api/accountClasses'

const api = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api }))

const row = {
  code: 'default',
  name: 'Default',
  default_concurrency: 5,
  default_rpm: 0,
  topup_multiplier: 1,
  policy_version: 7,
}

describe('account class contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it('preserves explicit unlimited limits and the policy version', () => {
    expect(parseAccountClass(row)).toEqual(row)
    expect(
      parseAccountClass({ ...row, default_concurrency: 0 }).default_concurrency
    ).toBe(0)
  })

  it.each([
    { topup_multiplier: 0 },
    { topup_multiplier: -1 },
    { topup_multiplier: Infinity },
    { default_concurrency: -1 },
    { default_concurrency: 1.5 },
    { default_rpm: '0' },
    { policy_version: 0 },
    { code: '' },
  ])('rejects invalid class policy %j', (patch) => {
    expect(() => parseAccountClass({ ...row, ...patch })).toThrow()
  })

  it('distinguishes inheritance, unlimited, and a positive override', () => {
    expect(parseLimitOverride('', 100)).toBeNull()
    expect(parseLimitOverride('0', 100)).toBe(0)
    expect(parseLimitOverride('5', 100)).toBe(5)
    for (const input of ['-1', '1.5', '1e2', '101', 'NaN']) {
      expect(parseLimitOverride(input, 100)).toBeUndefined()
    }
  })

  it('passes immutable codes as data instead of URL path segments', async () => {
    const input = { ...row, code: 'a/b?next=../#测试' }
    await accountClassesApi.update(input)
    expect(api.put).toHaveBeenCalledWith('/api/account-classes', input)
    await accountClassesApi.remove(input.code)
    expect(api.delete).toHaveBeenCalledWith('/api/account-classes', {
      code: input.code,
    })
    api.get.mockResolvedValue({ items: [row] })
    await expect(accountClassesApi.list()).rejects.toThrow()
  })
})
