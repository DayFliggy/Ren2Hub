/** Selected and idle looks shared by the amount and payment method choices. */
export function choiceClass(selected: boolean): string {
  return selected
    ? 'border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--accent-text)] shadow-[inset_0_0_0_1px_var(--accent)]'
    : 'border-[var(--border-subtle)] bg-[var(--surface-muted)] text-[var(--text-secondary)] hover:bg-[var(--surface-hover)] hover:text-[var(--text-primary)]'
}
