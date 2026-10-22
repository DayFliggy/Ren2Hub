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
export interface CatalogPrices {
  input?: string
  output?: string
  request?: string
}
export interface CatalogModel {
  model_name: string
  description: string
  icon: string
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
}
export interface CatalogOffer {
  model_id: string
  channel_id: number
  supported_endpoint_types: string[]
  channel_ratio: string
  price_status: PriceStatus
  pricing_values: CatalogPrices
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
  for (const key of ['input', 'output', 'request'] as const)
    if (value.pricing_values[key] != null)
      pricing_values[key] = decimal(value.pricing_values[key])
  if (
    value.price_status !== 'unavailable' &&
    !Object.keys(pricing_values).length
  )
    invalidResponse(endpoint)
  return { price_status: value.price_status as PriceStatus, pricing_values }
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
    vendor_id: requiredStrictInteger(row.vendor_id ?? 0, endpoint),
    supported_endpoint_types: parseStringArray(
      row.supported_endpoint_types,
      endpoint
    ),
    ...price(row),
    ...(row.price_reason == null
      ? {}
      : { price_reason: requiredString(row.price_reason, endpoint) }),
  }))
  const channels = records(value.channels).map((row): CatalogChannel => {
    if (!isRecord(row.lab) || typeof row.eligible !== 'boolean')
      invalidResponse(endpoint)
    const id = requiredStrictInteger(row.id, endpoint)
    const max_concurrency = requiredStrictInteger(row.max_concurrency, endpoint)
    if (id <= 0 || max_concurrency < 0) invalidResponse(endpoint)
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
    return {
      model_id,
      channel_id,
      channel_ratio: decimal(row.channel_ratio),
      supported_endpoint_types: parseStringArray(
        row.supported_endpoint_types,
        endpoint
      ),
      ...price(row),
    }
  })
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
