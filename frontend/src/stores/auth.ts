import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { clearDemoUser, readDemoUser, writeDemoUser } from '@/api/demoStorage'
import { setUnauthorizedHandler } from '@/api/createClient'
import { ApiError } from '@/api/types'
import type { UserInfo } from '@/types/auth'

async function getAuthApi() {
  const { authApi } = await import('@/api/auth')
  return authApi
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<UserInfo | null>(readDemoUser())
  const checked = ref(false)

  const isAuthenticated = computed(() => Boolean(user.value))
  // Demo sessions are local UI state, never proof of server-side authority.
  const isAdmin = computed(() => false)
  const isRoot = computed(() => false)
  const adminPermissions = computed<string[]>(() => [])

  function persist(next: UserInfo | null): void {
    user.value = next
    try {
      if (next) writeDemoUser(next)
      else clearDemoUser()
    } catch {
      // Restricted storage degrades to the in-memory demo session.
    }
  }

  async function login(username: string, password: string): Promise<void> {
    const api = await getAuthApi()
    const data = await api.login(username, password)
    persist(data.user)
    checked.value = true
  }

  async function logout(): Promise<void> {
    const api = await getAuthApi()
    try {
      await api.logout()
    } finally {
      persist(null)
      checked.value = true
    }
  }

  async function fetchSelf(): Promise<boolean> {
    const api = await getAuthApi()
    try {
      const fresh = await api.self()
      persist(fresh)
      checked.value = true
      return true
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        persist(null)
        checked.value = true
        return false
      }

      // Keep checked=false so the next protected navigation retries.
      return Boolean(user.value)
    }
  }

  async function updateProfile(patch: Partial<UserInfo>): Promise<void> {
    const api = await getAuthApi()
    const data = await api.updateProfile(patch)
    persist(data.user)
  }

  setUnauthorizedHandler(() => {
    persist(null)
    checked.value = true
  })

  return {
    user,
    checked,
    isAuthenticated,
    isAdmin,
    isRoot,
    adminPermissions,
    login,
    logout,
    fetchSelf,
    updateProfile,
    persist,
  }
})
