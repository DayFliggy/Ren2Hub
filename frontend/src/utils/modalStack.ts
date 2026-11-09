/**
 * Open-order stack of modal dialogs, following Sub2API's BaseDialog. A dialog
 * registers when it opens and leaves as soon as it closes, while its element
 * may still be in the DOM for the leave transition, so the dialog underneath
 * gets Escape and Tab back immediately. The most recently opened dialog is the
 * topmost one; teleported dialogs sit in the DOM in mount order, so DOM order
 * cannot decide this.
 */
const openDialogs: symbol[] = []

export function openModalDialog(dialog: symbol): void {
  closeModalDialog(dialog)
  openDialogs.push(dialog)
}

export function closeModalDialog(dialog: symbol): void {
  const index = openDialogs.indexOf(dialog)
  if (index !== -1) openDialogs.splice(index, 1)
}

/** Whether `dialog` is the open dialog that handles global Escape and Tab. */
export function isTopmostModalDialog(dialog: symbol): boolean {
  return openDialogs.at(-1) === dialog
}

/**
 * Page scroll lock shared by every overlay, including the public navigation
 * drawer. A per-overlay snapshot restores "hidden" when overlays close out of
 * order and freezes the page, so only the first holder snapshots the overflow
 * and only the last release restores it. Each holder releases only a lock it
 * took, once.
 *
 * The root element is locked as well: base.css gives <html> its own
 * overflow-x, so body overflow no longer propagates to the viewport and public
 * pages would keep scrolling. Hidden overflow keeps the scroll offset, and the
 * supported Safari (16.4+) honours it on the root, so no position: fixed
 * snapshot is needed.
 */
const scrollLockHolders = new Set<symbol>()
const scrollLockStyles = new Map<HTMLElement, string>()
let inertApp: { element: HTMLElement; previous: boolean } | null = null

export function lockBodyScroll(holder: symbol): void {
  if (scrollLockHolders.has(holder)) return
  if (scrollLockHolders.size === 0) {
    const surfaces = [
      document.documentElement,
      document.body,
      ...document.querySelectorAll<HTMLElement>('[data-console-scroll-zone]'),
    ]
    for (const surface of surfaces) {
      scrollLockStyles.set(surface, surface.style.overflow)
      surface.style.overflow = 'hidden'
    }
    const app = document.getElementById('app')
    if (app) {
      inertApp = { element: app, previous: app.inert }
      app.inert = true
    }
  }
  scrollLockHolders.add(holder)
}

export function unlockBodyScroll(holder: symbol): void {
  if (!scrollLockHolders.delete(holder)) return
  if (scrollLockHolders.size === 0) {
    for (const [surface, overflow] of scrollLockStyles)
      surface.style.overflow = overflow
    scrollLockStyles.clear()
    if (inertApp) inertApp.element.inert = inertApp.previous
    inertApp = null
  }
}
