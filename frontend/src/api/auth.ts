import { passwordPayload, proofOptions, turnstileParams } from './security'
import type {
  AuthBundle,
  AuthTokenRotation,
  LoginSession,
  LoginSessionBulkRevokeResult,
  LoginSessionRevokeResult,
  LoginResponse,
  UserInfo,
  UserProfilePatch,
} from '@/types/auth'

import { parseSecurityChange, type SecurityChange } from './accountSecurity'
import { api } from './client'
import {
  parseLoginSessionBulkRevokeResult,
  parseLoginSessionRevokeResult,
  parseLoginSessions,
} from './contracts'
import { ApiError } from './types'
import {
  clearAuthBundleIfCurrent,
  getAuthSessionSnapshot,
  parseAuthBundle,
  parseAuthRotation,
  parseTwoFactorChallenge,
  parseUserInfo,
  setAuthBundleIfCurrent,
} from './authSession'

function invalidAuthResponse(endpoint: string): never {
  throw new ApiError(`Invalid authentication response: ${endpoint}`, {
    status: 502,
  })
}

function parseLoginResponse(value: unknown, endpoint: string): LoginResponse {
  return (
    parseAuthBundle(value) ??
    parseTwoFactorChallenge(value) ??
    invalidAuthResponse(endpoint)
  )
}

async function refreshSession(): Promise<AuthBundle> {
  const value = await api.post<unknown>('/api/user/auth/refresh')
  return parseAuthBundle(value) ?? invalidAuthResponse('/api/user/auth/refresh')
}

export const authApi = {
  /** Native success carries only success and message, so read the envelope. */
  async sendVerification(
    email: string,
    turnstileToken?: string
  ): Promise<void> {
    await api.getEnvelope<unknown>('/api/verification', {
      email,
      ...turnstileParams(turnstileToken),
    })
  },
  async login(
    username: string,
    password: string,
    turnstileToken?: string
  ): Promise<LoginResponse> {
    return parseLoginResponse(
      await api.post<unknown>(
        '/api/user/login',
        { username, ...(await passwordPayload(password)) },
        { params: turnstileParams(turnstileToken) }
      ),
      '/api/user/login'
    )
  },
  async verifyTwoFactor(flowToken: string, code: string): Promise<AuthBundle> {
    const value = await api.post<unknown>('/api/user/login/2fa', {
      flow_token: flowToken,
      code,
    })
    return parseAuthBundle(value) ?? invalidAuthResponse('/api/user/login/2fa')
  },
  register: (
    payload: {
      username: string
      /** Sent only when email verification is enabled. */
      email?: string
      password: string
      aff_code?: string
      verification_code?: string
    },
    turnstileToken?: string
  ) =>
    api.post<{ message: string }>('/api/user/register', payload, {
      params: turnstileParams(turnstileToken),
    }),
  /** Native success carries only success and message, so read the envelope. */
  async resetPassword(email: string, turnstileToken?: string): Promise<void> {
    await api.getEnvelope<unknown>('/api/reset_password', {
      email,
      ...turnstileParams(turnstileToken),
    })
  },
  async confirmPasswordReset(email: string, token: string): Promise<string> {
    const value = await api.post<unknown>('/api/user/reset', { email, token })
    if (typeof value !== 'string' || !value)
      return invalidAuthResponse('/api/user/reset')
    return value
  },
  refreshSession,
  async logout(): Promise<void> {
    const snapshot = getAuthSessionSnapshot()
    try {
      await api.post('/api/user/auth/logout')
      return
    } catch (error) {
      if (
        !(error instanceof ApiError) ||
        error.status !== 409 ||
        error.code !== 'AUTH_SESSION_MISMATCH'
      ) {
        throw error
      }
    }

    if (!clearAuthBundleIfCurrent(snapshot)) {
      throw new ApiError('Authentication session changed during logout', {
        status: 409,
      })
    }
    const recoverySnapshot = getAuthSessionSnapshot()
    let bundle: AuthBundle
    try {
      bundle = await refreshSession()
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) return
      throw error
    }
    if (!setAuthBundleIfCurrent(recoverySnapshot, bundle)) {
      throw new ApiError('Authentication session changed during logout', {
        status: 409,
      })
    }
    await api.post('/api/user/auth/logout')
  },
  async self(): Promise<UserInfo> {
    return (
      parseUserInfo(await api.get<unknown>('/api/user/self')) ??
      invalidAuthResponse('/api/user/self')
    )
  },
  deleteSelf: (proof: string) =>
    api.delete<{ message: string }>(
      '/api/user/self',
      undefined,
      proofOptions(proof)
    ),
  updateProfile: (patch: UserProfilePatch) =>
    api.put<unknown>('/api/user/self', patch),
  updateLanguage: (language: string) =>
    api.put<unknown>('/api/user/self', { language }),
  async changePassword(
    originalPassword: string,
    password: string,
    proof: string
  ): Promise<AuthTokenRotation & SecurityChange> {
    const endpoint = '/api/user/self'
    const value = await api.put<unknown>(
      endpoint,
      {
        original_password: originalPassword,
        password,
      },
      proofOptions(proof)
    )
    const rotation = parseAuthRotation(value) ?? invalidAuthResponse(endpoint)
    return { ...rotation, ...parseSecurityChange(value, endpoint) }
  },
  async getLoginSessions(): Promise<LoginSession[]> {
    return parseLoginSessions(await api.get<unknown>('/api/user/sessions'))
  },
  async revokeLoginSession(sid: string): Promise<LoginSessionRevokeResult> {
    const value = await api.delete<unknown>(
      `/api/user/sessions/${encodeURIComponent(sid)}`
    )
    return parseLoginSessionRevokeResult(value)
  },
  async revokeOtherLoginSessions(): Promise<LoginSessionBulkRevokeResult> {
    return parseLoginSessionBulkRevokeResult(
      await api.post<unknown>('/api/user/sessions/revoke-others')
    )
  },
}
