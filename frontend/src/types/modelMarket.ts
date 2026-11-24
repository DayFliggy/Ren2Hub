import type { PriceSort, ReferencePrice } from './pricingReference'
import type { BillingUsageSchema } from '@/features/pricing/types'

export type ModelMarketViewMode = 'models' | 'channels' | 'comparison'
export type ModelMarketSort = PriceSort
export type MarketBillingMode =
  'token' | 'per_call' | 'dynamic' | 'unconfigured'
export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'audio' | 'video' | 'rerank'

export interface MarketChannel {
  id: number
  name: string
  ratio: string
}

/** Values stay exact; labels and formatting are applied by the shared price UI. */
export interface MarketPriceLine {
  key: string
  amount: string
  unit: 'per_million_tokens' | 'per_call' | 'per_image'
}

export interface MarketPricing {
  priceStatus?: import('@/api/modelCatalog').PriceStatus
  priceReason?: string
  referencePrices?: ReferencePrice[]
  id: string
  billingMode: MarketBillingMode
  priceLines: MarketPriceLine[]
  expression?: string
  pluginKey?: string
  pluginName?: string
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
  tags: string[]
  type: MarketModelType
  endpointTypes: string[]
  endpoints: { type: string; path: string; method: string }[]
  baselines: MarketPricing[]
  offers: MarketOffer[]
}

export interface ModelMarketCatalog {
  models: MarketModel[]
  channels: MarketChannel[]
  serverAddress: string
  telemetry: { status: 'unavailable'; reason: 'channel_metrics_not_collected' }
}

export interface MarketChannelShelf {
  channel: MarketChannel
  items: { model: MarketModel; offers: MarketOffer[] }[]
}

/** Per-model perf-metrics state. "empty" means the model had no sampled
 * requests in the window; it is never rendered as a 0% success rate. */
export type ModelPerformanceState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'empty' }
  | {
      status: 'ready'
      summary: import('@/api/liveContracts').PerfModelSummaryContract
    }
