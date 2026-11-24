import { compareReferencePrices } from '@/features/pricing/reference'
import type {
  CatalogModel,
  CatalogOffer,
  ModelCatalog,
} from '@/api/modelCatalog'
import type {
  MarketModel,
  MarketModelType,
  MarketOffer,
  MarketPricing,
  ModelMarketCatalog,
  ModelMarketSort,
  ModelMarketViewMode,
} from '@/types/modelMarket'

export const PLATFORM_VENDOR = '__platform__'
export function formatMarketRatio(value: string): string {
  const [integer, fraction = ''] = value.split('.')
  return `${integer}.${fraction.replace(/0+$/, '') || '0'}×`
}
export function isAudioEndpoint(endpoint: string): boolean {
  return endpoint === 'realtime' || endpoint.includes('audio')
}
export function normalizeMarketView(value: unknown): ModelMarketViewMode {
  return value === 'channels' || value === 'comparison' ? value : 'models'
}
function modelType(endpoints: string[]): MarketModelType {
  if (endpoints.includes('image-generation')) return 'image'
  if (endpoints.includes('embeddings')) return 'embedding'
  if (endpoints.includes('jina-rerank')) return 'rerank'
  if (endpoints.includes('openai-video')) return 'video'
  if (endpoints.some(isAudioEndpoint)) return 'audio'
  return 'chat'
}
export function projectMarketPricing(
  raw: CatalogModel | CatalogOffer,
  id: string
): MarketPricing {
  const dimensions: Record<string, string> = {
    cache: 'cacheRead',
    cache_create: 'cacheWrite',
    cache_create_1h: 'cacheWrite1h',
    cache_create_12h: 'cacheWrite12h',
    image_cache: 'imageCache',
    image_output: 'imageOutput',
    audio_input: 'audioInput',
    audio_output: 'audioOutput',
  }
  const priceLines = Object.entries(raw.pricing_values).map(
    ([key, amount]) => ({
      key: dimensions[key] ?? key,
      amount,
      unit:
        key === 'request'
          ? ('per_call' as const)
          : ('per_million_tokens' as const),
    })
  )
  return {
    id,
    priceStatus: raw.price_status,
    priceReason: 'price_reason' in raw ? raw.price_reason : undefined,
    billingMode:
      raw.price_status === 'unavailable'
        ? 'unconfigured'
        : raw.pricing_values.request != null
          ? 'per_call'
          : 'token',
    priceLines,
    referencePrices: priceLines.map((line) => ({
      ...line,
      source: 'configured' as const,
    })),
    endpointTypes: raw.supported_endpoint_types,
  }
}
export function parseModelTags(raw?: string): string[] {
  if (!raw) return []
  const seen = new Set<string>()
  const result: string[] = []
  for (const part of raw.split(',')) {
    const tag = part.trim()
    if (!tag || seen.has(tag)) continue
    seen.add(tag)
    result.push(tag)
  }
  return result
}

export function buildModelMarketCatalog(
  response: ModelCatalog
): ModelMarketCatalog {
  const channels = response.channels.map((row) => ({
    id: row.id,
    name: row.name,
    ratio: row.channel_ratio,
  }))
  const byChannel = new Map(channels.map((row) => [row.id, row]))
  const models = response.models.map((model): MarketModel => {
    const baseline = projectMarketPricing(model, model.model_name)
    return {
      id: model.model_name,
      name: model.model_name,
      description: model.description,
      icon: model.icon,
      tags: parseModelTags(model.tags),
      vendor:
        response.vendors.find((vendor) => vendor.id === model.vendor_id)
          ?.name || PLATFORM_VENDOR,
      type: modelType(model.supported_endpoint_types),
      endpointTypes: model.supported_endpoint_types,
      endpoints: model.supported_endpoint_types.map((type) => ({
        type,
        path:
          response.supported_endpoints[type]?.path
            .replaceAll('{model}', model.model_name)
            .replaceAll(':model', model.model_name) ?? '',
        method: response.supported_endpoints[type]?.method ?? '',
      })),
      baselines: [baseline],
      offers: response.offers
        .filter((offer) => offer.model_id === model.model_name)
        .map((offer): MarketOffer => ({
          id: `${offer.model_id}:${offer.channel_id}`,
          modelId: offer.model_id,
          channelId: offer.channel_id,
          channelName: byChannel.get(offer.channel_id)!.name,
          ratio: offer.channel_ratio,
          pricing: projectMarketPricing(
            offer,
            `${offer.model_id}:${offer.channel_id}`
          ),
          baseline,
          endpointTypes: offer.supported_endpoint_types,
        })),
    }
  })
  return {
    models,
    channels,
    serverAddress: response.server_address,
    telemetry: {
      status: 'unavailable',
      reason: 'channel_metrics_not_collected',
    },
  }
}

export function compareMarketOffers(
  a: MarketOffer,
  b: MarketOffer,
  sort: ModelMarketSort
): number {
  if (sort !== 'default' && sort !== 'name') {
    const audio = (offer: MarketOffer) =>
      offer.endpointTypes.length > 0 &&
      offer.endpointTypes.every(isAudioEndpoint)
    return compareReferencePrices(
      a.pricing.referencePrices ?? [],
      b.pricing.referencePrices ?? [],
      sort,
      audio(a),
      audio(b)
    )
  }
  // API order follows the same configured channel priority as model discovery.
  return (
    Number(a.pricing.billingMode === 'unconfigured') -
    Number(b.pricing.billingMode === 'unconfigured')
  )
}
