import { safeImageUrl } from './safeUrl'

export type WeChatQRCode =
  { state: 'missing' } | { state: 'invalid' } | { state: 'ready'; src: string }

/**
 * The QR image is set by an administrator and is often hosted elsewhere. The
 * shared safeImageUrl rules apply unchanged (base64 raster and sanitized SVG
 * data URIs, same-origin blob: URLs, HTTP(S) URLs from trusted origins); the
 * only extra trust added is the configured URL's own HTTP(S) origin. A
 * non-empty value those rules reject is reported as invalid, not as missing.
 */
export function wechatQRCode(value: string | undefined): WeChatQRCode {
  const configured = value?.trim() ?? ''
  if (!configured) return { state: 'missing' }
  const src = safeImageUrl(configured, [configured])
  return src ? { state: 'ready', src } : { state: 'invalid' }
}
