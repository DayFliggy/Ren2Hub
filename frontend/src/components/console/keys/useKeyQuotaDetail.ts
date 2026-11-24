import { onScopeDispose, shallowRef } from 'vue'

import type { TokenSummary } from '@/types/console'

/** Sweeping the pointer across rows should not flash a popover per row. */
const OPEN_DELAY_MS = 150
/** Bridges the gap between adjacent rows without leaving a stale popover. */
const CLOSE_DELAY_MS = 120

/**
 * State for the page's single key-detail popover: which key it shows, the ring
 * it is anchored to, and whether a tap or key press pinned it. Hover waits a
 * beat; once a popover is open, moving to another row switches it at once.
 * Only a pinned popover takes the pointer, so it can absorb the tap that
 * dismisses it.
 */
export function useKeyQuotaDetail() {
  const token = shallowRef<TokenSummary | null>(null)
  const anchor = shallowRef<HTMLElement | null>(null)
  const pinned = shallowRef(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let pendingId: number | null = null

  function cancel() {
    clearTimeout(timer)
    timer = undefined
    pendingId = null
  }

  function show(next: TokenSummary, ring: HTMLElement, pin = false) {
    cancel()
    token.value = next
    anchor.value = ring
    pinned.value = pin
  }

  /** Close it, or only when it still shows `forToken`. */
  function hide(forToken?: TokenSummary) {
    if (forToken && token.value?.id !== forToken.id) return
    cancel()
    token.value = null
    anchor.value = null
    pinned.value = false
  }

  function toggle(next: TokenSummary, ring: HTMLElement) {
    if (token.value?.id === next.id) hide()
    else show(next, ring, true)
  }

  function hover(next: TokenSummary, ring: HTMLElement) {
    if (token.value?.id === next.id) return cancel()
    if (pendingId === next.id) return
    if (token.value) return show(next, ring)
    cancel()
    pendingId = next.id
    timer = setTimeout(() => show(next, ring), OPEN_DELAY_MS)
  }

  /** The pointer left the rows, or reached a cell the popover must not cover. */
  function leave(immediate = false) {
    cancel()
    if (!token.value || pinned.value) return
    if (immediate) hide()
    else timer = setTimeout(() => hide(), CLOSE_DELAY_MS)
  }

  onScopeDispose(cancel)
  return { token, anchor, pinned, show, hide, toggle, hover, leave }
}
