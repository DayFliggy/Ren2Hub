// Roving-tabindex members (tabindex="-1") are focusable but not Tab stops.
const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]',
]
  .map((selector) => `${selector}:not([tabindex="-1"])`)
  .join(',')

/**
 * Keyboard-reachable descendants in DOM order. Hidden elements (display:none
 * ancestors, such as a sm:flex column on a phone) have no offsetParent and are
 * skipped; the active element counts even when it has none.
 */
export function tabbableElements(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (element) =>
      element.offsetParent !== null || element === document.activeElement
  )
}

/**
 * Floating panels teleported to <body>, mapped to the anchor they were opened
 * from. The panels also carry data-floating-panel. A focus trap treats focus
 * inside a registered panel as its own when the panel's anchor is inside the
 * trap: the panel is part of a control the trap already owns, even though it
 * left the trap's subtree.
 */
const floatingPanels = new WeakMap<Element, HTMLElement>()

export function registerFloatingPanel(
  panel: HTMLElement,
  anchor: HTMLElement
): void {
  floatingPanels.set(panel, anchor)
}

export function unregisterFloatingPanel(panel: HTMLElement): void {
  floatingPanels.delete(panel)
}

/** Whether `target` is inside `container` or a floating panel opened from it. */
export function containsWithFloatingPanels(
  container: HTMLElement,
  target: Node | null
): boolean {
  if (!target) return false
  if (container.contains(target)) return true
  const element = target instanceof Element ? target : target.parentElement
  const panel = element?.closest('[data-floating-panel]')
  const anchor = panel ? floatingPanels.get(panel) : undefined
  return anchor ? containsWithFloatingPanels(container, anchor) : false
}
