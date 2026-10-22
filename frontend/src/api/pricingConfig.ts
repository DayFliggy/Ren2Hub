import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import {
  NUMERIC_PRICING_KEYS,
  PRICING_KEYS,
  type NumericPricingKey,
  type PricingKey,
} from './pricingKeys'
import type {
  BillingUsageSchema,
  BillingUsageExample,
} from '@/features/pricing/types'
import { isDecimal } from '@/utils/decimal'

export { NUMERIC_PRICING_KEYS, PRICING_KEYS, type PricingKey }
// Drafts keep the exact decimal text typed in the editor; serializePricing
// turns it into the JSON numbers the backend accepts.
export type PricingValues = Partial<
  Record<NumericPricingKey, number | string>
> & {
  'billing_setting.billing_mode'?: string
  'billing_setting.billing_expr'?: string
  'billing_setting.plugin_billing_expr'?: Record<string, string>
}
export function isNumericPricingKey(key: string): key is NumericPricingKey {
  return (NUMERIC_PRICING_KEYS as readonly string[]).includes(key)
}
// Absorbed from upstream pricingFromDraft: a price is a finite, non-negative
// number; blank or non-decimal text is rejected instead of meaning free.
export function pricingNumber(value: number | string): number {
  const number =
    typeof value === 'number' ? value : isDecimal(value) ? Number(value) : NaN
  if (!Number.isFinite(number) || number < 0) throw new Error('invalid')
  return number
}
export function serializePricing(value: PricingValues): PricingValues {
  const result: PricingValues = { ...value }
  for (const key of NUMERIC_PRICING_KEYS) {
    const field = value[key]
    if (field !== undefined) result[key] = pricingNumber(field)
  }
  return result
}
export interface PluginPriceVariant {
  plugin_key: string
  plugin_name: string
  usage_schema: BillingUsageSchema
  usage_examples?: BillingUsageExample[]
  configured: string
  effective: string
  compatible: boolean
  stale?: boolean
}
export interface ModelPricingEntry {
  model_name: string
  version: string
  configured: PricingValues
  effective: PricingValues
  usage_schema?: BillingUsageSchema
  plugin_variants?: PluginPriceVariant[]
  cache_write_mode?: string
  billing_details?: Record<string, unknown>
}
export interface ModelPricingConfig {
  entries: ModelPricingEntry[]
  options: Record<PricingKey, string>
  empty_version: string
}
export interface ModelPricingChange {
  model_name: string
  expected_version: string
  pricing: PricingValues
  reset?: boolean
}
export interface PricingPreview {
  effective: PricingValues
  cache_write_mode?: string
  billing_details?: Record<string, unknown>
  expression?: string
  unsupported_reason?: string
}
// The backend owns the local fixed sources (pkg/pricesource.Sources) and
// returns their id and name. basellm, models.dev and OpenRouter go through the
// native ratio_sync upstreams below.
// The fixed sources are downloaded one after another, each limited to 15s.
const FIXED_PRICE_FETCH_TIMEOUT_MS = 45_000
export interface FetchedModelPrice {
  model_name: string
  provider: string
  pricing: PricingValues
  unsupported_reason?: string
  warnings?: string[]
}
// published_sha256 and matches are null when the source publishes no
// fingerprint or its .sha256 file could not be read.
export interface PriceSourceFingerprint {
  sha256: string
  published_sha256: string | null
  matches: boolean | null
}
export interface FetchedPriceSource {
  id: string
  name: string
  status: 'success' | 'partial' | 'error'
  // null only when the price document could not be downloaded.
  fingerprint: PriceSourceFingerprint | null
  models: FetchedModelPrice[]
  warnings: string[]
  errors: string[]
}
export type PriceUpstreamKind = 'basellm' | 'models_dev' | 'openrouter'
export interface PriceUpstream {
  id: number
  name: string
  base_url: string
  kind: PriceUpstreamKind
}
export interface FetchedUpstreamPrices {
  models: FetchedModelPrice[]
  errors: string[]
}
// Native preset channel ids and endpoints from NewAPI controller/ratio_sync.go
// and web/src/features/system-settings/models/constants.ts.
const PRESET_UPSTREAMS = new Map<number, PriceUpstreamKind>([
  [-100, 'basellm'],
  [-101, 'models_dev'],
])
const OPENROUTER_CHANNEL_TYPE = 20
const UPSTREAM_ENDPOINTS: Record<PriceUpstreamKind, string> = {
  basellm: '/llm-metadata/api/newapi/ratio_config-v1-base.json',
  models_dev: 'https://models.dev/api.json',
  openrouter: 'openrouter',
}
// Native sync field names mapped to model pricing keys.
const UPSTREAM_PRICING_KEYS = new Map<string, PricingKey>([
  ['model_ratio', 'ModelRatio'],
  ['completion_ratio', 'CompletionRatio'],
  ['cache_ratio', 'CacheRatio'],
  ['create_cache_ratio', 'CreateCacheRatio'],
  ['image_ratio', 'ImageRatio'],
  ['audio_ratio', 'AudioRatio'],
  ['audio_completion_ratio', 'AudioCompletionRatio'],
  ['model_price', 'ModelPrice'],
  ['billing_mode', 'billing_setting.billing_mode'],
  ['billing_expr', 'billing_setting.billing_expr'],
])
const SHA256_HEX = /^[0-9a-f]{64}$/

