import { beforeEach, describe, expect, it, vi } from 'vitest'

import { authApi } from '@/api/auth'
import { api } from '@/api/client'
import { parseAccessToken, parseLoginSessions } from '@/api/contracts'
import { ApiError } from '@/api/types'

beforeEach(() => vi.restoreAllMocks())

describe('account security API', () => {
  it('generates an access token and validates the one-time string response', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue('secret-token')

    await expect(authApi.generateAccessToken()).resolves.toBe('secret-token')
    expect(get).toHaveBeenCalledWith('/api/user/token')
    expect(() => parseAccessToken('')).toThrow(ApiError)
  })

  it('loads and revokes login sessions with encoded session ids', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue([
      {
        sid: 'current/session',
        current: true,
        login_method: 'password',
        ip: '127.0.0.1',
        user_agent: 'Test Browser',
        created_at: 1,
        last_active_at: 2,
        expires_at: 3,
      },
    ])
    const del = vi.spyOn(api, 'delete').mockResolvedValue({
      revoked_sid: 'current/session',
      current: true,
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({ revoked_count: 2 })

    await expect(authApi.getLoginSessions()).resolves.toHaveLength(1)
    await authApi.revokeLoginSession('current/session')
    await authApi.revokeOtherLoginSessions()

    expect(get).toHaveBeenCalledWith('/api/user/sessions')
    expect(del).toHaveBeenCalledWith('/api/user/sessions/current%2Fsession')
    expect(post).toHaveBeenCalledWith('/api/user/sessions/revoke-others')
  })

  it('rejects malformed login session payloads', () => {
    expect(() => parseLoginSessions([{ sid: 'missing-fields' }])).toThrow(
      ApiError
    )
    expect(() => parseLoginSessions({})).toThrow(ApiError)
  })
})
