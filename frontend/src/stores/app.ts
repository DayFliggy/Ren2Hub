import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { publicApi, type PublicStatus } from '@/api/public'
import { safeExternalUrl, safeImageUrl } from '@/utils/safeUrl'

export type PublicLoadState =
  'idle' | 'loading' | 'ready' | 'degraded' | 'error'

export interface ModuleAccess {
  enabled: boolean
  requireAuth: boolean
}

const DEFAULT_SYSTEM_NAME = 'Ren2Hub'
const DEFAULT_LOGO = '/logo.png'

export function parseBoolean(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (value === 1 || value === '1' || value === 'true') return true
  if (value === 0 || value === '0' || value === 'false') return false
  return fallback
}

export function parseModuleAccess(value: unknown): ModuleAccess {
  if (!value || typeof value !== 'object') {
    return { enabled: parseBoolean(value, true), requireAuth: false }
  }
  const record = value as Record<string, unknown>
  return {
    enabled: parseBoolean(record.enabled, true),
    requireAuth: parseBoolean(record.requireAuth, false),
  }
}

export function parseHeaderModules(value: unknown): Record<string, unknown> {
  if (value && typeof value === 'object')
    return value as Record<string, unknown>
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return parsed && typeof parsed === 'object'
      ? (parsed as Record<string, unknown>)
      : {}
  } catch {
    return {}
  }
}

function toPlainText(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return ''
  const documentNode = new DOMParser().parseFromString(value, 'text/html')
  return (documentNode.body.textContent || '').replace(/\s+/g, ' ').trim()
}

export const useAppStore = defineStore('app', () => {
  const phase = ref<PublicLoadState>('idle')
  const statusReachable = ref(false)
  const status = ref<PublicStatus>({})
  const notice = ref('')
  const modelCount = ref<number | null>(null)
  const uptimePercent = ref<number | null>(null)
  const lastError = ref<unknown>(null)

  const initialized = computed(
    () => phase.value === 'ready' || phase.value === 'degraded'
  )
  const loading = computed(() => phase.value === 'loading')
  const online = computed(() => phase.value === 'ready')
  const systemName = computed(
    () => status.value.system_name?.trim() || DEFAULT_SYSTEM_NAME
  )
  const logo = computed(() => safeImageUrl(status.value.logo) || DEFAULT_LOGO)
  const docsLink = computed(() => safeExternalUrl(status.value.docs_link) || '')
  const version = computed(() => status.value.version?.trim() || '')
  const headerModules = computed(() =>
    parseHeaderModules(status.value.HeaderNavModules)
  )
  const pricingAccess = computed(() =>
    parseModuleAccess(headerModules.value.pricing)
  )
  const showPricing = computed(() => pricingAccess.value.enabled)
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
  const uptimeLabel = computed(() =>
    uptimePercent.value === null ? '--' : `${uptimePercent.value.toFixed(2)}%`
  )
  const versionLabel = computed(() => version.value || '--')

  function applyBranding(): void {
    document.title = `${systemName.value} | One Key, All Models`
    const customLogo = safeImageUrl(status.value.logo)
    if (!customLogo) return
    let favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!favicon) {
      favicon = document.createElement('link')
      favicon.rel = 'icon'
      document.head.append(favicon)
    }
    favicon.href = customLogo
  }

  async function initialize(): Promise<void> {
    if (
      phase.value === 'loading' ||
      phase.value === 'ready' ||
      phase.value === 'degraded'
    ) {
      return
    }

    phase.value = 'loading'
    lastError.value = null
    const controller = new AbortController()
    try {
      status.value = (await publicApi.status(controller.signal)) || {}
      statusReachable.value = true
      applyBranding()

      const summaries = await Promise.allSettled([
        publicApi.notice(controller.signal),
        publicApi.pricing(controller.signal),
        publicApi.uptime(controller.signal),
      ])
      const [noticeResult, pricingResult, uptimeResult] = summaries

      if (noticeResult.status === 'fulfilled') {
        notice.value = toPlainText(noticeResult.value)
      }
      if (pricingResult.status === 'fulfilled') {
        modelCount.value = pricingResult.value.length
      }
      if (uptimeResult.status === 'fulfilled') {
        const monitors = uptimeResult.value.flatMap(
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
      }

      phase.value = summaries.every((result) => result.status === 'fulfilled')
        ? 'ready'
        : 'degraded'
    } catch (error) {
      statusReachable.value = false
      lastError.value = error
      phase.value = 'error'
    }
  }

  async function retry(): Promise<void> {
    if (phase.value !== 'error') return
    await initialize()
  }

  return {
    phase,
    initialized,
    loading,
    online,
    statusReachable,
    lastError,
    notice,
    systemName,
    logo,
    docsLink,
    showPricing,
    showDocs,
    showAbout,
    userAgreementEnabled,
    privacyPolicyEnabled,
    modelCountLabel,
    uptimeLabel,
    versionLabel,
    initialize,
    retry,
  }
})
