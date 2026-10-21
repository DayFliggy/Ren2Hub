import { decimalText } from '@/features/pricing/draft'
import { shiftDecimal, multiplyDecimal } from '@/utils/decimal'
import { createApiClient } from './createClient'
import { httpTransport, publicHttpTransport } from './httpTransport'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import {
  parsePricingModelsPartial,
  type PricingCatalogIssue,
  type PricingModelContract,
} from './liveContracts'
import { ApiError } from './types'

export type PublicDocument = 'about' | 'privacy-policy' | 'user-agreement'
export type RankingPeriod = 'today' | 'week' | 'month' | 'year'
export type CatalogModel = PricingModelContract
export interface CatalogPrice {
  key: string
  value: string
  unit: string
}
export interface PricingCatalog {
  models: CatalogModel[]
  issues: PricingCatalogIssue[]
  vendors: { id: number; name: string; description: string }[]
  endpoints: Record<string, { path: string; method: string }>
}
export interface RankedModel {
  rank: number
  previous_rank: number | null
  model_name: string
  vendor: string
  category: string
  total_tokens: number
  share: number
  growth_pct: number
}
export interface RankedVendor {
  rank: number
  vendor: string
  total_tokens: number
  share: number
  growth_pct: number
  models_count: number
  top_model: string
}
export interface RankingMover {
  model_name: string
  vendor: string
  rank_delta: number
  current_rank: number
  growth_pct: number
}
export interface RankingHistoryPoint {
  ts: string
  label: string
  entity: string
  tokens: number
  share: number | null
}
export interface RankingsSnapshot {
  models: RankedModel[]
  vendors: RankedVendor[]
  top_movers: RankingMover[]
  top_droppers: RankingMover[]
  modelHistory: RankingHistoryPoint[]
  vendorHistory: RankingHistoryPoint[]
}

const publicClient = createApiClient(publicHttpTransport)
const client = createApiClient(httpTransport)

function nonNegative(value: unknown, endpoint: string): number {
  const number = requiredStrictNumber(value, endpoint)
  if (number < 0) invalidResponse(endpoint)
  return number
}

function count(value: unknown, endpoint: string): number {
  const number = requiredStrictInteger(value, endpoint)
  if (number < 0) invalidResponse(endpoint)
  return number
}

function records(value: unknown, endpoint: string): Record<string, unknown>[] {
  if (!Array.isArray(value) || value.some((item) => !isRecord(item)))
    invalidResponse(endpoint)
  return value as Record<string, unknown>[]
}

export function parsePricingCatalog(value: unknown): PricingCatalog {
  const endpoint = '/api/pricing'
  if (!isRecord(value) || typeof value.success !== 'boolean')
    invalidResponse(endpoint)
  if (!value.success)
    throw new ApiError(requiredString(value.message, endpoint), {
      business: true,
      status: 200,
    })
  const { models, issues } = parsePricingModelsPartial(value.data)
  if (!isRecord(value.supported_endpoint)) invalidResponse(endpoint)
  return {
    models,
    issues,
    vendors: records(value.vendors ?? [], endpoint).map((vendor) => ({
      id: requiredStrictInteger(vendor.id, endpoint),
      name: requiredString(vendor.name, endpoint, false),
      description: requiredString(vendor.description ?? '', endpoint),
    })),
    endpoints: Object.fromEntries(
      Object.entries(value.supported_endpoint).map(([key, info]) => {
        if (!isRecord(info)) invalidResponse(endpoint)
        return [
          key,
          {
            path: requiredString(info.path, endpoint, false),
            method: requiredString(info.method, endpoint, false),
          },
        ]
      })
    ),
  }
}

function share(value: unknown, endpoint: string): number {
  const ratio = nonNegative(value, endpoint)
  if (ratio > 1) invalidResponse(endpoint)
  return ratio
}

function parseRankingHistory(
  value: unknown,
  entity: 'model' | 'vendor',
  endpoint: string
): RankingHistoryPoint[] {
  if (!isRecord(value)) invalidResponse(endpoint)
  return records(value.points, endpoint).map((point) => ({
    ts: requiredString(point.ts, endpoint, false),
    label: requiredString(point.label, endpoint),
    entity: requiredString(point[entity], endpoint, false),
    tokens: count(point.tokens, endpoint),
    share: entity === 'vendor' ? share(point.share, endpoint) : null,
  }))
}

