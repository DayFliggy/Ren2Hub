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
