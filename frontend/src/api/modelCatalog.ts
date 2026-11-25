import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { isDecimal } from '@/utils/decimal'

export type PriceStatus = 'configured' | 'default' | 'zero' | 'unavailable'
export type CatalogPrices = Record<string, string>
export type CatalogAvailability =
  'unknown' | 'no_restriction' | 'limited' | 'paused' | 'recovering'
export type CatalogQuality =
  'unrated' | 'no_downgrade' | 'degraded' | 'severely_degraded'
export interface CatalogOfferHealth {
  availability: CatalogAvailability
  quality: CatalogQuality
}
export interface CatalogChannelHealth {
  visible_models: number
  affected_models: number
  evaluated_models: number
  unrated_models: number
}
export interface CatalogModel {
  model_name: string
  description: string
  icon: string
  tags?: string
  vendor_id: number
  supported_endpoint_types: string[]
  price_status: PriceStatus
  pricing_values: CatalogPrices
  price_reason?: string
}
export interface CatalogChannel {
  id: number
  name: string
  type: number
  status: number
  channel_ratio: string
  max_concurrency: number
  eligible: boolean
  filter_reason: string
  lab: { id: string; name: string; source: string }
  health?: CatalogChannelHealth
}
export interface CatalogOffer {
  model_id: string
  channel_id: number
  supported_endpoint_types: string[]
  channel_ratio: string
  price_status: PriceStatus
  pricing_values: CatalogPrices
  price_reason?: string
  health?: CatalogOfferHealth
}
export interface ModelCatalog {
  schema_version: 1
  models: CatalogModel[]
  vendors: { id: number; name: string; icon?: string }[]
  supported_endpoints: Record<string, { path: string; method: string }>
  channels: CatalogChannel[]
  offers: CatalogOffer[]
  server_address: string
  generated_at: number
}
const endpoint = '/api/user/model-catalog'
const availabilities = new Set<string>([
  'unknown',
  'no_restriction',
  'limited',
  'paused',
  'recovering',
])
const qualities = new Set<string>([
  'unrated',
  'no_downgrade',
  'degraded',
  'severely_degraded',
])
function parseOfferHealth(value: unknown): CatalogOfferHealth | undefined {
  if (value === undefined) return undefined
  if (
    !isRecord(value) ||
    typeof value.availability !== 'string' ||
    typeof value.quality !== 'string' ||
    !availabilities.has(value.availability) ||
    !qualities.has(value.quality)
  ) {
    invalidResponse(endpoint)
  }
  return {
    availability: value.availability as CatalogAvailability,
    quality: value.quality as CatalogQuality,
  }
}
function parseChannelHealth(value: unknown): CatalogChannelHealth | undefined {
  if (value === undefined) return undefined
  if (!isRecord(value)) invalidResponse(endpoint)
  const visible_models = requiredStrictInteger(value.visible_models, endpoint)
  const affected_models = requiredStrictInteger(value.affected_models, endpoint)
  const evaluated_models = requiredStrictInteger(
    value.evaluated_models,
    endpoint
  )
  const unrated_models = requiredStrictInteger(value.unrated_models, endpoint)
  if (
    visible_models < 0 ||
    affected_models < 0 ||
    evaluated_models < 0 ||
    unrated_models < 0 ||
    affected_models + evaluated_models + unrated_models !== visible_models
  ) {
    invalidResponse(endpoint)
  }
  return {
    visible_models,
    affected_models,
    evaluated_models,
    unrated_models,
  }
}
function records(value: unknown): Record<string, unknown>[] {
  if (!Array.isArray(value) || value.some((item) => !isRecord(item)))
    invalidResponse(endpoint)
  return value as Record<string, unknown>[]
}
function decimal(value: unknown): string {
  if (!isDecimal(value) || value.startsWith('-')) invalidResponse(endpoint)
  return value
}
function price(value: Record<string, unknown>) {
  if (
    !['configured', 'default', 'zero', 'unavailable'].includes(
      String(value.price_status)
    ) ||
    !isRecord(value.pricing_values)
  )
    invalidResponse(endpoint)
  const pricing_values: CatalogPrices = {}
  for (const [key, amount] of Object.entries(value.pricing_values))
    pricing_values[key] = decimal(amount)
  if (
    value.price_status !== 'unavailable' &&
    !Object.keys(pricing_values).length
  )
    invalidResponse(endpoint)
  return {
    price_status: value.price_status as PriceStatus,
    pricing_values,
    ...(value.price_reason == null
      ? {}
      : { price_reason: requiredString(value.price_reason, endpoint) }),
  }
}
export function parseModelCatalog(value: unknown): ModelCatalog {
  if (
    !isRecord(value) ||
    value.schema_version !== 1 ||
    !isRecord(value.supported_endpoints)
  )
    invalidResponse(endpoint)
  const models = records(value.models).map((row): CatalogModel => ({
    model_name: requiredString(row.model_name, endpoint, false),
    description: requiredString(row.description ?? '', endpoint),
    icon: requiredString(row.icon ?? '', endpoint),
    ...(row.tags == null ? {} : { tags: requiredString(row.tags, endpoint) }),
    vendor_id: requiredStrictInteger(row.vendor_id ?? 0, endpoint),
    supported_endpoint_types: parseStringArray(
      row.supported_endpoint_types,
      endpoint
    ),
    ...price(row),
  }))
  const channels = records(value.channels).map((row): CatalogChannel => {
    if (!isRecord(row.lab) || typeof row.eligible !== 'boolean')
      invalidResponse(endpoint)
    const id = requiredStrictInteger(row.id, endpoint)
    const max_concurrency = requiredStrictInteger(row.max_concurrency, endpoint)
    if (id <= 0 || max_concurrency < 0) invalidResponse(endpoint)
    const health = parseChannelHealth(row.health)
    return {
      id,
      name: requiredString(row.name, endpoint),
      type: requiredStrictInteger(row.type, endpoint),
      status: requiredStrictInteger(row.status, endpoint),
      channel_ratio: decimal(row.channel_ratio),
      max_concurrency,
      eligible: row.eligible,
      filter_reason: requiredString(row.filter_reason ?? '', endpoint),
      lab: {
        id: requiredString(row.lab.id, endpoint),
        name: requiredString(row.lab.name, endpoint),
        source: requiredString(row.lab.source, endpoint),
      },
      ...(health ? { health } : {}),
    }
  })
  const names = new Set(models.map((model) => model.model_name))
  const ids = new Set(channels.map((channel) => channel.id))
  if (names.size !== models.length || ids.size !== channels.length)
    invalidResponse(endpoint)
  const offers = records(value.offers).map((row): CatalogOffer => {
    const model_id = requiredString(row.model_id, endpoint, false)
    const channel_id = requiredStrictInteger(row.channel_id, endpoint)
    if (!names.has(model_id) || !ids.has(channel_id)) invalidResponse(endpoint)
    const health = parseOfferHealth(row.health)
    return {
      model_id,
      channel_id,
      channel_ratio: decimal(row.channel_ratio),
      supported_endpoint_types: parseStringArray(
        row.supported_endpoint_types,
        endpoint
      ),
      ...price(row),
      ...(health ? { health } : {}),
    }
  })

  // Validate cross-field consistency between channel health summaries and their
  // deduplicated offers so contradictory payloads never render misleading pills.
  const offersByChannel = new Map<number, CatalogOffer[]>()
  for (const offer of offers) {
    const list = offersByChannel.get(offer.channel_id)
    if (list) list.push(offer)
    else offersByChannel.set(offer.channel_id, [offer])
  }
  for (const ch of channels) {
    const chOffers = offersByChannel.get(ch.id) ?? []
    const withHealth = chOffers.filter((offer) => offer.health !== undefined)
    if (ch.health === undefined) {
      if (withHealth.length > 0) invalidResponse(endpoint)
      continue
    }
    if (withHealth.length !== chOffers.length) invalidResponse(endpoint)
    const seenModels = new Map<string, CatalogOfferHealth>()
    for (const offer of chOffers) {
      const existing = seenModels.get(offer.model_id)
      if (
        existing &&
        (existing.availability !== offer.health!.availability ||
          existing.quality !== offer.health!.quality)
      ) {
        invalidResponse(endpoint)
      }
      seenModels.set(offer.model_id, offer.health!)
    }
    let affected = 0
    let evaluated = 0
    let unrated = 0
    for (const health of seenModels.values()) {
      if (
        health.availability === 'paused' ||
        health.availability === 'limited' ||
        health.availability === 'recovering' ||
        health.quality === 'degraded' ||
        health.quality === 'severely_degraded'
      ) {
        affected++
      } else if (
        health.availability === 'no_restriction' &&
        health.quality === 'no_downgrade'
      ) {
        evaluated++
      } else {
        unrated++
      }
    }
    if (
      ch.health.visible_models !== seenModels.size ||
      ch.health.affected_models !== affected ||
      ch.health.evaluated_models !== evaluated ||
      ch.health.unrated_models !== unrated
    ) {
      invalidResponse(endpoint)
    }
  }
  return {
    schema_version: 1,
    models,
    channels,
    offers,
    vendors: records(value.vendors).map((vendor) => ({
      id: requiredStrictInteger(vendor.id, endpoint),
      name: requiredString(vendor.name, endpoint),
      ...(vendor.icon == null
        ? {}
        : { icon: requiredString(vendor.icon, endpoint) }),
    })),
    supported_endpoints: Object.fromEntries(
      Object.entries(value.supported_endpoints).map(([key, row]) => {
        if (!isRecord(row)) invalidResponse(endpoint)
        return [
          key,
          {
            path: requiredString(row.path, endpoint, false),
            method: requiredString(row.method, endpoint, false),
          },
        ]
      })
    ),
    server_address: requiredString(value.server_address, endpoint),
    generated_at: requiredStrictInteger(value.generated_at, endpoint),
  }
}
export async function getModelCatalog(
  tokenId?: number,
  signal?: AbortSignal
): Promise<ModelCatalog> {
  return parseModelCatalog(
    await api.get(
      endpoint,
      tokenId == null ? undefined : { token_id: tokenId },
      { signal }
    )
  )
}
