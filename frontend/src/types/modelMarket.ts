import type { CatalogModel, PricingCatalog } from '@/api/publicCatalog'
import type { PriceSort } from './pricingReference'

export type ModelMarketViewMode = 'models' | 'channels' | 'comparison'
export type ModelMarketSort = PriceSort
export type MarketBillingMode =
  'token' | 'per_call' | 'dynamic' | 'unconfigured'
export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'audio' | 'video' | 'rerank' | 'other'

export interface MarketGroup {
  name: string
  description: string
  ratio: number | 'auto'
  models: string[]
}

export interface MarketModel {
  id: string
  name: string
  description: string
  vendor: string
  tags: string[]
  types: MarketModelType[]
  pricing: CatalogModel
  billingMode: MarketBillingMode
  groups: MarketGroup[]
  endpoints: { type: string; path: string; method: string }[]
}

export interface ModelMarketCatalog {
  models: MarketModel[]
  groups: MarketGroup[]
  missingPrices: string[]
  issues: PricingCatalog['issues']
}
