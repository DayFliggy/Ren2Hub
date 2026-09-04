import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  clearAuthBundle,
  getAuthBundle,
  getAuthSessionGeneration,
  setAuthBundle,
} from '@/api/authSession'
import { ApiError } from '@/api/types'
import type { AuthBundle } from '@/types/auth'

const mocks = vi.hoisted(() => ({
  authApi: {
    refreshSession: vi.fn(),
    self: vi.fn(),
  },
  publishAuthSessionEvent: vi.fn(),
}))

vi.mock('@/api/auth', () => ({ authApi: mocks.authApi }))
vi.mock('@/api/authSessionSync', () => ({
  publishAuthSessionEvent: mocks.publishAuthSessionEvent,
  subscribeAuthSessionEvents: vi.fn(() => () => undefined),
}))

import { useAuthStore } from '@/stores/auth'

const bundle: AuthBundle = {
  access_token: 'access-token',
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
    role: 1,
    status: 1,
    quota: 100,
    used_quota: 0,
    request_count: 0,
    created_at: 1,
  },
}

beforeEach(() => {
  clearAuthBundle()
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('authentication store', () => {
  it('invalidates the local session and broadcasts when self returns 401', async () => {
    setAuthBundle(bundle)
    const auth = useAuthStore()
    auth.persist(bundle.user)
    mocks.authApi.self.mockRejectedValueOnce(
      new ApiError('Session expired', { status: 401 })
    )

    await expect(auth.fetchSelf()).resolves.toBe(false)

    expect(getAuthBundle()).toBeNull()
    expect(auth.user).toBeNull()
    expect(auth.checked).toBe(true)
    expect(mocks.publishAuthSessionEvent).toHaveBeenCalledWith(
      'signed_out',
      'session-1'
    )
  })

  it('does not rebroadcast a peer-triggered 401 invalidation', async () => {
    setAuthBundle(bundle)
    const auth = useAuthStore()
    auth.persist(bundle.user)
    mocks.authApi.self.mockRejectedValueOnce(
      new ApiError('Session expired', { status: 401 })
    )

    await expect(auth.fetchSelf(false)).resolves.toBe(false)

    expect(getAuthBundle()).toBeNull()
    expect(auth.user).toBeNull()
    expect(auth.checked).toBe(true)
    expect(mocks.publishAuthSessionEvent).not.toHaveBeenCalled()
  })

  it('keeps repeated local invalidation idempotent after the bundle is cleared', () => {
    setAuthBundle(bundle)
    const auth = useAuthStore()
    auth.persist(bundle.user)

    auth.invalidateLocalSession()
    const generation = getAuthSessionGeneration()
    auth.invalidateLocalSession()

    expect(getAuthSessionGeneration()).toBe(generation)
    expect(mocks.publishAuthSessionEvent).toHaveBeenCalledOnce()
  })
})
