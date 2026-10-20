import { api } from './client'
import { invalidResponse, isRecord, requiredString } from './contracts'
import { PRICING_KEYS, type PricingKey } from './pricingKeys'
import type {
  BillingUsageSchema,
  BillingUsageExample,
} from '@/features/pricing/types'

export { PRICING_KEYS, type PricingKey }
export type PricingValues = Partial<
  Record<
    Exclude<PricingKey, 'billing_setting.plugin_billing_expr'>,
    number | string
  >
> & { 'billing_setting.plugin_billing_expr'?: Record<string, string> }
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
export const PRICE_SOURCES = [
  'sub2api',
  'axonhub',
  'basellm',
  'models_dev',
  'openrouter',
] as const
export type PriceSourceId = (typeof PRICE_SOURCES)[number]
export interface FetchedModelPrice {
  model_name: string
  provider: string
  pricing: PricingValues
  unsupported_reason?: string
  warnings?: string[]
}
export interface FetchedPriceSource {
  id: PriceSourceId
  name: string
  status: 'success' | 'partial' | 'error'
  models: FetchedModelPrice[]
  errors: string[]
}

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
    } else {
      if (
        typeof field !== 'string' &&
        (typeof field !== 'number' || !Number.isFinite(field))
      )
        invalidResponse(endpoint)
      result[
        key as Exclude<PricingKey, 'billing_setting.plugin_billing_expr'>
      ] = field as string | number
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
export function parseFetchedPrices(value: unknown): FetchedPriceSource[] {
  const endpoint = '/api/ratio_sync/fetch'
  if (!isRecord(value) || !Array.isArray(value.sources))
    invalidResponse(endpoint)
  return value.sources.map((raw) => {
    if (
      !isRecord(raw) ||
      !(PRICE_SOURCES as readonly unknown[]).includes(raw.id) ||
      !['success', 'partial', 'error'].includes(String(raw.status)) ||
      !Array.isArray(raw.models) ||
      !Array.isArray(raw.errors) ||
      raw.errors.some((error) => typeof error !== 'string')
    )
      invalidResponse(endpoint)
    return {
      id: raw.id as PriceSourceId,
      name: requiredString(raw.name, endpoint),
      status: raw.status as FetchedPriceSource['status'],
      errors: raw.errors as string[],
      models: raw.models.map((model) => {
        if (!isRecord(model)) invalidResponse(endpoint)
        return {
          model_name: requiredString(model.model_name, endpoint, false),
          provider: requiredString(model.provider, endpoint),
          warnings: Array.isArray(model.warnings)
            ? model.warnings.map((warning) => requiredString(warning, endpoint))
            : [],
          pricing: parsePricingValues(model.pricing, endpoint),
          ...(model.unsupported_reason === undefined
            ? {}
            : {
                unsupported_reason: requiredString(
                  model.unsupported_reason,
                  endpoint
                ),
              }),
        }
      }),
    }
  })
}
export const pricingConfigApi = {
  async load(signal?: AbortSignal) {
    return parsePricingConfig(
      await api.get('/api/option/model_pricing', undefined, { signal })
    )
  },
  save(changes: ModelPricingChange[]) {
    return api.patch('/api/option/model_pricing', { changes })
  },
  preview(model_name: string, pricing: PricingValues, signal?: AbortSignal) {
    return api.post<PricingPreview>(
      '/api/option/model_pricing/preview',
      { model_name, pricing },
      { signal }
    )
  },
  convert(model_name: string, pricing: PricingValues) {
    return api.post<PricingPreview>('/api/option/model_pricing/convert', {
      model_name,
      pricing,
    })
  },
  simulate(input: {
    plugin_key?: string
    model_name: string
    pricing: PricingValues
    usage: Record<string, unknown>
    request: {
      params: Record<string, unknown>
      headers: Record<string, string>
    }
    time: string
  }) {
    return api.post<{ amount_usd: string; details?: unknown }>(
      '/api/option/model_pricing/simulate',
      input
    )
  },
  async fetch(signal?: AbortSignal) {
    return parseFetchedPrices(
      await api.post('/api/ratio_sync/fetch', {}, { signal })
    )
  },
}
