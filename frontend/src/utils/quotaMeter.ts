import { quotaInteger, quotaRatio, type Quota } from './quota'

export type QuotaMeterState = 'normal' | 'low' | 'exhausted'

export interface QuotaMeter {
  state: QuotaMeterState
  /** Consumed share of remaining + used, 0-100. */
  percent: number
  /** Remaining share, 0-100 — what the "only N% left" note reports. */
  remainingPercent: number
}

/** Below this remaining share the balance is worth attention. */
const QUOTA_LOW_REMAINING_RATIO = 0.1

/**
 * Soft-balance policy shared by user balances and per-key allowances.
 * Deliberately NOT CapacityMeter's policy. Channel capacity is a hard ceiling —
 * at 80% it is nearly unroutable, so it warns early. A balance is refilled by a
 * top-up or an edit, so 80% consumed means nothing; only "almost out" and "out"
 * are worth signalling. remaining + used is a display denominator only, not a
 * domain total.
 */
export function quotaMeter(
  remaining: Quota | number,
  used: Quota | number
): QuotaMeter {
  // The display denominator can exceed one account field's int64 range.
  const lifetime = quotaInteger(String(remaining)) + quotaInteger(String(used))
  const percent =
    lifetime > 0n
      ? Math.min(100, Math.max(0, quotaRatio(String(used), lifetime) * 100))
      : 0
  const remainingRatio =
    lifetime > 0n ? quotaRatio(String(remaining), lifetime) : 0

  // remaining <= 0 covers both a spent-out balance and one never funded; either
  // way the takeaway is identical — nothing can be spent.
  const state: QuotaMeterState =
    BigInt(remaining) <= 0n
      ? 'exhausted'
      : remainingRatio < QUOTA_LOW_REMAINING_RATIO
        ? 'low'
        : 'normal'

  return {
    state,
    percent,
    remainingPercent: Math.round(remainingRatio * 100),
  }
}

export function quotaMeterColor(state: QuotaMeterState): string {
  if (state === 'exhausted') return 'var(--status-danger)'
  if (state === 'low') return 'var(--status-warning)'
  return 'var(--signal)'
}
