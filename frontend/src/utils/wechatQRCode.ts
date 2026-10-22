import { safeImageUrl } from './safeUrl'

export type WeChatQRCode =
  { state: 'missing' } | { state: 'invalid' } | { state: 'ready'; src: string }

/**
 * The QR image URL is set by an administrator and is often hosted elsewhere,
 * so the configured URL's own HTTP(S) origin is trusted for this one image.
 * Other protocols still fail the shared image check and are reported as
 * invalid rather than as not configured.
 */
export function wechatQRCode(value: string | undefined): WeChatQRCode {
  const configured = value?.trim() ?? ''
  if (!configured) return { state: 'missing' }
  const src = safeImageUrl(configured, [configured])
  return src ? { state: 'ready', src } : { state: 'invalid' }
}
