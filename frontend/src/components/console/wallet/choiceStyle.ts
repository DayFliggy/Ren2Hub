/** Selected and idle looks shared by the amount and payment method choices. */
export function choiceClass(selected: boolean): string {
  return selected
    ? 'border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--accent-text)] shadow-[inset_0_1px_0_rgba(255,244,214,0.14)]'
    : 'border-[var(--outline-variant)] bg-[var(--wallet-choice-surface)] text-[var(--text-secondary)] hover:border-[var(--outline)] hover:bg-[var(--wallet-choice-hover)] hover:text-[var(--text-primary)]'
}
