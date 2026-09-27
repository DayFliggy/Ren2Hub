import { compareDecimal } from '@/utils/decimal'
import { computed, ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import {
  parsePerfMetricsSummary,
  parsePricingModels,
  parseUserModels,
  type PerfModelSummaryContract,
  type PricingModelContract,
} from '@/api/liveContracts'
import { ApiError } from '@/api/types'
import type { MarketModel } from '@/types/console'
import { vendorMeta } from '@/constants/console'
import { useToast } from '@/composables/useToast'

// Prefixed to stay distinct from useMarketplace's same-named exports — the two
// composables cover different catalogs (models vs merchant listings).
export type ModelMarketViewMode = 'grid' | 'list'
export type ModelMarketSort = 'default' | 'latency' | 'tps' | 'price' | 'health'

export interface ModelMarketCatalog {
  models: MarketModel[]
  vendors: string[]
}

export interface VendorGroup {
  vendor: string
  tagline: string
  models: MarketModel[]
  healthy: number // health >= 95
  degraded: number // 80..94
  down: number // < 80
}

function modelType(endpoints: string[]): MarketModel['type'] {
  if (endpoints.includes('image-generation')) return 'image'
  if (endpoints.includes('embeddings')) return 'embedding'
  if (endpoints.includes('jina-rerank')) return 'rerank'
  if (endpoints.includes('openai-video')) return 'video'
  if (endpoints.some((endpoint) => endpoint.includes('audio'))) return 'audio'
  return 'chat'
}

function modelPrice(pricing: PricingModelContract): MarketModel['price'] {
  const offer = pricing.offers[0]!
  const version = offer.price_version
  if (version.mode === 'free') return { input: '0', output: '0' }
  const names: Record<string, keyof MarketModel['price']> = {
    p: 'input',
    c: 'output',
    cr: 'cache_read',
    cc: 'cache_write',
    calls: 'per_call',
  }
  const result: MarketModel['price'] = {}
  for (const line of offer.display_prices) {
    const key = names[line.quantity]
    if (
      key &&
      !line.tier_mode &&
      key !== 'tiers' &&
      (line.unit === 'per_million_tokens' ||
        (key === 'per_call' && line.unit === 'per_call'))
    )
      result[key] = line.price
  }
  return result
}

export function buildLiveModelCatalog(
  names: string[],
  pricing: PricingModelContract[],
  performance: PerfModelSummaryContract[]
): ModelMarketCatalog {
  const pricingByName = new Map(pricing.map((item) => [item.model_name, item]))
  const performanceByName = new Map(
    performance.map((item) => [item.model_name, item])
  )
  const models = names.map((name, index) => {
    const price = pricingByName.get(name)
    const metrics = performanceByName.get(name)
    const primary = price?.offers[0]?.price_version
    const vendor = price?.owner_by.trim() || '平台'
    return {
      id: index + 1,
      name,
      vendor,
      type: modelType(price?.supported_endpoint_types ?? []),
      billing:
        primary?.mode === 'expression'
          ? ('tiered' as const)
          : primary?.mode === 'free'
            ? ('free' as const)
            : primary?.prices.some(
                  (line) => !['p', 'c', 'cr', 'cc'].includes(line.quantity)
                )
              ? primary.prices.length === 1 &&
                primary.prices[0]?.quantity === 'calls'
                ? ('per_call' as const)
                : ('unit_price' as const)
              : ('token' as const),
      price: price ? modelPrice(price) : {},
      pricing: primary,
      offers: price?.offers,
      context: 0,
      tagline: price?.description ?? '',
      latency: metrics ? Math.max(0, metrics.avg_latency_ms / 1000) : 0,
      tps: metrics ? Math.max(0, metrics.avg_tps) : 0,
      health: metrics ? Math.min(100, Math.max(0, metrics.success_rate)) : 0,
    }
  })
  return {
    models,
    vendors: [...new Set(models.map((model) => model.vendor))],
  }
}

/**
 * Sort key for the price ordering. `input` ($/1M tokens) and `per_call`
 * ($/call) are different units and must not be compared directly, so per-call
 * models are grouped after all token-priced ones (bucket 0 vs 1) and only
 * compared within their own unit.
 */
function priceKey(m: MarketModel): [number, string] {
  const p = m.price
  const tokenPrice = p.input ?? p.tiers?.[0]?.input
  if (tokenPrice != null) return [0, tokenPrice]
  if (p.per_call != null) return [1, p.per_call]
  return [2, '0']
}

/** Compare two [bucket, price] keys: bucket first, then price within bucket. */
function comparePrice(a: MarketModel, b: MarketModel): number {
  const [ba, pa] = priceKey(a)
  const [bb, pb] = priceKey(b)
  return ba !== bb ? ba - bb : compareDecimal(pa, pb)
}

export function useModelMarket() {
  const { t } = useI18n()
  const toast = useToast()
  const loading = ref(true)
  const catalog = ref<ModelMarketCatalog | null>(null)

  const keyword = ref('')
  const vendor = ref('')
  const type = ref('')
  const sort = ref<ModelMarketSort>('default')
  const view = useLocalStorage<ModelMarketViewMode>(
    'ren2hub_models_view',
    'grid'
  )

  async function load() {
    loading.value = true
    try {
      const names = parseUserModels(await api.get<unknown>('/api/user/models'))
      const [pricingResult, performanceResult] = await Promise.allSettled([
        api.get<unknown>('/api/pricing').then(parsePricingModels),
        api
          .get<unknown>('/api/perf-metrics/summary')
          .then(parsePerfMetricsSummary),
      ])
      if (
        pricingResult.status === 'rejected' ||
        performanceResult.status === 'rejected'
      ) {
        toast.warning(t('models.metadataUnavailable'))
      }
      catalog.value = buildLiveModelCatalog(
        names,
        pricingResult.status === 'fulfilled' ? pricingResult.value : [],
        performanceResult.status === 'fulfilled' ? performanceResult.value : []
      )
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : t('common.failed')
      )
    } finally {
      loading.value = false
    }
  }

  const filtered = computed(() => {
    const all = catalog.value?.models ?? []
    const kw = keyword.value.trim().toLowerCase()
    const list = all.filter((m) => {
      if (
        kw &&
        !m.name.toLowerCase().includes(kw) &&
        !m.vendor.toLowerCase().includes(kw)
      )
        return false
      if (vendor.value && m.vendor !== vendor.value) return false
      if (type.value && m.type !== type.value) return false
      return true
    })
    if (sort.value === 'default') return list
    const sorted = [...list]
    switch (sort.value) {
      case 'latency':
        sorted.sort((a, b) => a.latency - b.latency)
        break
      case 'tps':
        sorted.sort((a, b) => b.tps - a.tps)
        break
      case 'price':
        sorted.sort(comparePrice)
        break
      case 'health':
        sorted.sort((a, b) => b.health - a.health)
        break
    }
    return sorted
  })

  /** Group filtered models by vendor, preserving catalog vendor order. */
  const groups = computed<VendorGroup[]>(() => {
    const order = catalog.value?.vendors ?? []
    const byVendor = new Map<string, MarketModel[]>()
    for (const m of filtered.value) {
      const bucket = byVendor.get(m.vendor)
      if (bucket) bucket.push(m)
      else byVendor.set(m.vendor, [m])
    }
    return order
      .filter((v) => byVendor.has(v))
      .map((v) => {
        const models = byVendor.get(v)!
        return {
          vendor: v,
          tagline: vendorMeta[v] ?? '',
          models,
          healthy: models.filter((m) => m.health >= 95).length,
          degraded: models.filter((m) => m.health >= 80 && m.health < 95)
            .length,
          down: models.filter((m) => m.health > 0 && m.health < 80).length,
        }
      })
  })

  const resultCount = computed(() => filtered.value.length)
  const hasResults = computed(() => resultCount.value > 0)

  const vendorOptions = computed(() => catalog.value?.vendors ?? [])

  return {
    loading,
    catalog,
    keyword,
    vendor,
    type,
    sort,
    view,
    filtered,
    groups,
    resultCount,
    hasResults,
    vendorOptions,
    load,
  }
}
