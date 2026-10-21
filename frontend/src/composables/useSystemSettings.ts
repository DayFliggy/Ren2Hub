import { ref, readonly } from 'vue'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import i18n from '@/i18n'
import {
  SYSTEM_SETTINGS_DEFAULTS,
  type AllSystemSettings,
  type SystemOption,
  type SystemOptionDescriptor,
} from '@/types/systemSettings'

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

/** Parse a raw string value into boolean / number / string based on the default key type. */
function castValue(
  key: string,
  raw: string,
  defaults: AllSystemSettings
): string | boolean | number {
  const expected = defaults[key as keyof AllSystemSettings]
  if (typeof expected === 'boolean') {
    if (!['true', 'false', '1', '0'].includes(raw))
      throw new Error(`Invalid boolean option: ${key}`)
    return raw === 'true' || raw === '1'
  }
  if (typeof expected === 'number') {
    const n = Number(raw)
    if (!raw.trim() || !Number.isFinite(n))
      throw new Error(`Invalid numeric option: ${key}`)
    return n
  }
  return raw
}

/** Merge a flat array of SystemOption into the typed settings object. */
function parseOptions(
  options: SystemOption[],
  defaults: AllSystemSettings,
  descriptors: SystemOptionDescriptor[] = []
): AllSystemSettings {
  const result = { ...defaults }
  const descriptorKeys = new Set(
    descriptors.map((descriptor) => descriptor.key)
  )
  for (const opt of options) {
    // Keep descriptor-backed options even when the Vue type snapshot has not
    // declared a dedicated property yet. This lets integration and plugin
    // settings retain their authoritative runtime value instead of silently
    // disappearing from the shared settings cache.
    if (
      Object.prototype.hasOwnProperty.call(defaults, opt.key) ||
      descriptorKeys.has(opt.key)
    ) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(result as any)[opt.key] = castValue(opt.key, opt.value, defaults)
    }
  }
  return result
}

// Module-level singleton so multiple consumers share the same fetch
const _settings = ref<AllSystemSettings>({ ...SYSTEM_SETTINGS_DEFAULTS })
const _rawOptions = ref<SystemSettingsRawOptions>({})
const _configuredSecrets = ref<string[]>([])
const _catalog = ref<SystemOptionDescriptor[]>([])
const _loading = ref(false)
const _loaded = ref(false)
const _saving = ref(false)
let _fetchPromise: Promise<boolean> | null = null
let _savePromise: Promise<boolean> | null = null

function isSensitiveOption(
  key: string,
  descriptors: readonly SystemOptionDescriptor[] = _catalog.value
): boolean {
  return (
    descriptors.some((item) => item.key === key && item.sensitive) ||
    /(?:Token|Secret|Key|Cert|Certificate|Password|secret|api_key)$/.test(key)
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
        const descriptors: SystemOptionDescriptor[] = []
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
          if (!isSensitiveOption(option.key, descriptors))
            raw[option.key] = option.value
        }
        // Publish options and credential availability together. A failed
        // refresh must not pair new values with stale secret status.
        const settings = parseOptions(
          Object.entries(raw).map(([key, value]) => ({ key, value })),
          SYSTEM_SETTINGS_DEFAULTS,
          descriptors
        )
        _rawOptions.value = raw
        _catalog.value = descriptors.map((item) =>
          isSensitiveOption(item.key, descriptors)
            ? { ...item, default_value: '' }
            : item
        )
        _settings.value = settings
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

  async function updateOption(
    key: string,
    value: SystemSettingValue
  ): Promise<boolean> {
    return saveOptions({ [key]: value })
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
        _settings.value = parseOptions(
          Object.entries(_rawOptions.value).map(([key, value]) => ({
            key,
            value,
          })),
          SYSTEM_SETTINGS_DEFAULTS
        )
        return true
      } catch (err) {
        const error = err instanceof Error ? err.message : String(err)
        toast.error(
          applied
            ? i18n.global.t('systemSettings.partialSaveFailed', {
                count: applied,
                error,
              })
            : error
        )
        return false
      } finally {
        _settings.value = parseOptions(
          Object.entries(_rawOptions.value).map(([key, value]) => ({
            key,
            value,
          })),
          SYSTEM_SETTINGS_DEFAULTS
        )
        _saving.value = false
        _savePromise = null
      }
    })()
    return _savePromise
  }

  function rawValue(
    key: string,
    fallback: SystemSettingValue = ''
  ): SystemSettingValue {
    if (isSensitiveOption(key)) return ''
    const value = _rawOptions.value[key]
    const descriptor = _catalog.value.find((item) => item.key === key)
    if (value === undefined) {
      const defaultValue = descriptor?.default_value
      if (
        typeof fallback === 'string' &&
        descriptor?.value_type === 'json' &&
        defaultValue !== undefined
      ) {
        return typeof defaultValue === 'string'
          ? defaultValue
          : JSON.stringify(defaultValue)
      }
      if (typeof defaultValue === typeof fallback) {
        return defaultValue as SystemSettingValue
      }
      return fallback
    }
    if (typeof fallback === 'boolean') return value === 'true' || value === '1'
    if (typeof fallback === 'number') {
      const parsed = Number(value)
      return Number.isFinite(parsed) ? parsed : fallback
    }
    return value
  }

  function isSecretConfigured(key: string): boolean {
    return _configuredSecrets.value.includes(key)
  }

  return {
    settings: readonly(_settings),
    rawOptions: readonly(_rawOptions),
    configuredSecrets: readonly(_configuredSecrets),
    catalog: readonly(_catalog),
    loading: readonly(_loading),
    loaded: readonly(_loaded),
    saving: readonly(_saving),
    load,
    updateOption,
    saveOptions,
    rawValue,
    isSecretConfigured,
  }
}
