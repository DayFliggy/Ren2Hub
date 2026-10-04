// Mirrors upstream lib/plugin-icon-file.ts on Ren2Hub's bounded fetch.
import { fetchPluginBytes, sha256Hex } from './plugin-url'

/** Mirrors the gateway's cap on an uploaded plugin logo. */
export const MAX_PLUGIN_ICON_BYTES = 512 * 1024

export type PluginIconMediaType = 'image/png' | 'image/svg+xml'

/** Decided by extension: browsers report empty or vendor types for SVG. */
export function pluginIconMediaType(
  fileName: string
): PluginIconMediaType | null {
  const lower = fileName.toLowerCase()
  if (lower.endsWith('.svg')) return 'image/svg+xml'
  if (lower.endsWith('.png')) return 'image/png'
  return null
}

export type PluginIconFileFailure = 'unsupported_type' | 'too_large'

export class PluginIconFileError extends Error {
  constructor(public readonly reason: PluginIconFileFailure) {
    super(reason)
    this.name = 'PluginIconFileError'
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  const chunk = 0x8000
  for (let offset = 0; offset < bytes.length; offset += chunk) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunk))
  }
  return btoa(binary)
}

/** Encodes a sidecar logo as the data URI the upload endpoint stores. */
export async function encodePluginIconFile(file: File): Promise<string> {
  const mediaType = pluginIconMediaType(file.name)
  if (!mediaType) throw new PluginIconFileError('unsupported_type')
  if (file.size > MAX_PLUGIN_ICON_BYTES)
    throw new PluginIconFileError('too_large')
  const bytes = new Uint8Array(await file.arrayBuffer())
  return `data:${mediaType};base64,${bytesToBase64(bytes)}`
}

/**
 * Fetches a marketplace logo as a data URI for display and upload. Raw hosts
 * serve SVG as text/plain with nosniff, so <img src> cannot link it directly.
 * A logo is cosmetic: any failure or digest mismatch yields null so it can
 * never block an install or show a swapped file.
 */
export async function fetchPluginIconDataUri(
  url: string,
  options: { sha256?: string; signal?: AbortSignal } = {}
): Promise<string | null> {
  const mediaType = pluginIconMediaType(new URL(url).pathname)
  if (!mediaType) return null
  let bytes: Uint8Array
  try {
    bytes = await fetchPluginBytes(url, MAX_PLUGIN_ICON_BYTES, options.signal)
  } catch {
    options.signal?.throwIfAborted()
    return null
  }
  if (bytes.length === 0) return null
  const expected = options.sha256?.trim().toLowerCase()
  if (expected) {
    const actual = await sha256Hex(bytes)
    if (actual !== null && actual !== expected) return null
  }
  return `data:${mediaType};base64,${bytesToBase64(bytes)}`
}
