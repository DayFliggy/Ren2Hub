import { ref, watch, type Ref } from 'vue'
import { useResizeObserver } from '@vueuse/core'

import {
  registerFloatingPanel,
  tabbableElements,
  unregisterFloatingPanel,
} from '@/utils/focusable'

export interface FloatingPanelOptions {
  /** Preferred side; the panel flips when only the other side has room. */
  placement?: () => 'down' | 'up'
  /** Which anchor edge the panel lines up with before viewport clamping. */
  align?: 'start' | 'end'
  /** 'exact' matches the anchor width, 'min' never gets narrower than it. */
  width?: 'exact' | 'min' | 'content'
  /** Distance between anchor and panel in px. */
  gap?: number
}

const VIEWPORT_MARGIN = 8

/** Rendered but invisible until the first placement is measured. */
const HIDDEN_STYLE = {
  position: 'fixed',
  top: '0px',
  left: '0px',
  visibility: 'hidden',
}

/**
 * Places a panel teleported to <body> against its anchor with fixed
 * positioning, so scrolling or rounded containers cannot clip it. The panel
 * stays inside the viewport, flips upward when there is more room above, and
 * shrinks to the larger side when neither fits.
 *
 * Scrolling, resizing and content size changes reposition an open panel
 * instead of closing it: a select inside a scrolling queue, a dialog or above
 * a mobile keyboard must stay usable while the page moves. Menus that act on
 * a single row (ActionMenu) keep their close-on-scroll behaviour.
 *
 * Placement is synchronous. The first measurement happens while the panel is
 * hidden; later ones read the content height from scrollHeight, so the
 * visible panel never passes through an intermediate position and keeps its
 * own scroll offset.
 */
export function useFloatingPanel(
  anchor: Ref<HTMLElement | null>,
  panel: Ref<HTMLElement | null>,
  open: Ref<boolean>,
  options: FloatingPanelOptions = {}
) {
  const style = ref<Record<string, string>>({ ...HIDDEN_STYLE })
  /** The anchor's hand-drawn scope, for the panel's data-handdrawn-scope.
      Light-theme sketch rules in base.css only apply inside a scope, and the
      teleported panel has left the anchor's subtree. */
  const scope = ref<string | undefined>()
  const gap = options.gap ?? 8
  /** The panel's own CSS max-height, read once per opening before any
      inline cap is applied. */
  let classMaxHeight = Infinity

  function naturalHeight(element: HTMLElement): number {
    const borders = element.offsetHeight - element.clientHeight
    return Math.min(element.scrollHeight + borders, classMaxHeight)
  }

  function update(): void {
    const element = panel.value
    if (!open.value || !anchor.value || !element) return
    const rect = anchor.value.getBoundingClientRect()
    // 'exact' panels take the anchor width, so clamp with that width now.
    const width = options.width === 'exact' ? rect.width : element.offsetWidth
    const height = naturalHeight(element)

    const below = window.innerHeight - rect.bottom - gap - VIEWPORT_MARGIN
    const above = rect.top - gap - VIEWPORT_MARGIN
    const preferUp = options.placement?.() === 'up'
    const fits = height <= (preferUp ? above : below)
    const up = fits ? preferUp : above > below
    const room = Math.max(0, up ? above : below)
    const shown = Math.min(height, room)

    const ideal = options.align === 'end' ? rect.right - width : rect.left
    const maxLeft = window.innerWidth - width - VIEWPORT_MARGIN
    const next: Record<string, string> = {
      position: 'fixed',
      zIndex: layerAbove(anchor.value),
      top: `${up ? rect.top - gap - shown : rect.bottom + gap}px`,
      left: `${Math.max(VIEWPORT_MARGIN, Math.min(ideal, maxLeft))}px`,
    }
    if (options.width === 'exact') next.width = `${rect.width}px`
    if (options.width === 'min') next.minWidth = `${rect.width}px`
    // Neither side fits: cap the panel to the roomier side and let it scroll.
    if (height > room)
      Object.assign(next, { maxHeight: `${room}px`, overflowY: 'auto' })
    style.value = next
  }

  function onScroll(event: Event) {
    // Scrolling the panel's own list does not move its anchor.
    if (panel.value?.contains(event.target as Node)) return
    update()
  }

  watch(
    open,
    (isOpen, _, onCleanup) => {
      // Closing keeps the last placement so the leave transition plays in place.
      if (!isOpen || !anchor.value || !panel.value) return
      style.value = { ...HIDDEN_STYLE }
      const element = panel.value
      registerFloatingPanel(element, anchor.value)
      scope.value =
        anchor.value
          .closest('[data-handdrawn-scope]')
          ?.getAttribute('data-handdrawn-scope') ?? undefined
      // A reopened panel mounts with the previous placement, which may carry
      // an inline cap; clear it so this reads the class's own max-height.
      // The next render rewrites every inline key from style.value.
      element.style.maxHeight = ''
      classMaxHeight = parseFloat(getComputedStyle(element).maxHeight)
      if (Number.isNaN(classMaxHeight)) classMaxHeight = Infinity
      update()
      // Listeners exist only while the panel is open.
      const listen = { capture: true, passive: true }
      window.addEventListener('scroll', onScroll, listen)
      window.addEventListener('resize', update, { passive: true })
      onCleanup(() => {
        unregisterFloatingPanel(element)
        window.removeEventListener('scroll', onScroll, listen)
        window.removeEventListener('resize', update)
      })
    },
    { flush: 'post' }
  )
  // The panel ref is null while closed, so this observes only an open panel.
  useResizeObserver(panel, update)

  return { style, scope, update }
}

/**
 * The popover layer, raised above the modal that owns the anchor when that
 * modal uses a higher z-index than the shared modal layer (for example a
 * security proof stacked over a form).
 */
function layerAbove(anchor: HTMLElement): string {
  const modal = anchor.closest<HTMLElement>('[aria-modal="true"]')
  const modalZ = modal ? parseInt(getComputedStyle(modal).zIndex, 10) : NaN
  if (Number.isNaN(modalZ)) return 'var(--z-popover)'
  return `max(var(--z-popover), ${modalZ + 1})`
}

/**
 * A teleported panel is no longer next to its trigger in tab order, so Tab
 * past either end of the panel would leave the page. Returns true when this
 * Tab should instead close the panel and hand focus back to the trigger.
 */
export function tabLeavesPanel(event: KeyboardEvent, panel: HTMLElement) {
  if (event.key !== 'Tab') return false
  const focusable = tabbableElements(panel)
  const edge = event.shiftKey ? focusable[0] : focusable.at(-1)
  return !edge || document.activeElement === edge
}
