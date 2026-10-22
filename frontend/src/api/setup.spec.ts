import { afterEach, describe, expect, it } from 'vitest'

import { setLocale } from '@/i18n'
import { ApiError } from './types'
import {
  buildSetupPayload,
  isSetupUsernameWithinLimit,
  parseSetupStatusEnvelope,
  parseSetupSubmitEnvelope,
  setupCharacterLength,
  type SetupFormValues,
} from './setup'

describe('setup API contract', () => {
  afterEach(async () => {
    await setLocale('en')
  })

  it('strictly parses setup status', () => {
    expect(
      parseSetupStatusEnvelope({
        success: true,
        data: { status: false, root_init: true, database_type: 'postgres' },
      })
    ).toEqual({ status: false, root_init: true, database_type: 'postgres' })
  })

  it('rejects malformed status envelopes', () => {
    expect(() => parseSetupStatusEnvelope({ success: true, data: {} })).toThrow(
      ApiError
    )
  })

  it('accepts the backend POST success without data', () => {
    expect(() =>
      parseSetupSubmitEnvelope({ success: true, message: 'ok' })
    ).not.toThrow()
  })

  it('preserves backend business errors', () => {
    expect(() =>
      parseSetupSubmitEnvelope({ success: false, message: '密码错误' })
    ).toThrow('密码错误')
  })

  it('renders the backend message key in the current locale', async () => {
    await setLocale('zh-CN')
    expect(() =>
      parseSetupSubmitEnvelope({
        success: false,
        message: 'Username must be 12 characters or fewer',
        message_key: 'Username must be {{max}} characters or fewer',
        message_params: { max: 12 },
      })
    ).toThrow('用户名长度不能超过12字节')
  })

  it('maps usage modes and omits account fields for existing roots', () => {
    const values: SetupFormValues = {
      username: ' admin ',
      password: 'password123',
      confirmPassword: 'password123',
      usageMode: 'self',
    }
    expect(buildSetupPayload(values, true)).toEqual({
      SelfUseModeEnabled: true,
      DemoSiteEnabled: false,
    })
    expect(buildSetupPayload(values, false)).toEqual({
      username: 'admin',
      password: 'password123',
      confirmPassword: 'password123',
      SelfUseModeEnabled: true,
      DemoSiteEnabled: false,
    })
  })

  it.each([
    ['external', false, false],
    ['self', true, false],
    ['demo', false, true],
  ] as const)(
    'maps %s usage mode to the backend flags',
    (usageMode, selfUse, demoSite) => {
      expect(
        buildSetupPayload(
          {
            username: 'admin',
            password: 'password123',
            confirmPassword: 'password123',
            usageMode,
          },
          true
        )
      ).toEqual({
        SelfUseModeEnabled: selfUse,
        DemoSiteEnabled: demoSite,
      })
    }
  )

  // controller/setup.go rejects len(username) > 12, which counts UTF-8 bytes.
  it.each([
    ['a'.repeat(12), true],
    ['a'.repeat(13), false],
    ['管理员账', true],
    ['管理员账户', false],
    ['admin管理', true],
    ['admin管理员', false],
  ])('limits the setup username %s by UTF-8 bytes', (username, allowed) => {
    expect(isSetupUsernameWithinLimit(username)).toBe(allowed)
  })

  it('counts password length in characters like the backend', () => {
    expect(setupCharacterLength('管理员账户')).toBe(5)
  })
})
