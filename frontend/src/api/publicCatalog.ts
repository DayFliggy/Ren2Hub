import { createApiClient } from './createClient'
import { httpTransport, publicHttpTransport } from './httpTransport'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import { parsePricingModels, type PricingModelContract } from './liveContracts'
import { ApiError } from './types'

export type PublicDocument = 'about' | 'privacy-policy' | 'user-agreement'
export type RankingPeriod = 'today' | 'week' | 'month' | 'year'
export interface CatalogModel extends PricingModelContract {
  billing_expr: string
  image_ratio: number | null
  audio_ratio: number | null
  audio_completion_ratio: number | null
}
export interface PricingCatalog {
  models: CatalogModel[]
  vendors: { id: number; name: string; description: string }[]
  groupRatios: Record<string, number>
  usableGroups: Record<string, string>
  autoGroups: string[]
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
  const rawModels = records(value.data, endpoint)
  const models = parsePricingModels(rawModels).map((model, index) => {
    const raw = rawModels[index]!
    if (model.quota_type !== 0 && model.quota_type !== 1)
      invalidResponse(endpoint)
    for (const key of [
      'model_ratio',
      'model_price',
      'completion_ratio',
    ] as const)
      nonNegative(raw[key], endpoint)
    for (const ratio of [model.cache_ratio, model.create_cache_ratio])
      if (ratio !== null) nonNegative(ratio, endpoint)
    return {
      ...model,
      billing_expr: requiredString(raw.billing_expr ?? '', endpoint),
      image_ratio:
        raw.image_ratio == null ? null : nonNegative(raw.image_ratio, endpoint),
      audio_ratio:
        raw.audio_ratio == null ? null : nonNegative(raw.audio_ratio, endpoint),
      audio_completion_ratio:
        raw.audio_completion_ratio == null
          ? null
          : nonNegative(raw.audio_completion_ratio, endpoint),
    }
  })
  if (
    !isRecord(value.group_ratio) ||
    !isRecord(value.usable_group) ||
    !isRecord(value.supported_endpoint)
  )
    invalidResponse(endpoint)
  return {
    models,
    vendors: records(value.vendors ?? [], endpoint).map((vendor) => ({
      id: count(vendor.id, endpoint),
      name: requiredString(vendor.name, endpoint, false),
      description: requiredString(vendor.description ?? '', endpoint),
    })),
    groupRatios: Object.fromEntries(
      Object.entries(value.group_ratio).map(([key, ratio]) => [
        key,
        nonNegative(ratio, endpoint),
      ])
    ),
    usableGroups: Object.fromEntries(
      Object.entries(value.usable_group).map(([key, description]) => [
        key,
        requiredString(description, endpoint),
      ])
    ),
    autoGroups: parseStringArray(value.auto_groups ?? [], endpoint),
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

export function catalogGroupRatio(
  catalog: PricingCatalog,
  model: CatalogModel,
  group: string
): number | null {
  if (group === 'auto') {
    const ratios = catalog.autoGroups
      .filter(
        (name) =>
          model.enable_groups.includes(name) ||
          model.enable_groups.includes('all')
      )
      .map((name) => catalog.groupRatios[name])
      .filter((ratio): ratio is number => ratio !== undefined)
    return ratios.length ? Math.min(...ratios) : null
  }
  if (
    !model.enable_groups.includes(group) &&
    !model.enable_groups.includes('all')
  )
    return null
  return catalog.groupRatios[group] ?? null
}

export function catalogPrices(
  model: CatalogModel,
  groupRatio: number,
  unit: 'M' | 'K'
) {
  if (model.billing_mode === 'tiered_expr') return []
  if (model.quota_type === 1)
    return [{ key: 'request', value: model.model_price * groupRatio }]
  const input = (model.model_ratio * 2 * groupRatio) / (unit === 'K' ? 1000 : 1)
  const prices = [
    { key: 'input', value: input },
    { key: 'output', value: input * model.completion_ratio },
  ]
  const optionalPrices = [
    ['cacheRead', model.cache_ratio],
    ['cacheWrite', model.create_cache_ratio],
    ['image', model.image_ratio],
    ['audioInput', model.audio_ratio],
    [
      'audioOutput',
      model.audio_ratio !== null && model.audio_completion_ratio !== null
        ? model.audio_ratio * model.audio_completion_ratio
        : null,
    ],
  ] as const
  for (const [key, ratio] of optionalPrices)
    if (ratio !== null) prices.push({ key, value: input * ratio })
  return prices
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
