import { configureQuotaDisplay } from '@/utils/format'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { invalidResponse, isRecord } from '@/api/contracts'
import { publicApi, type FeatureStatus, type PublicStatus } from '@/api/public'
import i18n from '@/i18n'
import { publicCatalogApi } from '@/api/publicCatalog'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { BRAND_LOGO_PATH, BRAND_NAME } from '@/constants/branding'
import { useAuthStore } from '@/stores/auth'
import { safeExternalUrl, safeImageUrl } from '@/utils/safeUrl'

export type PublicLoadState =
  'idle' | 'loading' | 'ready' | 'degraded' | 'error'

export interface ModuleAccess {
  enabled: boolean
  requireAuth: boolean
}

function parseBoolean(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (value === 1 || value === '1' || value === 'true') return true
  if (value === 0 || value === '0' || value === 'false') return false
  return fallback
}

function headerNavBool(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') {
    const text = value.trim().toLowerCase()
    if (text === 'true' || text === '1') return true
    if (text === 'false' || text === '0') return false
    return fallback
  }
  if (value === 1) return true
  if (value === 0) return false
  return fallback
}

/**
 * Reads one module from the HeaderNavModules option exactly as
 * middleware/header_nav.go does, so the navigation never hides or shows a
 * page differently from what the backend enforces.
 */
function headerNavAccess(
  raw: string | undefined,
  module: string
): ModuleAccess {
  const open: ModuleAccess = { enabled: true, requireAuth: false }
  if (!raw?.trim()) return open
  let modules: unknown
  try {
    modules = JSON.parse(raw)
  } catch {
    // header_nav.go treats an option it cannot decode as unset (all open).
    return open
  }
  if (!isRecord(modules)) return open
  const value = modules[module]
  if (['boolean', 'string', 'number'].includes(typeof value))
    return { enabled: headerNavBool(value, true), requireAuth: false }
  if (!isRecord(value)) return open
  return {
    enabled: headerNavBool(value.enabled, true),
    requireAuth: headerNavBool(value.requireAuth, false),
  }
}

function parseSidebarModules(raw: string | undefined): Record<string, unknown> {
  if (!raw?.trim()) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch (error) {
    console.error('Invalid SidebarModulesAdmin; showing every module', error)
    return {}
  }
  if (isRecord(parsed)) return parsed
  console.error('SidebarModulesAdmin is not an object; showing every module')
  return {}
}

function toPlainText(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return ''
  const documentNode = new DOMParser().parseFromString(value, 'text/html')
  return (documentNode.body.textContent || '').replace(/\s+/g, ' ').trim()
}