export function parsePricingValues(
  value: unknown,
  endpoint = '/api/option/model_pricing'
): PricingValues {
  if (!isRecord(value)) invalidResponse(endpoint)
  const result: PricingValues = {}
  for (const [key, field] of Object.entries(value)) {
    if (!(PRICING_KEYS as readonly string[]).includes(key))
      invalidResponse(endpoint)
    if (key === 'billing_setting.plugin_billing_expr') {
      if (
        !isRecord(field) ||
        Object.values(field).some((item) => typeof item !== 'string')
      )
        invalidResponse(endpoint)
      result[key] = field as Record<string, string>
    } else if (isNumericPricingKey(key)) {
      if (typeof field !== 'number' || !Number.isFinite(field) || field < 0)
        invalidResponse(endpoint)
      result[key] = field
    } else {
      if (typeof field !== 'string') invalidResponse(endpoint)
      result[key as 'billing_setting.billing_mode'] = field
    }
  }
  return result
}
export function parsePricingConfig(value: unknown): ModelPricingConfig {
  const endpoint = '/api/option/model_pricing'
  if (
    !isRecord(value) ||
    !Array.isArray(value.entries) ||
    !isRecord(value.options)
  )
    invalidResponse(endpoint)
  const entries = value.entries.map((raw): ModelPricingEntry => {
    if (!isRecord(raw)) invalidResponse(endpoint)
    return {
      ...raw,
      model_name: requiredString(raw.model_name, endpoint, false),
      version: requiredString(raw.version, endpoint),
      configured: parsePricingValues(raw.configured),
      effective: parsePricingValues(raw.effective),
    } as ModelPricingEntry
  })
  if (new Set(entries.map((entry) => entry.model_name)).size !== entries.length)
    invalidResponse(endpoint)
  return {
    entries,
    options: value.options as ModelPricingConfig['options'],
    empty_version: requiredString(value.empty_version, endpoint),
  }
}
// The published digest and matches are both null or both set, and matches
// must agree with the digests so a mismatch can never render as a match.
function parseFingerprint(
  value: unknown,
  endpoint: string
): PriceSourceFingerprint | null {
  if (value === null) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  const sha256 = requiredString(value.sha256, endpoint)
  if (!SHA256_HEX.test(sha256)) invalidResponse(endpoint)
  const published = value.published_sha256
  if (published === null && value.matches === null)
    return { sha256, published_sha256: null, matches: null }
  if (typeof published !== 'string' || !SHA256_HEX.test(published))
    invalidResponse(endpoint)
  if (value.matches !== (published === sha256)) invalidResponse(endpoint)
  return { sha256, published_sha256: published, matches: published === sha256 }
}
function parseFetchedModel(value: unknown, endpoint: string) {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    model_name: requiredString(value.model_name, endpoint, false),
    provider: requiredString(value.provider, endpoint),
    // The backend omits empty model warnings.
    warnings:
      value.warnings === undefined
        ? []
        : parseStringArray(value.warnings, endpoint),
    pricing: parsePricingValues(value.pricing, endpoint),
    ...(value.unsupported_reason === undefined
      ? {}
      : {
          unsupported_reason: requiredString(
            value.unsupported_reason,
            endpoint
          ),
        }),
  }
}
export function parseFetchedPrices(value: unknown): FetchedPriceSource[] {
  const endpoint = '/api/ratio_sync/fetch'
  if (!isRecord(value) || !Array.isArray(value.sources))
    invalidResponse(endpoint)
  return value.sources.map((raw) => {
    if (
      !isRecord(raw) ||
      !['success', 'partial', 'error'].includes(String(raw.status)) ||
      !Array.isArray(raw.models)
    )
      invalidResponse(endpoint)
    return {
      id: requiredString(raw.id, endpoint, false),
      name: requiredString(raw.name, endpoint),
      status: raw.status as FetchedPriceSource['status'],
      fingerprint: parseFingerprint(raw.fingerprint, endpoint),
      warnings: parseStringArray(raw.warnings, endpoint),
      errors: parseStringArray(raw.errors, endpoint),
      models: raw.models.map((model) => parseFetchedModel(model, endpoint)),
    }
  })
}
// Only the native presets and OpenRouter channels take part in price sync;
// other syncable channels are dropped here on purpose.
export function parsePriceUpstreams(value: unknown): PriceUpstream[] {
  const endpoint = '/api/ratio_sync/channels'
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.flatMap((raw): PriceUpstream[] => {
    if (!isRecord(raw)) invalidResponse(endpoint)
    const id = requiredStrictInteger(raw.id, endpoint)
    const kind =
      PRESET_UPSTREAMS.get(id) ??
      (raw.type === OPENROUTER_CHANNEL_TYPE ? 'openrouter' : undefined)
    if (!kind) return []
    return [
      {
        id,
        kind,
        name: requiredString(raw.name, endpoint, false),
        base_url: requiredString(raw.base_url, endpoint, false),
      },
    ]
  })
}
function parseUpstreamPricing(value: unknown, endpoint: string) {
  if (!isRecord(value)) invalidResponse(endpoint)
  const pricing: Record<string, unknown> = {}
  for (const [field, item] of Object.entries(value)) {
    const key = UPSTREAM_PRICING_KEYS.get(field)
    if (!key) invalidResponse(endpoint)
    pricing[key] = item
  }
  return parsePricingValues(pricing, endpoint)
}
// The native response lists only models whose prices differ from the saved
// local prices; one upstream is requested at a time.
export function parseUpstreamPrices(value: unknown): FetchedUpstreamPrices {
  const endpoint = '/api/ratio_sync/fetch'
  if (
    !isRecord(value) ||
    !isRecord(value.prices) ||
    !Array.isArray(value.test_results)
  )
    invalidResponse(endpoint)
  const errors = value.test_results.flatMap((result) => {
    if (!isRecord(result)) invalidResponse(endpoint)
    if (result.status === 'success') return []
    if (result.status !== 'error') invalidResponse(endpoint)
    return [requiredString(result.error, endpoint, false)]
  })
  const models = Object.entries(value.prices).flatMap(([name, row]) => {
    if (!isRecord(row) || !isRecord(row.upstreams)) invalidResponse(endpoint)
    return Object.values(row.upstreams).map((pricing) => ({
      model_name: requiredString(name, endpoint, false),
      provider: '',
      pricing: parseUpstreamPricing(pricing, endpoint),
    }))
  })
  if (new Set(models.map((model) => model.model_name)).size !== models.length)
    invalidResponse(endpoint)
  return { models, errors }
}
export const pricingConfigApi = {
  async load(signal?: AbortSignal) {
    return parsePricingConfig(
      await api.get('/api/option/model_pricing', undefined, { signal })
    )
  },
  save(changes: ModelPricingChange[]) {
    return api.patch('/api/option/model_pricing', {
      changes: changes.map((change) => ({
        ...change,
        pricing: serializePricing(change.pricing),
      })),
    })
  },
  preview(model_name: string, pricing: PricingValues, signal?: AbortSignal) {
    return api.post<PricingPreview>(
      '/api/option/model_pricing/preview',
      { model_name, pricing: serializePricing(pricing) },
      { signal }
    )
  },
  convert(model_name: string, pricing: PricingValues) {
    return api.post<PricingPreview>('/api/option/model_pricing/convert', {
      model_name,
      pricing: serializePricing(pricing),
    })
  },
  async fetch(signal?: AbortSignal) {
    return parseFetchedPrices(
      await api.post(
        '/api/ratio_sync/fetch',
        {},
        { signal, timeoutMs: FIXED_PRICE_FETCH_TIMEOUT_MS }
      )
    )
  },
  async upstreams(signal?: AbortSignal) {
    return parsePriceUpstreams(
      await api.get('/api/ratio_sync/channels', undefined, { signal })
    )
  },
  // Sending upstreams selects the native NewAPI flow; the preset endpoint
  // must be explicit because the native default is /api/pricing.
  async fetchUpstream(upstream: PriceUpstream, signal?: AbortSignal) {
    const body = {
      upstreams: [
        {
          id: upstream.id,
          name: upstream.name,
          base_url: upstream.base_url,
          endpoint: UPSTREAM_ENDPOINTS[upstream.kind],
        },
      ],
    }
    return parseUpstreamPrices(
      await api.post('/api/ratio_sync/fetch', body, { signal })
    )
  },
}
