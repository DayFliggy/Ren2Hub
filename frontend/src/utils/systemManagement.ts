export function formatSystemTimestamp(
  timestamp: number,
  locale: string
): string {
  if (!timestamp) return '-'
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(timestamp * 1000)
}

export function formatSystemRelative(
  timestamp: number,
  now: number,
  locale: string
): string {
  if (!timestamp) return '-'
  const seconds = Math.round((timestamp * 1000 - now) / 1000)
  const magnitude = Math.abs(seconds)
  const unit =
    magnitude < 60
      ? 'second'
      : magnitude < 3600
        ? 'minute'
        : magnitude < 86400
          ? 'hour'
          : 'day'
  const divisor =
    unit === 'second'
      ? 1
      : unit === 'minute'
        ? 60
        : unit === 'hour'
          ? 3600
          : 86400
  return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(
    Math.round(seconds / divisor),
    unit
  )
}

export function formatSystemBytes(
  bytes: number | null,
  locale: string
): string {
  if (bytes === null) return '-'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  const index =
    bytes > 0
      ? Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
      : 0
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: index === 0 ? 0 : 1 }).format(bytes / 1024 ** index)} ${units[index]}`
}
