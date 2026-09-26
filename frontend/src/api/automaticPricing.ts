import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { parsePriceVersion } from './modelPricing'
import { parseSystemTask } from './systemManagement'

export const AUTOMATIC_PRICE_SOURCES = ['sub2api', 'axonhub'] as const
export type AutomaticPriceSource = (typeof AUTOMATIC_PRICE_SOURCES)[number]
const HISTORICAL_PRICE_SOURCES = ['models_dev', 'openrouter'] as const
export type PriceOrigin =
  | AutomaticPriceSource
  | (typeof HISTORICAL_PRICE_SOURCES)[number]
  | 'manual'
  | 'legacy_seed'
const SNAPSHOT_STATES = ['bundled', 'current', 'cached', 'unavailable'] as const
export interface AutomaticPricingConfig {
  enabled: boolean
  check_interval_minutes: number
}
export interface AutomaticPricingSource {
  source: AutomaticPriceSource
  url: string
  role: 'primary' | 'fallback'
  hash: string
  checked_at: number
  updated_at: number
  last_error: string
  model_count: number
  rejected_count?: number
  snapshot_state?: (typeof SNAPSHOT_STATES)[number]
}
export interface ModelPriceOrigin {
  model: string
  version_id: number
  origin: PriceOrigin
  source_model: string
  source_hash: string
}
export interface AutomaticPricingStatus {
  config: AutomaticPricingConfig
  sources: AutomaticPricingSource[]
  origins: ModelPriceOrigin[]
}
const endpoint = '/api/billing/prices/automatic'

export function parseAutomaticPricingConfig(
  value: unknown,
  path = endpoint
): AutomaticPricingConfig {
  if (!isRecord(value)) invalidResponse(path)
  const minutes = requiredStrictInteger(value.check_interval_minutes, path)
  if (minutes < 5 || minutes > 1440) invalidResponse(path)
  return {
    enabled: requiredBoolean(value.enabled, path),
    check_interval_minutes: minutes,
  }
}

export function parseAutomaticPricingStatus(
  value: unknown
): AutomaticPricingStatus {
  if (
    !isRecord(value) ||
    !Array.isArray(value.sources) ||
    !Array.isArray(value.origins)
  )
    invalidResponse(endpoint)
  const sources = value.sources.map((source): AutomaticPricingSource => {
    if (
      !isRecord(source) ||
      !AUTOMATIC_PRICE_SOURCES.includes(
        source.source as AutomaticPriceSource
      ) ||
      !['primary', 'fallback'].includes(String(source.role))
    )
      invalidResponse(endpoint)
    const result: AutomaticPricingSource = {
      source: source.source as AutomaticPriceSource,
      url: requiredString(source.url, endpoint, false),
      role: source.role as AutomaticPricingSource['role'],
      hash: requiredString(source.hash, endpoint),
      checked_at: requiredStrictInteger(source.checked_at, endpoint),
      updated_at: requiredStrictInteger(source.updated_at, endpoint),
      last_error: requiredString(source.last_error, endpoint),
      model_count: requiredStrictInteger(source.model_count, endpoint),
    }
    if (source.rejected_count !== undefined)
      result.rejected_count = requiredStrictInteger(
        source.rejected_count,
        endpoint
      )
    if (source.snapshot_state !== undefined) {
      if (
        !SNAPSHOT_STATES.includes(
          source.snapshot_state as (typeof SNAPSHOT_STATES)[number]
        )
      )
        invalidResponse(endpoint)
      result.snapshot_state =
        source.snapshot_state as AutomaticPricingSource['snapshot_state']
    }
    if (
      [
        result.checked_at,
        result.updated_at,
        result.model_count,
        result.rejected_count ?? 0,
      ].some((number) => number < 0)
    )
      invalidResponse(endpoint)
    return result
  })
  const origins = value.origins.map((origin): ModelPriceOrigin => {
    if (
      !isRecord(origin) ||
      ![
        ...AUTOMATIC_PRICE_SOURCES,
        ...HISTORICAL_PRICE_SOURCES,
        'manual',
        'legacy_seed',
      ].includes(String(origin.origin))
    )
      invalidResponse(endpoint)
    const result: ModelPriceOrigin = {
      model: requiredString(origin.model, endpoint, false),
      version_id: requiredStrictInteger(origin.version_id, endpoint),
      origin: origin.origin as PriceOrigin,
      source_model: requiredString(origin.source_model, endpoint),
      source_hash: requiredString(origin.source_hash, endpoint),
    }
    if (result.version_id < 1) invalidResponse(endpoint)
    return result
  })
  if (
    new Set(sources.map((row) => row.source)).size !== sources.length ||
    new Set(origins.map((row) => row.model)).size !== origins.length
  )
    invalidResponse(endpoint)
  return { config: parseAutomaticPricingConfig(value.config), sources, origins }
}

export const automaticPricingApi = {
  async status(signal?: AbortSignal) {
    return parseAutomaticPricingStatus(
      await api.get<unknown>(endpoint, undefined, { signal })
    )
  },
  async configure(config: AutomaticPricingConfig) {
    return parseAutomaticPricingConfig(await api.put<unknown>(endpoint, config))
  },
  async sync() {
    const path = `${endpoint}/sync`
    return parseSystemTask(await api.post<unknown>(path), path)
  },
  async restore(model: string, expectedVersion: number) {
    const path = `${endpoint}/restore`
    return parsePriceVersion(
      await api.post<unknown>(path, {
        model,
        expected_version: expectedVersion,
      }),
      path
    )
  },
}
