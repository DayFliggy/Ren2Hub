import { ref, readonly } from 'vue'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import i18n from '@/i18n'
import type { SystemOption } from '@/types/systemSettings'

export type SystemSettingValue = string | boolean | number
export type SystemSettingsRawOptions = Record<string, string>

const dependentEnableKeys = new Set([
  'GitHubOAuthEnabled',
  'LinuxDOOAuthEnabled',
  'discord.enabled',
  'TelegramOAuthEnabled',
  'WeChatAuthEnabled',
  'oidc.enabled',
  'TurnstileCheckEnabled',
  'EmailDomainRestrictionEnabled',
])

// Read-only status flags the backend returns instead of saved secret values.
const secretStatusKeys = new Map([['ResendKeyConfigured', 'ResendApiKey']])

// Older installations persisted empty strings for JSON console-content
// options. Treat those values as the documented empty list so untouched
// fields remain valid and do not block saving the section.
const emptyJsonListOptions = new Set([
  'console_setting.announcements',
  'console_setting.api_info',
  'console_setting.faq',
  'console_setting.uptime_kuma_groups',
])

function savePhase(key: string, value: SystemSettingValue): number {
  if (!dependentEnableKeys.has(key)) return 1
  if (String(value) === 'false') return 0
  if (String(value) === 'true') return 2
  return 1
}

interface SaveOptionsCallbacks {
  onApplied?: (
    key: string,
    value: SystemSettingValue,
    sensitive: boolean
  ) => void
}

// Module-level singleton so multiple consumers share the same fetch
const _rawOptions = ref<SystemSettingsRawOptions>({})
const _configuredSecrets = ref<string[]>([])
const _loading = ref(false)
const _loaded = ref(false)
const _saving = ref(false)
const _statusRefreshing = ref(false)
const _statusRefreshError = ref('')
let _statusRefreshPromise: Promise<boolean> | null = null
let _fetchPromise: Promise<boolean> | null = null
let _savePromise: Promise<boolean> | null = null

function isSensitiveOption(key: string): boolean {
  return /(?:Token|Secret|Key|Cert|Certificate|Password|secret|api_key)$/.test(
    key
  )
}

