import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api/client'

interface ApiResponse<T> {
  success: boolean
  message?: string
  data: T
}

interface ModuleAccess {
  enabled: boolean
  requireAuth: boolean
}

interface StatusData {
  version?: string
  system_name?: string
  logo?: string
  docs_link?: string
  register_enabled?: boolean
  user_agreement_enabled?: boolean
  privacy_policy_enabled?: boolean
  uptime_kuma_enabled?: boolean
  HeaderNavModules?: unknown
}

interface PricingModel {
  id: number
}

interface UptimeMonitor {
  uptime: number
  status: number
}

interface UptimeGroup {
  monitors: UptimeMonitor[]
}

const DEFAULT_SYSTEM_NAME = 'RenRen AI'
const DEFAULT_LOGO = '/logo.png'

function parseBoolean(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (value === 1 || value === '1' || value === 'true') return true
  if (value === 0 || value === '0' || value === 'false') return false
  return fallback
}

function parseModuleAccess(value: unknown): ModuleAccess {
  if (value && typeof value === 'object') {
    const record = value as Record<string, unknown>
    return {
      enabled: parseBoolean(record.enabled, true),
      requireAuth: parseBoolean(record.requireAuth, false),
    }
  }

  return { enabled: parseBoolean(value, true), requireAuth: false }
}

function parseHeaderModules(value: unknown): Record<string, unknown> {
  if (value && typeof value === 'object')
    return value as Record<string, unknown>
  if (typeof value !== 'string' || !value.trim()) return {}

  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object'
      ? (parsed as Record<string, unknown>)
      : {}
  } catch (error) {
    console.error('Failed to parse HeaderNavModules:', error)
    return {}
  }
}

function getStoredUser(): Record<string, unknown> | null {
  try {
    const raw = window.localStorage.getItem('user')
    return raw ? (JSON.parse(raw) as Record<string, unknown>) : null
  } catch (error) {
    console.error('Failed to restore user state:', error)
    return null
  }
}

function toPlainText(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return ''
  const documentNode = new DOMParser().parseFromString(value, 'text/html')
  return (documentNode.body.textContent || '').replace(/\s+/g, ' ').trim()
}

export const useAppStore = defineStore('app', () => {
  const initialized = ref(false)
  const loading = ref(false)
  const online = ref(false)
  const status = ref<StatusData>({})
  const notice = ref('')
  const modelCount = ref<number | null>(null)
  const uptimePercent = ref<number | null>(null)
  const user = ref<Record<string, unknown> | null>(getStoredUser())

  const systemName = computed(
    () => status.value.system_name?.trim() || DEFAULT_SYSTEM_NAME
  )
  const logo = computed(() => status.value.logo?.trim() || DEFAULT_LOGO)
  const docsLink = computed(() => status.value.docs_link?.trim() || '')
  const version = computed(() => status.value.version?.trim() || '')
  const isAuthenticated = computed(() => Boolean(user.value))
  const primaryHref = computed(() =>
    isAuthenticated.value ? '/dashboard' : '/sign-in'
  )
  const headerModules = computed(() =>
    parseHeaderModules(status.value.HeaderNavModules)
  )
  const pricingAccess = computed(() =>
    parseModuleAccess(headerModules.value.pricing)
  )
  const showPricing = computed(
    () =>
      pricingAccess.value.enabled &&
      (!pricingAccess.value.requireAuth || isAuthenticated.value)
  )
  const showDocs = computed(
    () =>
      parseBoolean(headerModules.value.docs, true) && Boolean(docsLink.value)
  )
  const showAbout = computed(() =>
    parseBoolean(headerModules.value.about, true)
  )
  const userAgreementEnabled = computed(() =>
    Boolean(status.value.user_agreement_enabled)
  )
  const privacyPolicyEnabled = computed(() =>
    Boolean(status.value.privacy_policy_enabled)
  )
  const modelCountLabel = computed(() =>
    modelCount.value === null ? '--' : String(modelCount.value)
  )
  const uptimeLabel = computed(() => {
    if (uptimePercent.value !== null)
      return `${uptimePercent.value.toFixed(2)}%`
    return online.value ? 'ONLINE' : 'OFFLINE'
  })
  const versionLabel = computed(() => version.value || '--')

  function applyBranding(): void {
    document.title = `${systemName.value} · One Key, All Models`

    let favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!favicon) {
      favicon = document.createElement('link')
      favicon.rel = 'icon'
      document.head.append(favicon)
    }
    favicon.href = logo.value
  }

  async function initialize(): Promise<void> {
    if (initialized.value || loading.value) return
    loading.value = true

    try {
      const statusResponse =
        await api.get<ApiResponse<StatusData>>('/api/status')
      if (!statusResponse.data.success) {
        throw new Error(
          statusResponse.data.message || 'Status API returned an error'
        )
      }

      status.value = statusResponse.data.data || {}
      online.value = true
      applyBranding()

      const requests = await Promise.allSettled([
        api.get<ApiResponse<string>>('/api/notice'),
        api.get<ApiResponse<PricingModel[]>>('/api/pricing'),
        api.get<ApiResponse<UptimeGroup[]>>('/api/uptime/status'),
      ])

      const [noticeResult, pricingResult, uptimeResult] = requests

      if (
        noticeResult.status === 'fulfilled' &&
        noticeResult.value.data.success
      ) {
        notice.value = toPlainText(noticeResult.value.data.data)
      } else if (noticeResult.status === 'rejected') {
        console.error('Failed to load notice:', noticeResult.reason)
      }

      if (
        pricingResult.status === 'fulfilled' &&
        pricingResult.value.data.success
      ) {
        modelCount.value = pricingResult.value.data.data.length
      } else if (pricingResult.status === 'rejected') {
        console.error('Failed to load pricing summary:', pricingResult.reason)
      }

      if (
        uptimeResult.status === 'fulfilled' &&
        uptimeResult.value.data.success
      ) {
        const monitors = uptimeResult.value.data.data.flatMap(
          (group) => group.monitors || []
        )
        const measured = monitors.filter((monitor) =>
          Number.isFinite(monitor.uptime)
        )
        if (measured.length > 0) {
          uptimePercent.value =
            (measured.reduce((sum, monitor) => sum + monitor.uptime, 0) /
              measured.length) *
            100
        }
      } else if (uptimeResult.status === 'rejected') {
        console.error('Failed to load uptime summary:', uptimeResult.reason)
      }
    } catch (error) {
      online.value = false
      console.error('Failed to initialize the application:', error)
    } finally {
      loading.value = false
      initialized.value = true
    }
  }

  return {
    initialized,
    loading,
    online,
    notice,
    systemName,
    logo,
    docsLink,
    isAuthenticated,
    primaryHref,
    showPricing,
    showDocs,
    showAbout,
    userAgreementEnabled,
    privacyPolicyEnabled,
    modelCountLabel,
    uptimeLabel,
    versionLabel,
    initialize,
  }
})