export const useAppStore = defineStore('app', () => {
  const auth = useAuthStore()
  const phase = ref<PublicLoadState>('idle')
  const statusReachable = ref(false)
  const status = ref<PublicStatus>({})
  const notice = ref('')
  const modelCount = ref<number | null>(null)
  const modelCountState = ref<
    'loading' | 'notPublic' | 'signIn' | 'error' | 'ready'
  >('loading')
  const uptimePercent = ref<number | null>(null)
  const lastError = ref<unknown>(null)
  let initializationPromise: Promise<void> | null = null
  let statusRequested = 0
  let statusApplied = 0
  let startupStatus: Promise<void> | null = null
  const pricingRequest = useLatestRequest()
  onScopeDispose(() => {
    modelCount.value = null
  })

  const initialized = computed(
    () => phase.value === 'ready' || phase.value === 'degraded'
  )
  const loading = computed(() => phase.value === 'loading')
  const online = computed(() => phase.value === 'ready')
  const systemName = computed(
    () => status.value.system_name?.trim() || BRAND_NAME
  )
  const logo = computed(
    () => safeImageUrl(status.value.logo) || BRAND_LOGO_PATH
  )
  const docsLink = computed(() => safeExternalUrl(status.value.docs_link) || '')
  const version = computed(() => status.value.version?.trim() || '')
  const startTime = computed(() => status.value.start_time ?? null)
  const moduleAccess = (module: string) =>
    headerNavAccess(status.value.HeaderNavModules, module)
  const pricingAccess = computed(() => moduleAccess('pricing'))
  const showPricing = computed(
    () =>
      pricingAccess.value.enabled &&
      (!pricingAccess.value.requireAuth || auth.isAuthenticated)
  )
  const docsNavigation = computed(() => {
    const access = moduleAccess('docs')
    const reason = !access.enabled
      ? 'nav.disabledConfigured'
      : access.requireAuth && !auth.isAuthenticated
        ? 'nav.loginRequired'
        : !status.value.docs_link?.trim()
          ? 'nav.notConfigured'
          : !docsLink.value
            ? 'nav.invalidLink'
            : undefined
    return {
      href: reason ? undefined : docsLink.value,
      disabled: Boolean(reason),
      disabledReason: reason,
    }
  })
  const showDocs = computed(() => !docsNavigation.value.disabled)
  const showAbout = computed(() => moduleAccess('about').enabled)
  const sidebarModules = computed(() =>
    parseSidebarModules(status.value.SidebarModulesAdmin)
  )
  // Upstream web/src/hooks/use-sidebar-config.ts fills each missing section
  // and module from defaults that are all true, then shows a module only when
  // its section is enabled and the module is exactly true.
  function isSidebarModuleEnabled(section: string, module: string): boolean {
    const config = sidebarModules.value[section]
    if (!isRecord(config)) return true
    const enabled = config.enabled === undefined ? true : config.enabled
    const value = config[module] === undefined ? true : config[module]
    return Boolean(enabled) && value === true
  }
  const userAgreementEnabled = computed(() =>
    Boolean(status.value.user_agreement_enabled)
  )
  const privacyPolicyEnabled = computed(() =>
    Boolean(status.value.privacy_policy_enabled)
  )
  const registerEnabled = computed(() =>
    parseBoolean(status.value.register_enabled, true)
  )
  const passwordRegisterEnabled = computed(() =>
    parseBoolean(status.value.password_register_enabled, true)
  )
  const checkinEnabled = computed(() => status.value.checkin_enabled === true)
  // The backend serves /api/uptime/status whenever groups are configured, so
  // the administrator's panel switch is enforced here. A missing field keeps
  // the backend default (enabled).
  const uptimeEnabled = computed(() =>
    parseBoolean(status.value.uptime_kuma_enabled, true)
  )
  const turnstileEnabled = computed(() =>
    parseBoolean(status.value.turnstile_check, false)
  )
  const turnstileSiteKey = computed(
    () => status.value.turnstile_site_key?.trim() || ''
  )
  const modelCountLabel = computed(() =>
    modelCount.value === null
      ? i18n.global.t(`hero.metrics.${modelCountState.value}`)
      : String(modelCount.value)
  )
  const uptimeLabel = computed(() =>
    uptimePercent.value === null ? '--' : `${uptimePercent.value.toFixed(2)}%`
  )
  const versionLabel = computed(() => version.value || '--')

  function featureStatus(
    feature: string | undefined,
    fallback: FeatureStatus = 'disabled'
  ): FeatureStatus {
    if (!feature) return fallback
    return status.value.frontend_capabilities?.[feature] ?? fallback
  }

  function isFeatureEnabled(
    feature: string | undefined,
    fallback: FeatureStatus = 'disabled'
  ): boolean {
    return featureStatus(feature, fallback) !== 'disabled'
  }

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

  // Every /api/status read publishes here, so the navigation and the public
  // route guards share one snapshot. A slower earlier response never replaces
  // a newer one.
  async function refreshStatus(): Promise<void> {
    const request = ++statusRequested
    // A refresh must not reuse a GET started before an option was saved.
    // Give every call its own signal to bypass the API client's GET sharing.
    const next = await publicApi.status(new AbortController().signal)
    if (request < statusApplied) return
    statusApplied = request
    status.value = next
    configureQuotaDisplay(next)
    statusReachable.value = true
    applyBranding()
  }

  // The first navigation starts together with initialization, so that read is
  // already as fresh as the navigation; a cold public visit reuses it.
  function statusAtStartup(): Promise<void> {
    return startupStatus ?? refreshStatus()
  }

  async function runInitialization(): Promise<void> {
    phase.value = 'loading'
    lastError.value = null
    modelCount.value = null
    uptimePercent.value = null
    try {
      startupStatus = refreshStatus()
      await startupStatus

      const summaries = await Promise.allSettled([
        publicApi.notice(),
        loadModelCount(),
        uptimeEnabled.value ? publicApi.uptime() : Promise.resolve([]),
      ])
      const [noticeResult, , uptimeResult] = summaries

      if (noticeResult.status === 'fulfilled') {
        notice.value = toPlainText(noticeResult.value)
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
            (measured.reduce(
              (sum, monitor) => sum + Math.min(1, Math.max(0, monitor.uptime)),
              0
            ) /
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
      modelCountState.value = 'error'
    }
  }

  function initialize(): Promise<void> {
    if (phase.value === 'ready' || phase.value === 'degraded') {
      return Promise.resolve()
    }
    if (initializationPromise) return initializationPromise

    initializationPromise = runInitialization().finally(() => {
      initializationPromise = null
    })
    return initializationPromise
  }

  async function loadModelCount(): Promise<void> {
    pricingRequest.cancel()
    modelCount.value = null
    modelCountState.value = 'loading'
    if (!statusReachable.value) {
      modelCountState.value = phase.value === 'error' ? 'error' : 'loading'
      return
    }
    if (!pricingAccess.value.enabled) {
      modelCountState.value = 'notPublic'
      return
    }
    if (!auth.checked) return
    if (pricingAccess.value.requireAuth && !auth.isAuthenticated) {
      modelCountState.value = 'signIn'
      return
    }
    const result = await pricingRequest.run((signal) =>
      publicCatalogApi.pricing(signal, auth.isAuthenticated)
    )
    if (result.stale) return
    if (!result.ok) {
      modelCountState.value = 'error'
      throw result.error
    }
    if (result.value.issues.length > 0) {
      modelCountState.value = 'error'
      invalidResponse('/api/pricing')
    }
    modelCountState.value = 'ready'
    modelCount.value = result.value.models.length
  }

  watch(
    [
      () => auth.user,
      () => auth.checked,
      () => statusReachable.value,
      () => pricingAccess.value.enabled,
      () => pricingAccess.value.requireAuth,
    ],
    (next, previous) => {
      if (phase.value === 'loading' && next[2] && !previous[2]) return
      void loadModelCount().catch((error: unknown) => {
        lastError.value = error
        if (initialized.value) phase.value = 'degraded'
      })
    },
    { flush: 'sync' }
  )

  async function retry(): Promise<void> {
    if (phase.value !== 'error') return
    await initialize()
  }

  return {
    status,
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
    docsNavigation,
    showPricing,
    showDocs,
    showAbout,
    moduleAccess,
    registerEnabled,
    passwordRegisterEnabled,
    checkinEnabled,
    turnstileEnabled,
    turnstileSiteKey,
    userAgreementEnabled,
    privacyPolicyEnabled,
    modelCountLabel,
    modelCountState,
    uptimeLabel,
    uptimePercent,
    versionLabel,
    startTime,
    featureStatus,
    isFeatureEnabled,
    isSidebarModuleEnabled,
    initialize,
    refreshStatus,
    statusAtStartup,
    retry,
  }
})
