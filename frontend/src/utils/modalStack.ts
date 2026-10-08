const CLOSING = 'data-dialog-closing'
const MODAL_DIALOG = `[role="dialog"][aria-modal="true"]:not([${CLOSING}])`

/**
 * Takes a dialog out of the stack as soon as it is closed. Its element stays
 * in the DOM for the leave transition and would otherwise keep Escape and Tab
 * from the dialog underneath until the animation ends. Reopening mounts a new
 * element, so the mark never needs clearing.
 */
export function markModalDialogClosing(dialog: Element | null): void {
  dialog?.setAttribute(CLOSING, '')
}

/**
 * z-index of the stacking layer a dialog paints in: its own value, or that of
 * the nearest ancestor that sets one (a drawer may mark an inner panel as the
 * dialog while its fixed wrapper carries the layer). Unlayered counts as 0.
 */
function paintZIndex(dialog: Element): number {
  for (let node: Element | null = dialog; node; node = node.parentElement) {
    const zIndex = Number.parseInt(getComputedStyle(node).zIndex, 10)
    if (!Number.isNaN(zIndex)) return zIndex
  }
  return 0
}

/**
 * Whether `dialog` is the modal dialog painted above every other one, so only
 * it should handle global Escape and Tab. Teleported dialogs sit in the DOM in
 * mount order, not open order, so a dialog opened later can precede the one it
 * covers. Paint order decides instead: the highest z-index wins and ties go to
 * the later DOM node, matching how the browser stacks them.
 */
export function isTopmostModalDialog(dialog: Element | null): boolean {
  if (!dialog) return false
  let topmost: Element | null = null
  let topmostZIndex = -Infinity
  for (const candidate of document.querySelectorAll(MODAL_DIALOG)) {
    const zIndex = paintZIndex(candidate)
    if (zIndex >= topmostZIndex) {
      topmost = candidate
      topmostZIndex = zIndex
    }
  }
  return topmost === dialog
}
