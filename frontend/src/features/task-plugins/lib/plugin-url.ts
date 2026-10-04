// Mirrors upstream lib/plugin-url.ts. Ren2Hub keeps its stricter fetch: HTTPS
// only, no credentials, referrer or redirects, and a streamed byte cap.

/** Backend limit for one plugin source (maxTaskPluginSourceBytes). */
export const MAX_PLUGIN_SOURCE_BYTES = 8 * 1024 * 1024

export function pluginSourceByteLength(source: string): number {
  return new TextEncoder().encode(source).length
}

/**
 * Rewrites GitHub and gist page URLs to their raw-content equivalents.
 * Returns null unless the input is an absolute HTTPS URL.
 */
export function normalizePluginSourceUrl(input: string): string | null {
  const trimmed = input.trim()
  if (!trimmed) return null
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return null
  }
  if (parsed.protocol !== 'https:' || parsed.username || parsed.password)
    return null
  // Line anchors (#L12-L20) are page-only and break raw requests.
  parsed.hash = ''
  const host = parsed.hostname.toLowerCase()
  const segments = parsed.pathname.split('/').filter(Boolean)
  if (host === 'github.com' || host === 'www.github.com') {
    const isSourceView = segments[2] === 'blob' || segments[2] === 'raw'
    if (isSourceView && segments.length > 4) {
      const rest = segments.slice(3).join('/')
      return `https://raw.githubusercontent.com/${segments[0]}/${segments[1]}/${rest}`
    }
    return parsed.toString()
  }
  if (host === 'gist.github.com' && segments.length > 0) {
    const path = segments.join('/')
    const suffix = segments.includes('raw') ? path : `${path}/raw`
    return `https://gist.githubusercontent.com/${suffix}${parsed.search}`
  }
  return parsed.toString()
}

export type PluginSourceFetchFailure = 'unreachable' | 'not_found' | 'too_large'

export class PluginSourceFetchError extends Error {
  constructor(
    public readonly reason: PluginSourceFetchFailure,
    public readonly status?: number
  ) {
    super(reason)
    this.name = 'PluginSourceFetchError'
  }
}

/**
 * Fetches untrusted bytes in the browser; the gateway never makes this
 * request. Revalidates so a file republished in place is not read stale.
 */
export async function fetchPluginBytes(
  url: string,
  limit: number,
  signal?: AbortSignal
): Promise<Uint8Array> {
  if (new URL(url).protocol !== 'https:')
    throw new PluginSourceFetchError('unreachable')
  let response: Response
  try {
    response = await fetch(url, {
      signal,
      cache: 'no-cache',
      credentials: 'omit',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
    })
  } catch {
    // An aborted request surfaces as the abort, not as an unreachable host.
    signal?.throwIfAborted()
    throw new PluginSourceFetchError('unreachable')
  }
  if (!response.ok || !response.body)
    throw new PluginSourceFetchError('not_found', response.status)
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    for (;;) {
      const next = await reader.read()
      if (next.done) break
      size += next.value.length
      if (size > limit) throw new PluginSourceFetchError('too_large')
      chunks.push(next.value)
    }
  } finally {
    await reader.cancel()
  }
  const bytes = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.length
  }
  return bytes
}

export async function fetchPluginSourceText(
  url: string,
  signal?: AbortSignal
): Promise<string> {
  const bytes = await fetchPluginBytes(url, MAX_PLUGIN_SOURCE_BYTES, signal)
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

export async function sha256Hex(bytes: Uint8Array): Promise<string | null> {
  if (!globalThis.crypto?.subtle) return null
  const digest = await globalThis.crypto.subtle.digest(
    'SHA-256',
    bytes as Uint8Array<ArrayBuffer>
  )
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}

/**
 * Hex SHA-256 of the source bytes, or null without WebCrypto. The server
 * re-hashes on upload, so this is an early check, not the authority.
 */
export function computeSourceSha256(source: string): Promise<string | null> {
  return sha256Hex(new TextEncoder().encode(source))
}
