import type {
  UpstreamAccount,
  UpstreamKeyInput,
  UpstreamPlatform,
  UpstreamRemoteKey,
} from '@/api/upstreams'
import type { SelectOption } from '@/components/common/FilterSelect.vue'
import { fromDateTimeLocal } from '@/utils/format'
import { utf8Bytes } from './upstreamText'

export const KEEP_GROUP = '__keep__'
const NEWAPI_NAME_MAX_BYTES = 50

export type ExpiryMode = 'keep' | 'never' | 'date' | 'days'
export type QuotaMode = 'keep' | 'unlimited' | 'limited'

export interface KeyForm {
  name: string
  /** NewAPI group name or Sub2API group ID text; '' = account default. */
  group: string
  expiryMode: ExpiryMode
  expiresAt: string
  days: string
  quotaMode: QuotaMode
  quota: string
}

export type KeyInputResult =
  | { input: UpstreamKeyInput }
  | { error: string; values?: Record<string, unknown> }

type Translate = (key: string) => string

export function defaultKeyForm(
  platform: UpstreamPlatform,
  key: UpstreamRemoteKey | null
): KeyForm {
  return {
    name: key?.name ?? '',
    group: key ? KEEP_GROUP : '',
    expiryMode: key ? 'keep' : 'never',
    expiresAt: '',
    days: platform === 'sub2api' ? '30' : '',
    quotaMode: key ? 'keep' : 'unlimited',
    quota: '',
  }
}

export function expiryModes(
  platform: UpstreamPlatform,
  editing: boolean
): ExpiryMode[] {
  if (editing) return ['keep', 'never', 'date']
  return platform === 'sub2api' ? ['never', 'days'] : ['never', 'date']
}

export function quotaModes(editing: boolean): QuotaMode[] {
  return editing ? ['keep', 'unlimited', 'limited'] : ['unlimited', 'limited']
}

export function groupOptions(
  account: UpstreamAccount,
  editing: boolean,
  t: Translate
): SelectOption[] {
  const first = editing
    ? { value: KEEP_GROUP, label: t('upstreams.keyForm.keepGroup') }
    : { value: '', label: t('upstreams.keyForm.defaultGroup') }
  const groups =
    account.platform === 'sub2api'
      ? account.upstream_groups
          .filter((group) => group.id !== null)
          .map((group) => ({ value: String(group.id), label: group.name }))
      : account.upstream_groups.map((group) => ({
          value: group.name,
          label: group.name,
        }))
  return [first, ...groups]
}

function nameError(
  platform: UpstreamPlatform,
  name: string
): KeyInputResult | null {
  if (!name) return { error: 'upstreams.keyForm.nameRequired' }
  const bytes = utf8Bytes(name)
  if (platform === 'newapi' && bytes > NEWAPI_NAME_MAX_BYTES)
    return { error: 'upstreams.keyForm.nameTooLong', values: { bytes } }
  return null
}

function futureSeconds(value: string): number | null {
  const seconds = fromDateTimeLocal(value)
  return seconds !== null && seconds > Date.now() / 1000 ? seconds : null
}

function applyExpiry(
  platform: UpstreamPlatform,
  form: KeyForm,
  input: UpstreamKeyInput
): string | null {
  if (form.expiryMode === 'keep') return null
  if (form.expiryMode === 'days') {
    if (!/^[1-9]\d{0,4}$/.test(form.days.trim()))
      return 'upstreams.keyForm.daysInvalid'
    input.expires_in_days = Number(form.days.trim())
    return null
  }
  if (form.expiryMode === 'never') {
    if (platform === 'newapi') input.expired_time = -1
    else input.expires_at = ''
    return null
  }
  const seconds = futureSeconds(form.expiresAt)
  if (seconds === null) return 'upstreams.keyForm.expiryInvalid'
  if (platform === 'newapi') input.expired_time = seconds
  else
    input.expires_at = new Date(seconds * 1000)
      .toISOString()
      .replace(/\.\d{3}Z$/, 'Z')
  return null
}

function applyQuota(
  platform: UpstreamPlatform,
  form: KeyForm,
  editing: boolean,
  input: UpstreamKeyInput
): string | null {
  if (form.quotaMode === 'keep') return null
  const text = form.quota.trim()
  if (platform === 'newapi') {
    input.unlimited_quota = form.quotaMode === 'unlimited'
    if (form.quotaMode === 'unlimited') return null
    if (!/^\d{1,15}$/.test(text)) return 'upstreams.keyForm.quotaInvalid'
    input.remain_quota = Number(text)
    return null
  }
  if (form.quotaMode === 'unlimited') {
    // Sub2API reads a quota of 0 as unlimited; a new key is unlimited by default.
    if (editing) input.quota = 0
    return null
  }
  const quota = Number(text)
  if (!/^\d+(?:\.\d+)?$/.test(text) || !Number.isFinite(quota))
    return 'upstreams.keyForm.sub2apiQuotaInvalid'
  input.quota = quota
  return null
}

/** Builds the KeyInput; on edit only changed or explicitly chosen fields. */
export function buildKeyInput(
  platform: UpstreamPlatform,
  form: KeyForm,
  key: UpstreamRemoteKey | null
): KeyInputResult {
  const input: UpstreamKeyInput = {}
  const name = form.name.trim()
  if (!key || name !== key.name) {
    const invalid = nameError(platform, name)
    if (invalid) return invalid
    input.name = name
  }
  if (form.group && form.group !== KEEP_GROUP) {
    if (platform === 'newapi') input.upstream_group = form.group
    else input.upstream_group_id = Number(form.group)
  }
  const expiryError = applyExpiry(platform, form, input)
  if (expiryError) return { error: expiryError }
  const quotaError = applyQuota(platform, form, key !== null, input)
  if (quotaError) return { error: quotaError }
  return { input }
}
