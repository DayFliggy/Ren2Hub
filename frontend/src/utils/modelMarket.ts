import type { PricingCatalog, CatalogModel } from '@/api/publicCatalog'
import {
  compareReferencePrices,
  publicPricingReferences,
} from '@/features/pricing/reference'
import type {
  MarketBillingMode,
  MarketGroup,
  MarketModel,
  MarketModelType,
  ModelMarketCatalog,
  ModelMarketSort,
  ModelMarketViewMode,
} from '@/types/modelMarket'

export function isAudioEndpoint(endpoint: string): boolean {
  return endpoint === 'realtime' || endpoint.includes('audio')
}

export function normalizeMarketView(value: string): ModelMarketViewMode {
  return value === 'channels' || value === 'comparison' ? value : 'models'
}

export function marketBillingMode(pricing: CatalogModel): MarketBillingMode {
  if (pricing.billing_mode === 'unconfigured') return 'unconfigured'
  if (pricing.billing_mode === 'tiered_expr') return 'dynamic'
  return pricing.quota_type === 1 ? 'per_call' : 'token'
}

function endpointTypes(endpoints: MarketModel['endpoints']): MarketModelType[] {
  const types = new Set<MarketModelType>()
  for (const endpoint of endpoints) {
    const path = endpoint.path
    if (endpoint.type === 'image-generation' || path.includes('/images/'))
      types.add('image')
    else if (endpoint.type === 'openai-video' || path.includes('/videos'))
      types.add('video')
    else if (endpoint.type === 'embeddings' || path.includes('/embeddings'))
      types.add('embedding')
    else if (endpoint.type === 'jina-rerank' || path.includes('/rerank'))
      types.add('rerank')
    else if (path.includes('/audio/')) types.add('audio')
    else if (
      [
        'openai',
        'openai-response',
        'openai-response-compact',
        'anthropic',
        'gemini',
      ].includes(endpoint.type)
    )
      types.add('chat')
  }
  return types.size ? [...types] : ['other']
}

export function buildModelMarketCatalog(
  pricing: PricingCatalog,
  groups: MarketGroup[]
): ModelMarketCatalog {
  const allowed = new Set(groups.flatMap((group) => group.models))
  const models = pricing.models
    .filter((model) => allowed.has(model.model_name))
    .map((model): MarketModel => {
      const endpoints = model.supported_endpoint_types.map((type) => ({
        type,
        path: pricing.endpoints[type]?.path ?? '',
        method: pricing.endpoints[type]?.method ?? '',
      }))
      return {
        id: model.model_name,
        name: model.model_name,
        description: model.description,
        vendor:
          pricing.vendors.find((vendor) => vendor.id === model.vendor_id)
            ?.name || model.owner_by,
        tags: model.tags
          .split(/[,，]/)
          .map((tag) => tag.trim())
          .filter(Boolean),
        types: endpointTypes(endpoints),
        pricing: model,
        billingMode: marketBillingMode(model),
        groups: groups.filter((group) =>
          group.models.includes(model.model_name)
        ),
        endpoints,
      }
    })
  const priced = new Set(models.map((model) => model.id))
  return {
    models,
    groups,
    missingPrices: [...allowed].filter((name) => !priced.has(name)),
    issues: pricing.issues.filter(
      (issue) => issue.model_name !== null && allowed.has(issue.model_name)
    ),
  }
}

export function compareMarketModels(
  a: MarketModel,
  b: MarketModel,
  sort: ModelMarketSort,
  groupName: string
): number {
  if (sort === 'default') return 0
  if (sort === 'name') return a.name.localeCompare(b.name)
  const references = (model: MarketModel) => {
    const group = model.groups.find((item) => item.name === groupName)
    // Auto selection does not have a single price; no invented multiplier or quote.
    if (groupName && (!group || group.ratio === 'auto')) return []
    return publicPricingReferences(
      model.pricing,
      group?.ratio as number | undefined
    )
  }
  return (
    compareReferencePrices(
      references(a),
      references(b),
      sort,
      a.types.includes('audio'),
      b.types.includes('audio')
    ) || a.name.localeCompare(b.name)
  )
}
