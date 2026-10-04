// Mirrors upstream NewAPI web/src/features/channels/lib/task-plugin-base-url.ts.

/**
 * Hosts worth a second look before a channel key is sent to them: loopback,
 * RFC 1918, CGNAT, link-local (including cloud metadata), IPv6 loopback /
 * link-local / ULA, and single-label or *.local / *.internal / *.lan names.
 * Self-hosted upstreams are legitimate, so these are flagged, never blocked.
 */
const PRIVATE_HOST_PATTERNS: readonly RegExp[] = [
  /^localhost$/i,
  /\.localhost$/i,
  /^127\./,
  /^0\.0\.0\.0$/,
  /^10\./,
  /^192\.168\./,
  /^172\.(1[6-9]|2\d|3[01])\./,
  /^169\.254\./,
  /^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./,
  /^\[?::1\]?$/,
  /^\[?fe80:/i,
  /^\[?f[cd][0-9a-f]{2}:/i,
  /\.(local|internal|lan)$/i,
]

export type BaseUrlTrust = {
  plainHttp: boolean
  privateHost: boolean
}

/** Returns null for empty or unparsable input so callers render nothing. */
export function assessBaseUrlTrust(
  value: string | undefined
): BaseUrlTrust | null {
  const trimmed = value?.trim()
  if (!trimmed) return null
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return null
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
  const host = parsed.hostname.toLowerCase()
  const singleLabel = !host.includes('.') && !host.includes(':')
  return {
    plainHttp: parsed.protocol === 'http:',
    privateHost:
      singleLabel ||
      PRIVATE_HOST_PATTERNS.some((pattern) => pattern.test(host)),
  }
}

export function normalizeBaseUrlValue(value: string | undefined): string {
  return String(value ?? '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * The plugin default is written only when the field is empty or still holds
 * the previous plugin's default, so a hand-typed value survives switching
 * plugins. Returns null when nothing should change.
 */
export function nextTaskPluginBaseUrl(
  currentValue: string | undefined,
  previousDefault: string | undefined,
  nextDefault: string | undefined
): string | null {
  if (!nextDefault) return null
  const current = normalizeBaseUrlValue(currentValue)
  if (current && current !== normalizeBaseUrlValue(previousDefault)) return null
  if (current === normalizeBaseUrlValue(nextDefault)) return null
  return nextDefault
}
