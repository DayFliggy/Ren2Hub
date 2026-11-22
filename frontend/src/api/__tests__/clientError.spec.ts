import { afterAll, describe, expect, it } from 'vitest'

import { ClientError, errorMessage } from '@/api/clientError'
import { ApiError } from '@/api/types'
import i18n, { setLocale } from '@/i18n'

afterAll(() => setLocale('en'))

describe('client error messages', () => {
  it('translates client-generated errors in both locales', async () => {
    const t = i18n.global.t
    const error = new ClientError('oauthPopupBlocked')
    await setLocale('zh-CN')
    expect(errorMessage(error, t)).toBe(
      '浏览器阻止了登录弹窗，请允许弹窗后重试。'
    )
    await setLocale('en')
    expect(errorMessage(error, t)).toBe(
      'The browser blocked the sign-in pop-up. Allow pop-ups and try again.'
    )
  })

  it('shows server messages unchanged', () => {
    const t = i18n.global.t
    expect(errorMessage(new ApiError('令牌已过期', { status: 401 }), t)).toBe(
      '令牌已过期'
    )
    expect(errorMessage('plain', t)).toBe('plain')
  })

  it('keeps the browser cancellation name while translating the text', async () => {
    const t = i18n.global.t
    const oauth = new ClientError('oauthCancelled', 'AbortError')
    const passkey = new ClientError('passkeyCancelled', 'NotAllowedError')
    expect(oauth).toMatchObject({ name: 'AbortError', code: 'oauthCancelled' })
    expect(passkey.name).toBe('NotAllowedError')
    await setLocale('zh-CN')
    expect(errorMessage(oauth, t)).toBe('已取消第三方登录验证。')
    expect(errorMessage(passkey, t)).toBe('已取消 Passkey 验证。')
    await setLocale('en')
    expect(errorMessage(oauth, t)).toBe('Third-party sign-in was cancelled.')
    expect(errorMessage(passkey, t)).toBe('Passkey verification was cancelled.')
  })
})