export function useSystemSettings() {
  const toast = useToast()

  async function load(force = false): Promise<boolean> {
    if (_savePromise) await _savePromise
    if (_fetchPromise) {
      const loaded = await _fetchPromise
      // A post-save refresh must start after an earlier read has completed.
      return force ? load(true) : loaded
    }
    if (_loaded.value && !force) return true

    _loading.value = true
    // api.get<T> unwraps the ApiResponse envelope and returns .data directly
    // /api/option/ returns { success, message, data: SystemOption[] }
    // so T = SystemOption[]
    _fetchPromise = api
      .get<SystemOption[]>('/api/option/')
      .then(async (data) => {
        if (
          !Array.isArray(data) ||
          data.some(
            (option) =>
              typeof option?.key !== 'string' ||
              typeof option.value !== 'string'
          )
        )
          throw new Error('Invalid system settings response')
        const raw: SystemSettingsRawOptions = {}
        const configuredSecrets: string[] = []
        for (const option of data) {
          const secretKey = secretStatusKeys.get(option.key)
          if (secretKey) {
            // Status flags are never editable options, so keep them out of raw.
            if (!['true', 'false'].includes(option.value))
              throw new Error(`Invalid boolean option: ${option.key}`)
            if (option.value === 'true') configuredSecrets.push(secretKey)
            continue
          }
          if (!isSensitiveOption(option.key))
            raw[option.key] =
              emptyJsonListOptions.has(option.key) && option.value.trim() === ''
                ? '[]'
                : option.value
        }
        // Publish options and credential availability together. A failed
        // refresh must not pair new values with stale secret status.
        _rawOptions.value = raw
        _configuredSecrets.value = configuredSecrets
        _loaded.value = true
        return true
      })
      .catch((err) => {
        _loaded.value = false
        const msg = err instanceof ApiError ? err.message : String(err)
        toast.error(msg)
        return false
      })
      .finally(() => {
        _loading.value = false
        _fetchPromise = null
      })

    return _fetchPromise
  }

  // Committed settings and their public projection have different outcomes.
  // A failed status read must not invite the user to submit committed writes again.
  async function refreshStatus(afterCommit = false): Promise<boolean> {
    if (_statusRefreshPromise) {
      const refreshed = await _statusRefreshPromise
      return afterCommit ? refreshStatus() : refreshed
    }
    _statusRefreshing.value = true
    _statusRefreshPromise = (async () => {
      try {
        await useAppStore().refreshStatus()
        _statusRefreshError.value = ''
        return true
      } catch (error) {
        _statusRefreshError.value = i18n.global.t(
          'systemSettings.statusRefreshFailed',
          {
            error: error instanceof Error ? error.message : String(error),
          }
        )
        toast.error(_statusRefreshError.value)
        return false
      } finally {
        _statusRefreshing.value = false
        _statusRefreshPromise = null
      }
    })()
    return _statusRefreshPromise
  }

  /** Native options save independently; retain successful writes if a later key fails. */
  async function saveOptions(
    patch: Partial<Record<string, SystemSettingValue>>,
    callbacks: SaveOptionsCallbacks = {}
  ): Promise<boolean> {
    if (!_loaded.value || _loading.value || _saving.value) return false
    const entries = Object.entries(patch)
      .filter(
        (entry): entry is [string, SystemSettingValue] =>
          entry[1] !== undefined &&
          (!isSensitiveOption(entry[0]) || String(entry[1]).trim() !== '')
      )
      .sort(([a, av], [b, bv]) => savePhase(a, av) - savePhase(b, bv))
    if (entries.length === 0) return true
    _saving.value = true
    _savePromise = (async () => {
      let applied = 0
      let saved = true
      try {
        for (const [key, value] of entries) {
          await api.put('/api/option/', { key, value: String(value) })
          if (isSensitiveOption(key)) {
            _configuredSecrets.value = [
              ...new Set([..._configuredSecrets.value, key]),
            ]
          } else {
            _rawOptions.value = { ..._rawOptions.value, [key]: String(value) }
          }
          applied++
          callbacks.onApplied?.(key, value, isSensitiveOption(key))
        }
      } catch (err) {
        saved = false
        const error = err instanceof Error ? err.message : String(err)
        toast.error(
          applied
            ? i18n.global.t('systemSettings.partialSaveFailed', {
                count: applied,
                error,
              })
            : error
        )
      }
      try {
        // Refresh once for every successful batch, including a partially
        // committed batch. Status is the single authority for shared UI state.
        if (applied > 0) await refreshStatus(true)
      } finally {
        _saving.value = false
        _savePromise = null
      }
      return saved
    })()
    return _savePromise
  }

  function rawValue(
    key: string,
    fallback: SystemSettingValue = ''
  ): SystemSettingValue {
    if (isSensitiveOption(key)) return ''
    const value = _rawOptions.value[key]
    if (value === undefined) return fallback
    if (emptyJsonListOptions.has(key) && value.trim() === '') return '[]'
    // Invalid stored values stay raw so field validation reports them instead
    // of hiding them behind the default.
    if (typeof fallback === 'boolean') {
      if (value === 'true' || value === '1') return true
      if (value === 'false' || value === '0') return false
      return value
    }
    if (typeof fallback === 'number') {
      const parsed = Number(value)
      return value.trim() && Number.isFinite(parsed) ? parsed : value
    }
    return value
  }

  function isSecretConfigured(key: string): boolean {
    return _configuredSecrets.value.includes(key)
  }

  return {
    rawOptions: readonly(_rawOptions),
    loading: readonly(_loading),
    loaded: readonly(_loaded),
    saving: readonly(_saving),
    load,
    saveOptions,
    statusRefreshing: readonly(_statusRefreshing),
    statusRefreshError: readonly(_statusRefreshError),
    refreshStatus,
    rawValue,
    isSecretConfigured,
  }
}
