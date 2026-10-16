import type { ModelCapabilities } from '@/api/modelManifest'
import type { PricingValues } from '@/api/pricingConfig'
import type { BillingUsageSchema } from '@/features/pricing/types'

export type ModelMarketViewMode = 'models' | 'channels' | 'comparison'
export type ModelMarketSort = 'default' | 'name' | 'input' | 'call'
export type MarketBillingMode =
  'token' | 'per_call' | 'dynamic' | 'unconfigured'
export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'audio' | 'video' | 'rerank'

export interface MarketPricingContract {
  id: string
  version: string
  configured: boolean
  values: PricingValues
  plugin_key?: string
  plugin_name?: string
  supported_endpoint_types: string[]
  usage_schema?: BillingUsageSchema
  cache_write_mode?: string
  billing_details?: Record<string, unknown>
}

export interface MarketChannel {
  id: number
  name: string
  ratio: string
}

export interface MarketModelContract {
  id: string
  description: string
  vendor: string
  icon: string
  capabilities: ModelCapabilities | null
  supported_endpoint_types: string[]
  baselines: MarketPricingContract[]
}

export interface MarketOfferContract {
  id: string
  model_id: string
  channel_id: number
  pricing_id: string
  supported_endpoint_types: string[]
}

export interface ModelMarketResponse {
  schema_version: 1
  models: MarketModelContract[]
  channels: MarketChannel[]
  offers: MarketOfferContract[]
  supported_endpoints: Record<string, { path: string; method: string }>
  telemetry: { status: 'unavailable'; reason: 'channel_metrics_not_collected' }
}

/** Values stay exact; labels and formatting are applied by the shared price UI. */
export interface MarketPriceLine {
  key: string
  amount: string
  unit: 'per_million_tokens' | 'per_call' | 'per_image'
}

export interface MarketPricing {
  id: string
  version: string
  billingMode: MarketBillingMode
  priceLines: MarketPriceLine[]
  expression: string
  pluginKey: string
  pluginName: string
  endpointTypes: string[]
  usageSchema?: BillingUsageSchema
  cacheWriteMode?: string
  billingDetails?: Record<string, unknown>
}

export interface MarketOffer {
  id: string
  modelId: string
  channelId: number
  channelName: string
  ratio: string
  pricing: MarketPricing
  baseline: MarketPricing
  endpointTypes: string[]
}

export interface MarketModel {
  id: string
  name: string
  vendor: string
  icon: string
  description: string
  type: MarketModelType
  capabilities: ModelCapabilities | null
  endpointTypes: string[]
  endpoints: { type: string; path: string; method: string }[]
  baselines: MarketPricing[]
  offers: MarketOffer[]
}

export interface ModelMarketCatalog {
  models: MarketModel[]
  channels: MarketChannel[]
  telemetry: ModelMarketResponse['telemetry']
}

export interface MarketChannelShelf {
  channel: MarketChannel
  items: { model: MarketModel; offers: MarketOffer[] }[]
}
