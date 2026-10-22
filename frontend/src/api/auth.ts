import { passwordPayload, proofOptions } from './security'
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
    await api.getEnvelope<unknown>(
      '/api/verification',
      { email },
      turnstileToken
        ? { headers: { 'X-Turnstile-Token': turnstileToken } }
        : undefined
    )
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
        turnstileToken
          ? { headers: { 'X-Turnstile-Token': turnstileToken } }
          : undefined
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
      email: string
      password: string
      aff_code?: string
      verification_code?: string
    },
    turnstileToken?: string
  ) =>
    api.post<{ message: string }>(
      '/api/user/register',
      payload,
      turnstileToken
        ? { headers: { 'X-Turnstile-Token': turnstileToken } }
        : undefined
    ),
  /** Native success carries only success and message, so read the envelope. */
  async resetPassword(email: string): Promise<void> {
    await api.getEnvelope<unknown>('/api/reset_password', { email })
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
  ): Promise<AuthTokenRotation> {
    const value = await api.put<unknown>(
      '/api/user/self',
      {
        original_password: originalPassword,
        password,
      },
      proofOptions(proof)
    )
    return parseAuthRotation(value) ?? invalidAuthResponse('/api/user/self')
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
