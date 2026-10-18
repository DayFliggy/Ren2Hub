export function tokenStatusLabel(status: number): string {
  return (
    (
      {
        1: 'common.enabled',
        2: 'common.disabled',
        3: 'keys.statusExpired',
        4: 'keys.statusExhausted',
      } as Record<number, string>
    )[status] ?? 'common.unknown'
  )
}