export function parseRankings(value: unknown): RankingsSnapshot {
  const endpoint = '/api/rankings'
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    models: records(value.models, endpoint).map((model) => ({
      rank: count(model.rank, endpoint),
      previous_rank:
        model.previous_rank == null
          ? null
          : count(model.previous_rank, endpoint),
      model_name: requiredString(model.model_name, endpoint, false),
      vendor: requiredString(model.vendor, endpoint),
      category: requiredString(model.category, endpoint),
      total_tokens: count(model.total_tokens, endpoint),
      share: share(model.share, endpoint),
      growth_pct: requiredStrictNumber(model.growth_pct, endpoint),
    })),
    vendors: records(value.vendors, endpoint).map((vendor) => ({
      rank: count(vendor.rank, endpoint),
      vendor: requiredString(vendor.vendor, endpoint, false),
      total_tokens: count(vendor.total_tokens, endpoint),
      share: share(vendor.share, endpoint),
      growth_pct: requiredStrictNumber(vendor.growth_pct, endpoint),
      models_count: count(vendor.models_count, endpoint),
      top_model: requiredString(vendor.top_model, endpoint),
    })),
    top_movers: parseMovers(value.top_movers),
    top_droppers: parseMovers(value.top_droppers),
    modelHistory: parseRankingHistory(value.models_history, 'model', endpoint),
    vendorHistory: parseRankingHistory(
      value.vendor_share_history,
      'vendor',
      endpoint
    ),
  }
}

function parseMovers(value: unknown): RankingMover[] {
  const endpoint = '/api/rankings'
  return records(value, endpoint).map((mover) => ({
    model_name: requiredString(mover.model_name, endpoint, false),
    vendor: requiredString(mover.vendor, endpoint),
    rank_delta: requiredStrictInteger(mover.rank_delta, endpoint),
    current_rank: count(mover.current_rank, endpoint),
    growth_pct: requiredStrictNumber(mover.growth_pct, endpoint),
  }))
}

/** Display the global native numeric pricing fields. */
export type CatalogPricingInput = Pick<
  CatalogModel,
  'quota_type' | 'billing_mode'
> &
  Partial<
    Record<
      | 'model_price'
      | 'model_ratio'
      | 'completion_ratio'
      | 'cache_ratio'
      | 'create_cache_ratio'
      | 'image_ratio'
      | 'audio_ratio'
      | 'audio_completion_ratio',
      string | number
    >
  >

export function catalogPrices(
  model: CatalogPricingInput,
  unit: 'M' | 'K'
): CatalogPrice[] {
  if (
    model.billing_mode === 'tiered_expr' ||
    model.billing_mode === 'unconfigured'
  )
    return []
  if (model.quota_type === 1)
    return model.model_price === undefined
      ? []
      : [
          {
            key: 'request',
            value: decimalText(model.model_price),
            unit: 'per_call',
          },
        ]
  if (model.model_ratio === undefined) return []
  const input = multiplyDecimal(decimalText(model.model_ratio), '2')
  const prices: CatalogPrice[] = [
    { key: 'input', value: input, unit: 'per_million_tokens' },
  ]
  for (const [field, key] of [
    ['completion_ratio', 'output'],
    ['cache_ratio', 'cacheRead'],
    ['create_cache_ratio', 'cacheWrite'],
    ['image_ratio', 'image'],
  ] as const) {
    const ratio = model[field]
    if (ratio === undefined) continue
    prices.push({
      key,
      value: multiplyDecimal(input, decimalText(ratio)),
      unit: 'per_million_tokens',
    })
  }
  if (model.audio_ratio !== undefined) {
    const audioInput = multiplyDecimal(input, decimalText(model.audio_ratio))
    prices.push({
      key: 'audioInput',
      value: audioInput,
      unit: 'per_million_tokens',
    })
    if (model.audio_completion_ratio !== undefined) {
      prices.push({
        key: 'audioOutput',
        value: multiplyDecimal(
          audioInput,
          decimalText(model.audio_completion_ratio)
        ),
        unit: 'per_million_tokens',
      })
    }
  }
  return prices.map((price) => ({
    ...price,
    value: unit === 'K' ? shiftDecimal(price.value, 3) : price.value,
    unit: unit === 'K' ? 'per_thousand_tokens' : price.unit,
  }))
}

export const publicCatalogApi = {
  async document(kind: PublicDocument, signal?: AbortSignal): Promise<string> {
    return requiredString(
      await publicClient.get<unknown>(`/api/${kind}`, undefined, { signal }),
      `/api/${kind}`
    )
  },
  async pricing(signal?: AbortSignal): Promise<PricingCatalog> {
    return parsePricingCatalog(
      await httpTransport.request<unknown>('GET', '/api/pricing', { signal })
    )
  },
  async rankings(
    period: RankingPeriod,
    signal?: AbortSignal
  ): Promise<RankingsSnapshot> {
    return parseRankings(
      await client.get<unknown>('/api/rankings', { period }, { signal })
    )
  },
}
