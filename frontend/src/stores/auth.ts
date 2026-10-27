import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import type { SecurityChange } from '@/api/accountSecurity'
import { setApiUnauthorizedHandler } from '@/api/client'
import {
  applyAuthRotation,
  clearAuthBundle,
  getAuthBundle,
  setAuthBundle,
} from '@/api/authSession'
import {
  publishAuthSessionEvent,
  subscribeAuthSessionEvents,
  type AuthSessionSyncEvent,
} from '@/api/authSessionSync'
import { hasSessionHint } from '@/api/sessionHint'
import { ApiError } from '@/api/types'
import type {
  AuthBundle,
  TwoFactorChallenge,
  UserInfo,
  UserProfilePatch,
} from '@/types/auth'

async function getAuthApi() {
  const { authApi } = await import('@/api/auth')
  return authApi
}

export interface AuthSessionSyncEffects {
  currentSid: () => string | undefined
  clearBundle: () => void
  setPending: () => void
  setAnonymous: () => void
  refreshFromCookie: () => Promise<boolean>
}

export async function applyAuthSessionSyncEvent(
  event: AuthSessionSyncEvent,
  effects: AuthSessionSyncEffects
): Promise<void> {
  const currentSid = effects.currentSid()
  if (event.kind === 'signed_out') {
    if (event.sid !== currentSid) return
    effects.clearBundle()
    effects.setAnonymous()
    return
  }

  effects.clearBundle()
  if (event.sid !== currentSid) effects.setPending()

  try {
    if (!(await effects.refreshFromCookie())) effects.setAnonymous()
  } catch {
    effects.setAnonymous()
  }
}

