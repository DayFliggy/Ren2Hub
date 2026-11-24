import { ApiError } from '@/api/types'
import { errorMessage } from '@/api/clientError'
import {
  UpstreamRequestError,
  type UpstreamAccount,
  type UpstreamRemoteKey,
} from '@/api/upstreams'

type Translate = (key: string, values?: Record<string, unknown>) => string
type HasKey = (key: string) => boolean
export type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

const ACCOUNT_TONES: Record<string, Tone> = {
  active: 'success',
  disabled: 'neutral',
  reauth_required: 'warning',
  credential_undecryptable: 'danger',
  rate_limited: 'warning',
  error: 'danger',
}

const KEY_TONES: Record<string, Tone> = {
  active: 'success',
  inactive: 'neutral',
  expired: 'warning',
  exhausted: 'warning',
  missing: 'danger',
}

export function accountTone(status: string): Tone {
  return ACCOUNT_TONES[status] ?? 'info'
}

export function accountStatusText(status: string, t: Translate, te: HasKey) {
  const key = `upstreams.statuses.${status}`
  return te(key) ? t(key) : t('upstreams.statuses.unknown', { status })
}

export function keyTone(status: string): Tone {
  return KEY_TONES[status] ?? 'info'
}

export function keyStatusText(status: string, t: Translate, te: HasKey) {
  const key = `upstreams.keys.statuses.${status}`
  return te(key) ? t(key) : t('upstreams.keys.statuses.unknown', { status })
}

/** Business codes map to translated text; unknown codes keep the server text. */
export function upstreamErrorText(error: unknown, t: Translate, te: HasKey) {
  let text = errorMessage(error, t)
  if (error instanceof ApiError && error.code) {
    const key = `upstreams.errors.${error.code}`
    if (te(key)) text = t(key)
  }
  if (error instanceof UpstreamRequestError && error.retryAfter !== null)
    text = `${text} ${t('upstreams.retryAfter', { seconds: error.retryAfter })}`
  return text
}

const CURRENCY_SYMBOLS: Record<string, string> = { USD: '$', CNY: '¥' }

/** NewAPI balance in the upstream's own display unit, when it gave one. */
function convertedNewAPIBalance(meta: UpstreamAccount['site_meta']) {
  const perUnit = meta.quota_per_unit
  const mode = meta.quota_display_type
  if (!perUnit || !['USD', 'CNY', 'CUSTOM'].includes(mode)) return null
  const rate =
    mode === 'USD'
      ? 1
      : mode === 'CNY'
        ? meta.usd_exchange_rate
        : meta.custom_currency_exchange_rate
  if (!rate) return null
  const value = ((Number(meta.balance) / perUnit) * rate).toFixed(2)
  const symbol =
    mode === 'CUSTOM'
      ? meta.custom_currency_symbol || '¤'
      : CURRENCY_SYMBOLS[mode]
  return { symbol, value }
}

export interface BalanceText {
  text: string
  hint: string
}

export function balanceText(
  account: UpstreamAccount,
  t: Translate
): BalanceText {
  const meta = account.site_meta
  if (meta.balance === '')
    return { text: t('upstreams.balance.none'), hint: '' }
  if (account.platform === 'sub2api')
    return {
      text: t('upstreams.balance.sub2api', { value: meta.balance }),
      hint: t('upstreams.balanceHint.sub2api'),
    }
  const converted = convertedNewAPIBalance(meta)
  if (!converted)
    return {
      text: t('upstreams.balance.newapi', { value: meta.balance }),
      hint: t('upstreams.balanceHint.newapi'),
    }
  return {
    text: t('upstreams.balance.converted', converted),
    hint: t('upstreams.balanceHint.converted', { raw: meta.balance }),
  }
}

export function keyQuotaText(key: UpstreamRemoteKey, t: Translate): string {
  if (key.unlimited_quota === true) return t('upstreams.keys.unlimited')
  if (key.remain_quota !== null)
    return t('upstreams.keys.newapiQuota', {
      remain: key.remain_quota,
      used: key.used_quota ?? 0,
    })
  if (key.quota === null) return '—'
  if (key.quota === 0)
    return key.quota_used
      ? t('upstreams.keys.sub2apiUsed', { used: key.quota_used })
      : t('upstreams.keys.unlimited')
  return t('upstreams.keys.sub2apiQuota', {
    used: key.quota_used ?? 0,
    limit: key.quota,
  })
}

export function keyExpiryText(
  key: UpstreamRemoteKey,
  t: Translate,
  formatTime: (epochSec: number) => string
): string {
  if (key.expired_time !== null)
    return key.expired_time === -1
      ? t('upstreams.keys.never')
      : formatTime(key.expired_time)
  if (key.expires_at === null) return '—'
  if (key.expires_at === '') return t('upstreams.keys.never')
  const ms = Date.parse(key.expires_at)
  return Number.isNaN(ms) ? key.expires_at : formatTime(Math.floor(ms / 1000))
}

export function utf8Bytes(value: string): number {
  return new TextEncoder().encode(value).length
}

export function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

export function isProxyUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return (
      url.protocol === 'http:' ||
      url.protocol === 'https:' ||
      url.protocol === 'socks5:' ||
      url.protocol === 'socks5h:'
    )
  } catch {
    return false
  }
}
