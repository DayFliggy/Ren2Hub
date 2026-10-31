import type { SystemResourceState } from '@/api/systemStatus'

export type LoadTier = 'idle' | 'normal' | 'notice' | 'warning' | 'danger'

/**
 * A reading's fill for gauges, rings and segments, and its text colour. The
 * fills only reach 3:1 against the tile, so figures use the -text tokens, which
 * keep 4.5:1 in both themes (see the load ramp in tokens.css). The dashboard
 * tiles and the Root resource gauges share these, so one reading and verdict
 * takes one colour on both pages.
 */
export interface LoadTone {
  tier: LoadTier | 'unchecked' | 'unknown'
  fill: string
  text: string
}

export const UNKNOWN_TONE: LoadTone = {
  tier: 'unknown',
  fill: 'var(--text-tertiary)',
  text: 'var(--text-tertiary)',
}

/** A reading whose threshold check is off was not judged and takes no tier. */
const UNCHECKED_TONE: LoadTone = {
  tier: 'unchecked',
  fill: 'var(--load-unchecked)',
  text: 'var(--load-unchecked-text)',
}

export function loadTone(tier: LoadTier): LoadTone {
  return { tier, fill: `var(--load-${tier})`, text: `var(--load-${tier}-text)` }
}

/**
 * 未越过阈值的占比色阶（CPU / 内存 / 磁盘 / 负载格）：
 * - <= 30%: 空闲
 * - 30% ~ 60%: 正常
 * - > 60%: 关注
 * 警告、危险两档只来自服务端阈值判定，前端不再按固定比例告警。
 */
export function shareTone(percent: number | null): LoadTone {
  if (percent === null) return UNKNOWN_TONE
  if (percent > 60) return loadTone('notice')
  if (percent > 30) return loadTone('normal')
  return loadTone('idle')
}

/**
 * A measured reading under the server's verdict. Resource checks only produce
 * warning today; critical is a reserved contract value shown as danger. A
 * verdict that judges nothing (unknown, stale) leaves the reading neutral.
 */
export function verdictTone(
  verdict: SystemResourceState | undefined,
  percent: number | null
): LoadTone {
  if (percent === null) return UNKNOWN_TONE
  if (verdict === 'critical') return loadTone('danger')
  if (verdict === 'warning') return loadTone('warning')
  if (verdict === 'unchecked') return UNCHECKED_TONE
  if (verdict === 'ok') return shareTone(percent)
  return UNKNOWN_TONE
}
