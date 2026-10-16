import { catalogPrices, type CatalogPricingInput } from '@/api/publicCatalog'
import { compareDecimal, multiplyDecimal } from '@/utils/decimal'
import type {
  MarketBillingMode,
  MarketModel,
  MarketModelType,
  MarketOffer,
  MarketPricing,
  MarketPriceLine,
  MarketPricingContract,
  ModelMarketCatalog,
  ModelMarketResponse,
  ModelMarketSort,
  ModelMarketViewMode,
} from '@/types/modelMarket'

export const PLATFORM_VENDOR = '__platform__'

/** Display validated decimal ratios without converting to floating point. */
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
  raw: MarketPricingContract,
  ratio = '1'
): MarketPricing {
  const values = raw.values
  const nativeAudio =
    !raw.plugin_key &&
    raw.supported_endpoint_types.length > 0 &&
    raw.supported_endpoint_types.every(isAudioEndpoint)
  const billingMode: MarketBillingMode = !raw.configured
    ? 'unconfigured'
    : values['billing_setting.billing_mode'] === 'tiered_expr'
      ? 'dynamic'
      : values.ModelPrice !== undefined
        ? 'per_call'
        : 'token'
  const price: CatalogPricingInput = {
    quota_type: billingMode === 'per_call' ? 1 : 0,
    billing_mode:
      billingMode === 'dynamic'
        ? 'tiered_expr'
        : billingMode === 'unconfigured'
          ? 'unconfigured'
          : 'ratio',
    model_price: values.ModelPrice,
    model_ratio: values.ModelRatio,
    completion_ratio: values.CompletionRatio,
    cache_ratio: values.CacheRatio,
    create_cache_ratio: values.CreateCacheRatio,
    image_ratio: values.ImageRatio,
    audio_ratio: values.AudioRatio ?? (nativeAudio ? '1' : undefined),
    audio_completion_ratio: values.AudioCompletionRatio,
  }
  const priceLines: MarketPriceLine[] = catalogPrices(price, 'M')
    .filter(
      (line) =>
        !nativeAudio || !['cacheRead', 'cacheWrite', 'image'].includes(line.key)
    )
    .map((line) => ({
      key: line.key,
      amount: multiplyDecimal(line.value, ratio),
      unit:
        line.unit === 'per_call'
          ? raw.billing_details?.image_count === true
            ? 'per_image'
            : 'per_call'
          : 'per_million_tokens',
    }))
  // A generic cache-write fallback is not a configured charge.
  if (raw.cache_write_mode === 'none') {
    const index = priceLines.findIndex((line) => line.key === 'cacheWrite')
    if (index >= 0) priceLines.splice(index, 1)
  }
  if (raw.cache_write_mode === 'claude_ttl') {
    const write = priceLines.find((line) => line.key === 'cacheWrite')
    if (write)
      priceLines.push({
        ...write,
        key: 'cacheWrite1h',
        amount: multiplyDecimal(write.amount, '1.6'),
      })
  }
  return {
    id: raw.id,
    version: raw.version,
    billingMode,
    priceLines,
    expression:
      billingMode === 'dynamic'
        ? String(values['billing_setting.billing_expr'] ?? '')
        : '',
    pluginKey: raw.plugin_key ?? '',
    pluginName: raw.plugin_name ?? '',
    endpointTypes: raw.supported_endpoint_types,
    usageSchema: raw.usage_schema,
    cacheWriteMode: raw.cache_write_mode,
    billingDetails: raw.billing_details,
  }
}

export function buildModelMarketCatalog(
  response: ModelMarketResponse
): ModelMarketCatalog {
  const channels = new Map(
    response.channels.map((channel) => [channel.id, channel])
  )
  const byModel = new Map<string, typeof response.offers>()
  for (const offer of response.offers) {
    const offers = byModel.get(offer.model_id) ?? []
    offers.push(offer)
    byModel.set(offer.model_id, offers)
  }
  const models = response.models.map((model): MarketModel => {
    const baselines = model.baselines.map((price) =>
      projectMarketPricing(price)
    )
    const prices = new Map(model.baselines.map((price) => [price.id, price]))
    const byPrice = new Map(baselines.map((price) => [price.id, price]))
    const offers = (byModel.get(model.id) ?? []).map((offer): MarketOffer => {
      const channel = channels.get(offer.channel_id)!
      const raw = prices.get(offer.pricing_id)!
      return {
        id: offer.id,
        modelId: model.id,
        channelId: channel.id,
        channelName: channel.name,
        ratio: channel.ratio,
        pricing: projectMarketPricing(raw, channel.ratio),
        baseline: byPrice.get(raw.id)!,
        endpointTypes: offer.supported_endpoint_types,
      }
    })
    return {
      id: model.id,
      name: model.id,
      vendor: model.vendor || PLATFORM_VENDOR,
      icon: model.icon,
      description: model.description,
      type: modelType(model.supported_endpoint_types),
      capabilities: model.capabilities,
      endpointTypes: model.supported_endpoint_types,
      endpoints: model.supported_endpoint_types.map((type) => ({
        type,
        path:
          response.supported_endpoints[type]?.path.replaceAll(
            '{model}',
            model.id
          ) ?? '',
        method: response.supported_endpoints[type]?.method ?? '',
      })),
      baselines,
      offers,
    }
  })
  return { models, channels: response.channels, telemetry: response.telemetry }
}

/** Do not order request, text-token and audio-token prices against each other. */
function offerPriceKey(
  offer: MarketOffer,
  sort: ModelMarketSort
): [number, string] {
  const lines = offer.pricing.priceLines
  if (sort === 'call') {
    const line = lines.find((item) => item.key === 'request')
    return line ? [line.unit === 'per_image' ? 1 : 0, line.amount] : [2, '0']
  }
  const audio =
    offer.endpointTypes.length > 0 && offer.endpointTypes.every(isAudioEndpoint)
  const line = lines.find(
    (item) => item.key === (audio ? 'audioInput' : 'input')
  )
  return line ? [audio ? 1 : 0, line.amount] : [2, '0']
}

export function compareMarketOffers(
  a: MarketOffer,
  b: MarketOffer,
  sort: ModelMarketSort
): number {
  if (sort === 'input' || sort === 'call') {
    const [bucketA, priceA] = offerPriceKey(a, sort)
    const [bucketB, priceB] = offerPriceKey(b, sort)
    return bucketA - bucketB || compareDecimal(priceA, priceB)
  }
  // API order follows the same configured channel priority as model discovery.
  return (
    Number(a.pricing.billingMode === 'unconfigured') -
    Number(b.pricing.billingMode === 'unconfigured')
  )
}
