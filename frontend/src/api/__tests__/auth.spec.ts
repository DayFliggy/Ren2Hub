import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { authApi } from '@/api/auth'
import { api } from '@/api/client'
import { httpTransport } from '@/api/httpTransport'
import {
  clearAuthBundle,
  getAuthBundle,
  setAuthBundle,
} from '@/api/authSession'
import { ApiError } from '@/api/types'
import type { AuthBundle } from '@/types/auth'

const bundle: AuthBundle = {
  access_token: 'old-token',
  token_type: 'Bearer',
  access_expires_at: 2_000_000_000,
  session: {
    sid: 'session-1',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'vitest',
    created_at: 1,
    last_active_at: 2,
    expires_at: 2_000_000_000,
  },
  user: {
    id: 1,
    username: 'demo',
    display_name: 'Demo',
    email: 'demo@example.com',
    role: 10,
    status: 1,
    quota: 100,
    used_quota: 5,
    request_count: 12,
  },
}

beforeEach(() => {
  clearAuthBundle()
  vi.spyOn(api, 'get').mockResolvedValue({ enabled: false })
})
afterEach(() => vi.restoreAllMocks())

describe('authentication API', () => {
  it('confirms reset links through the existing API and validates the generated password', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue('example-generated-password')
    await expect(
      authApi.confirmPasswordReset('reset@example.com', 'example-code')
    ).resolves.toBe('example-generated-password')
    expect(post).toHaveBeenCalledWith('/api/user/reset', {
      email: 'reset@example.com',
      token: 'example-code',
    })
    post.mockResolvedValue({ password: 'unexpected shape' })
    await expect(
      authApi.confirmPasswordReset('reset@example.com', 'example-code')
    ).rejects.toThrow('Invalid authentication response')
  })
  it('accepts the dataless native success of verification and reset mail', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue({ success: true, message: '' } as never)

    await expect(
      authApi.sendVerification('new@example.com')
    ).resolves.toBeUndefined()
    await expect(
      authApi.resetPassword('reset@example.com')
    ).resolves.toBeUndefined()

    expect(
      request.mock.calls.map(([method, url, options]) => [
        method,
        url,
        options?.params,
      ])
    ).toEqual([
      ['GET', '/api/verification', { email: 'new@example.com' }],
      ['GET', '/api/reset_password', { email: 'reset@example.com' }],
    ])
  })
  it('accepts the dataless native register success without an email', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue({ success: true, message: '' } as never)

    await expect(
      authApi.register({ username: 'demo', password: 'password-123' })
    ).resolves.toBeUndefined()

    expect(request).toHaveBeenCalledWith(
      'POST',
      '/api/user/register',
      expect.objectContaining({
        data: { username: 'demo', password: 'password-123' },
        params: {},
      })
    )
  })
  it('rejects a native mail business failure', async () => {
    vi.spyOn(httpTransport, 'request').mockResolvedValue({
      success: false,
      message: 'Email address is already in use',
      message_key: 'Email address is already in use',
    } as never)

    await expect(
      authApi.sendVerification('used@example.com')
    ).rejects.toMatchObject({
      business: true,
      message: 'Email address is already in use',
    })
  })
  it('sends Turnstile tokens in the native turnstile query', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(bundle)
      .mockResolvedValueOnce(undefined)
    const getEnvelope = vi
      .spyOn(api, 'getEnvelope')
      .mockResolvedValue({ success: true, message: '' })

    await authApi.login('demo', 'password', 'login-token')
    await authApi.register(
      { username: 'demo', email: 'new@example.com', password: 'password' },
      'register-token'
    )
    await authApi.sendVerification('new@example.com', 'mail-token')
    await authApi.resetPassword('reset@example.com', 'reset-token')

    expect(post.mock.calls).toEqual([
      [
        '/api/user/login',
        { username: 'demo', password: 'password' },
        { params: { turnstile: 'login-token' } },
      ],
      [
        '/api/user/register',
        { username: 'demo', email: 'new@example.com', password: 'password' },
        { params: { turnstile: 'register-token' } },
      ],
    ])
    expect(getEnvelope.mock.calls).toEqual([
      [
        '/api/verification',
        { email: 'new@example.com', turnstile: 'mail-token' },
      ],
      [
        '/api/reset_password',
        { email: 'reset@example.com', turnstile: 'reset-token' },
      ],
    ])
  })
  it('omits the turnstile query when Turnstile is not in use', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(bundle)

    await authApi.login('demo', 'password')

    expect(post).toHaveBeenCalledWith(
      '/api/user/login',
      { username: 'demo', password: 'password' },
      { params: {} }
    )
  })

  it('recovers a session mismatch before retrying logout', async () => {
    setAuthBundle(bundle)
    const replacement = {
      ...bundle,
      access_token: 'replacement-token',
      session: { ...bundle.session, sid: 'session-2' },
    }
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValueOnce(
        new ApiError('Session mismatch', {
          status: 409,
          code: 'AUTH_SESSION_MISMATCH',
        })
      )
      .mockResolvedValueOnce(replacement)
      .mockResolvedValueOnce(undefined)

    await authApi.logout()

    expect(post.mock.calls.map(([url]) => url)).toEqual([
      '/api/user/auth/logout',
      '/api/user/auth/refresh',
      '/api/user/auth/logout',
    ])
    expect(getAuthBundle()?.session.sid).toBe('session-2')
  })
})