// Refresh failures that say nothing about the session itself. A malformed
// success response is a contract break, not an outage, so it still surfaces.
function isTransientAuthError(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false
  if (error.code === 'INVALID_RESPONSE') return false
  const status = error.status
  return status === undefined || status === 429 || status >= 500
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<UserInfo | null>(null)
  const checked = ref(false)
  // Set when a public page settled on "guest" without the server's verdict.
  // Protected pages must still ask the server before trusting it.
  const guestAssumed = ref(false)

  const isAuthenticated = computed(() => Boolean(user.value))
  const isAdmin = computed(() => (user.value?.role ?? 0) >= 10)
  const isRoot = computed(() => (user.value?.role ?? 0) >= 100)
  const adminPermissions = computed<Record<string, Record<string, boolean>>>(
    () => user.value?.permissions?.admin_permissions ?? {}
  )

  function persist(next: UserInfo | null): void {
    user.value = next
    guestAssumed.value = false
  }

  function hasPermission(resource: string, action: string): boolean {
    if (isRoot.value) return true
    return adminPermissions.value[resource]?.[action] === true
  }

  function publishCurrentSession(
    kind: 'authenticated' | 'signed_out',
    sid = getAuthBundle()?.session.sid
  ): void {
    if (sid) publishAuthSessionEvent(kind, sid)
  }

  function invalidateLocalSession(publish = true): void {
    const bundle = getAuthBundle()
    if (!bundle && user.value === null && checked.value) return
    const sid = bundle?.session.sid
    clearAuthBundle()
    persist(null)
    checked.value = true
    if (publish) publishCurrentSession('signed_out', sid)
  }

  async function login(
    username: string,
    password: string,
    turnstileToken?: string
  ): Promise<TwoFactorChallenge | null> {
    const api = await getAuthApi()
    const data = await api.login(username, password, turnstileToken)
    if ('require_verification' in data) return data
    setAuthBundle(data)
    persist(data.user)
    checked.value = true
    publishCurrentSession('authenticated', data.session.sid)
    return null
  }

  function acceptLogin(bundle: AuthBundle): void {
    setAuthBundle(bundle)
    persist(bundle.user)
    checked.value = true
    publishCurrentSession('authenticated', bundle.session.sid)
  }

  async function verifyTwoFactor(
    flowToken: string,
    code: string
  ): Promise<void> {
    const api = await getAuthApi()
    const bundle = await api.verifyTwoFactor(flowToken, code)
    setAuthBundle(bundle)
    persist(bundle.user)
    checked.value = true
    publishCurrentSession('authenticated', bundle.session.sid)
  }

  async function logout(): Promise<void> {
    const api = await getAuthApi()
    try {
      await api.logout()
    } finally {
      invalidateLocalSession()
    }
  }

  async function fetchSelf(publish = true): Promise<boolean> {
    const api = await getAuthApi()
    try {
      if (!getAuthBundle()) {
        const bundle = await api.refreshSession()
        setAuthBundle(bundle)
        persist(bundle.user)
        checked.value = true
        if (publish) publishCurrentSession('authenticated', bundle.session.sid)
        return true
      }
      const fresh = await api.self()
      persist(fresh)
      checked.value = true
      return true
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        invalidateLocalSession(publish)
        return false
      }

      throw error
    }
  }

  function assumeGuest(): void {
    persist(null)
    checked.value = true
    guestAssumed.value = true
  }

  /**
   * Session check for public and guest-only pages, which render either way.
   * Without the server's hint cookie a refresh is certain to 401 and still
   * spends the IP-keyed CriticalRateLimit budget, so it is skipped. A
   * transient refresh failure also renders the guest view; ensureSession
   * retries once a protected page needs the answer.
   */
  async function ensureVisitorSession(): Promise<void> {
    if (checked.value) return
    if (!getAuthBundle() && !hasSessionHint()) {
      assumeGuest()
      return
    }
    try {
      await fetchSelf()
    } catch (error) {
      if (!isTransientAuthError(error)) throw error
      console.warn('[auth] Session check failed; showing the guest view', error)
      assumeGuest()
    }
  }

  /** Session check for protected pages; failures propagate to the router. */
  async function ensureSession(): Promise<void> {
    if (checked.value && (isAuthenticated.value || !guestAssumed.value)) return
    await fetchSelf()
    guestAssumed.value = false
  }

  async function updateProfile(patch: UserProfilePatch): Promise<void> {
    const api = await getAuthApi()
    await api.updateProfile(patch)
    persist(await api.self())
  }

  async function saveLanguage(language: string): Promise<void> {
    const api = await getAuthApi()
    await api.updateLanguage(language)
    persist(await api.self())
  }

  async function changePassword(
    originalPassword: string,
    password: string,
    proof: string
  ): Promise<SecurityChange> {
    const api = await getAuthApi()
    const { notification_warning, ...rotation } = await api.changePassword(
      originalPassword,
      password,
      proof
    )
    applyAuthRotation(rotation)
    publishCurrentSession('authenticated', rotation.session.sid)
    return { notification_warning }
  }

  async function deleteAccount(proof: string): Promise<void> {
    const api = await getAuthApi()
    await api.deleteSelf(proof)
    invalidateLocalSession()
  }

  setApiUnauthorizedHandler(() => {
    invalidateLocalSession()
  })

  subscribeAuthSessionEvents((event) => {
    // Access tokens never cross tabs. The shared HttpOnly refresh cookie is
    // the only source used to establish the peer's current session.
    void applyAuthSessionSyncEvent(event, {
      currentSid: () => getAuthBundle()?.session.sid,
      clearBundle: clearAuthBundle,
      setPending: () => {
        persist(null)
        checked.value = false
      },
      setAnonymous: () => {
        persist(null)
        checked.value = true
      },
      refreshFromCookie: () => fetchSelf(false),
    })
  })

  return {
    user,
    checked,
    isAuthenticated,
    isAdmin,
    isRoot,
    adminPermissions,
    hasPermission,
    login,
    acceptLogin,
    verifyTwoFactor,
    logout,
    fetchSelf,
    ensureVisitorSession,
    ensureSession,
    updateProfile,
    saveLanguage,
    changePassword,
    deleteAccount,
    invalidateLocalSession,
    persist,
  }
})
