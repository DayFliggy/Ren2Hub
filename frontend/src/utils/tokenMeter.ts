import type { TokenSummary } from '@/types/console'
import { formatDate } from './format'
import { quotaMeter, quotaMeterColor } from './quotaMeter'
import { tokenStatusLabel } from './tokenStatus'

/**
 * One state per key. A key that cannot be used reads as blocked before
 * anything about its balance; a key someone switched off stays quiet even when
 * its balance is also spent.
 */
export type TokenMeterState =
  'disabled' | 'expired' | 'exhausted' | 'low' | 'unlimited' | 'normal'

export interface TokenMeter {
  state: TokenMeterState
  /** Arc length 0-100. Any spend keeps a visible sliver. */
  arcPercent: number
  /** Exact consumed share for text; null for an unlimited key. */
  usedPercent: number | null
  remainingPercent: number | null
  /** Whole days left when a usable key expires within a week. */
  expiresInDays: number | null
  ringColor: string
  /** Blocked keys keep their balance readable but visibly out of service. */
  remainingColor: string
}

export interface TokenMeterText {
  key: string
  params?: Record<string, string | number>
  /** Only the part that needs attention takes a colour. */
  tone?: 'warning' | 'danger'
}

type MeterToken = Pick<
  TokenSummary,
  'status' | 'unlimited' | 'remain_quota' | 'used_quota' | 'expired_time'
>

const DAY_SECONDS = 86_400
const EXPIRY_SOON_DAYS = 7
const MIN_ARC_PERCENT = 5
// Faded so a blocked ring never reads like the healthy --signal ring.
const BLOCKED_COLOR =
  'color-mix(in srgb, var(--text-tertiary) 45%, transparent)'

function meterState(
  token: MeterToken,
  quotaState: string | null,
  nowSec: number
): TokenMeterState {
  if (token.status === 3) return 'expired'
  if (token.status === 4) return 'exhausted'
  // Unknown statuses cannot serve requests either; tokenStatus labels them.
  if (token.status !== 1) return 'disabled'
  // Same test the backend applies before it flips the status lazily.
  if (token.expired_time >= 0 && token.expired_time < nowSec) return 'expired'
  if (quotaState === 'exhausted') return 'exhausted'
  if (quotaState === 'low') return 'low'
  return token.unlimited ? 'unlimited' : 'normal'
}

function remainingColor(state: TokenMeterState): string {
  if (state === 'exhausted') return 'var(--status-danger-text)'
  if (state === 'disabled') return 'var(--text-secondary)'
  if (state === 'expired') return 'var(--text-tertiary)'
  return 'var(--text-primary)'
}

function ringColor(state: TokenMeterState, urgent: boolean): string {
  if (state === 'disabled' || state === 'expired') return BLOCKED_COLOR
  if (state === 'exhausted') return quotaMeterColor('exhausted')
  return quotaMeterColor(urgent ? 'low' : 'normal')
}

export function tokenMeter(token: MeterToken, nowSec: number): TokenMeter {
  const quota = token.unlimited
    ? null
    : quotaMeter(token.remain_quota, token.used_quota)
  const state = meterState(token, quota?.state ?? null, nowSec)
  const usable = state !== 'disabled' && state !== 'expired'
  const secondsLeft = token.expired_time - nowSec
  const expiresInDays =
    usable &&
    token.expired_time >= 0 &&
    secondsLeft <= EXPIRY_SOON_DAYS * DAY_SECONDS
      ? Math.max(1, Math.ceil(secondsLeft / DAY_SECONDS))
      : null
  const urgent =
    state === 'low' || (state !== 'exhausted' && expiresInDays !== null)

  // Blocked keys draw a whole faded ring behind their centre mark.
  let arcPercent = 100
  if (quota && usable && state !== 'exhausted') {
    arcPercent =
      quota.percent > 0 ? Math.max(MIN_ARC_PERCENT, quota.percent) : 0
  }

  return {
    state,
    arcPercent,
    // Keep non-exhausted usage below 100% so active rings never read as spent
    // and avoid crowding 4-character text inside the 36px mobile ring.
    usedPercent: quota
      ? state === 'exhausted'
        ? 100
        : Math.min(99, Math.round(quota.percent))
      : null,
    remainingPercent: quota ? quota.remainingPercent : null,
    expiresInDays,
    ringColor: ringColor(state, urgent),
    remainingColor: remainingColor(state),
  }
}

/** What a large ring shows in its centre; blocked rings draw a mark instead. */
export function tokenMeterCentre(meter: TokenMeter): string {
  if (meter.state === 'exhausted') return ''
  if (meter.state === 'disabled' || meter.state === 'expired') return ''
  return meter.usedPercent === null ? '∞' : `${meter.usedPercent}%`
}

/** The status label a key carries in the detail popover. */
export function tokenMeterLabel(
  token: Pick<TokenSummary, 'status'>,
  meter: TokenMeter
): string {
  if (meter.state === 'expired') return 'keys.statusExpired'
  if (meter.state === 'exhausted') return 'keys.statusExhausted'
  if (meter.state === 'disabled') return tokenStatusLabel(token.status)
  return 'common.enabled'
}

function expiryText(
  token: Pick<TokenSummary, 'expired_time'>,
  meter: TokenMeter
): TokenMeterText {
  if (meter.expiresInDays !== null) {
    return {
      key: 'keys.meter.expiresInDays',
      params: { days: meter.expiresInDays },
      tone: 'warning',
    }
  }
  if (token.expired_time < 0) return { key: 'common.never' }
  return {
    key: 'keys.meter.expiresOn',
    params: { date: formatDate(token.expired_time) },
  }
}

/**
 * The one-line note under a key's name: what blocks it, otherwise what is
 * about to run out, otherwise when it expires.
 */
export function tokenMeterNote(
  token: Pick<TokenSummary, 'status' | 'expired_time'>,
  meter: TokenMeter
): TokenMeterText[] {
  if (meter.state === 'disabled') {
    return [{ key: tokenStatusLabel(token.status) }]
  }
  if (meter.state === 'expired') {
    if (token.expired_time < 0) return [{ key: 'keys.statusExpired' }]
    return [
      {
        key: 'keys.meter.expiredOn',
        params: { date: formatDate(token.expired_time) },
      },
    ]
  }
  if (meter.state === 'exhausted') {
    return [{ key: 'keys.statusExhausted', tone: 'danger' }]
  }
  if (meter.state !== 'low') return [expiryText(token, meter)]
  return [
    {
      key: 'keys.meter.onlyLeft',
      params: { percent: meter.remainingPercent ?? 0 },
      tone: 'warning',
    },
    expiryText(token, meter),
  ]
}
