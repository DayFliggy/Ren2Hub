import type {
  ChannelUpstreamRatio,
  UpstreamRatioStatus,
} from '@/api/channelUpstreamRatio'

type Translate = (key: string, params?: Record<string, unknown>) => string
type Tone = 'success' | 'warning' | 'danger' | 'neutral'

const errorCodes = new Set([
  'unsupported_channel',
  'not_detected',
  'invalid_base_url',
  'missing_key',
  'official_api',
  'invalid_proxy',
  'invalid_response',
  'invalid_pricing',
  'pricing_auth_required',
  'response_too_large',
  'request_failed',
  'unsupported',
  'auth_failed',
  'rate_limited',
  'redirect',
  'group_missing',
  'detail_too_large',
])

/** Backend reasons are fixed codes; unknown codes are shown verbatim. */
export function upstreamRatioErrorText(code: string, t: Translate): string {
  if (!code) return ''
  const pricing =
    code.startsWith('pricing_') && code !== 'pricing_auth_required'
  const base = pricing ? code.slice('pricing_'.length) : code
  const http = /^http_(\d{3})$/.exec(base)
  let text = code
  if (http) text = t('channels.upstreamRatio.errors.http', { status: http[1] })
  else if (errorCodes.has(base))
    text = t(`channels.upstreamRatio.errors.${base}`)
  return pricing ? t('channels.upstreamRatio.errors.pricing', { text }) : text
}

export function upstreamRatioStatusTone(status: UpstreamRatioStatus): Tone {
  if (status === 'ok') return 'success'
  if (status === 'needs_mapping' || status === 'rate_limited') return 'warning'
  if (status === 'auth_failed' || status === 'failed') return 'danger'
  return 'neutral'
}

/** The value shown in the table: the ratio in effect now, else the stored one. */
export function upstreamRatioValue(view: ChannelUpstreamRatio): string {
  return view.effective_ratio || view.ratio
}
