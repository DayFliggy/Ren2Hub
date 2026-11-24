import { computed, ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { getModelMarket, getModelPerformance } from '@/api/modelMarket'
import type { PerfModelSummaryContract } from '@/api/liveContracts'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { ApiError } from '@/api/types'
import {
  buildModelMarketCatalog,
  compareMarketOffers,
  normalizeMarketView,
  PLATFORM_VENDOR,
} from '@/utils/modelMarket'
import type {
  MarketBillingMode,
  MarketChannelShelf,
  MarketModelType,
  ModelMarketCatalog,
  ModelMarketSort,
  ModelMarketViewMode,
  ModelPerformanceState,
} from '@/types/modelMarket'

type PerformanceFeed =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; models: Map<string, PerfModelSummaryContract> }

export { PLATFORM_VENDOR } from '@/utils/modelMarket'
export type { ModelMarketViewMode, ModelMarketSort } from '@/types/modelMarket'

export function useModelMarket() {
  const { t } = useI18n()
  const loading = ref(true)
  const error = ref('')
  const catalog = ref<ModelMarketCatalog | null>(null)
  const keyword = ref('')
  const channel = ref<number | ''>('')
  const billing = ref<MarketBillingMode | ''>('')
  const type = ref<MarketModelType | ''>('')
  const sort = ref<ModelMarketSort>('default')
  const savedView = useLocalStorage<string>('ren2hub_models_view', 'models')
  savedView.value = normalizeMarketView(savedView.value)
  const view = computed<ModelMarketViewMode>({
    get: () => normalizeMarketView(savedView.value),
    set: (value) => {
      savedView.value = normalizeMarketView(value)
    },
  })
  const request = useLatestRequest()
  const performance = ref<PerformanceFeed>({ status: 'loading' })
  const performanceRequest = useLatestRequest()

  function vendorLabel(vendor: string): string {
    return vendor === PLATFORM_VENDOR ? t('models.platformVendor') : vendor
  }

  function failureMessage(cause: unknown): string {
    return cause instanceof ApiError ? cause.message : t('common.failed')
  }

  async function loadCatalog() {
    loading.value = true
    error.value = ''
    const result = await request.run(getModelMarket)
    if (result.stale) return
    loading.value = false
    if (result.ok) catalog.value = buildModelMarketCatalog(result.value)
    else {
      // Discard old permission-scoped data after a failed refresh.
      catalog.value = null
      error.value = failureMessage(result.error)
    }
  }

  /** Metrics load independently, so a failure never hides catalog prices. */
  async function loadPerformance() {
    performance.value = { status: 'loading' }
    const result = await performanceRequest.run(getModelPerformance)
    if (result.stale) return
    performance.value = result.ok
      ? {
          status: 'ready',
          models: new Map(result.value.map((row) => [row.model_name, row])),
        }
      : {
          status: 'error',
          message: failureMessage(result.error) || t('common.failed'),
        }
  }

  async function load() {
    await Promise.all([loadCatalog(), loadPerformance()])
  }

  function performanceFor(modelName: string): ModelPerformanceState {
    const feed = performance.value
    if (feed.status !== 'ready') return { status: feed.status }
    const summary = feed.models.get(modelName)
    return summary ? { status: 'ready', summary } : { status: 'empty' }
  }

  const performanceError = computed(() =>
    performance.value.status === 'error' ? performance.value.message : ''
  )

  const filtered = computed(() => {
    const query = keyword.value.trim().toLowerCase()
    const rows = (catalog.value?.models ?? [])
      .filter((model) => !type.value || model.type === type.value)
      .map((model) => {
        const offers = model.offers
          .filter(
            (offer) =>
              (!channel.value || offer.channelId === Number(channel.value)) &&
              (!billing.value || offer.pricing.billingMode === billing.value) &&
              (!query ||
                `${model.name} ${vendorLabel(model.vendor)} ${offer.channelName} ${model.tags.join(' ')}`
                  .toLowerCase()
                  .includes(query))
          )
          .sort((a, b) => compareMarketOffers(a, b, sort.value))
        const priceIds = new Set(offers.map((offer) => offer.baseline.id))
        return {
          ...model,
          offers,
          baselines: model.baselines.filter((price) => priceIds.has(price.id)),
        }
      })
      .filter((model) => model.offers.length)
    if (sort.value === 'name') rows.sort((a, b) => a.name.localeCompare(b.name))
    else if (sort.value !== 'default')
      rows.sort((a, b) =>
        compareMarketOffers(a.offers[0]!, b.offers[0]!, sort.value)
      )
    return rows
  })

  const shelves = computed<MarketChannelShelf[]>(() =>
    (catalog.value?.channels ?? [])
      .map((item) => {
        const items = filtered.value
          .map((model) => ({
            model,
            offers: model.offers.filter((offer) => offer.channelId === item.id),
          }))
          .filter((row) => row.offers.length)
        if (sort.value !== 'name')
          items.sort((a, b) =>
            compareMarketOffers(a.offers[0]!, b.offers[0]!, sort.value)
          )
        return { channel: item, items }
      })
      .filter((shelf) => shelf.items.length)
  )

  function resetFilters() {
    keyword.value = ''
    channel.value = ''
    billing.value = ''
    type.value = ''
    sort.value = 'default'
  }

  const visibleChannels = computed(() =>
    shelves.value.map((shelf) => shelf.channel)
  )
  const resultCount = computed(() => filtered.value.length)
  const offerCount = computed(() =>
    filtered.value.reduce((sum, model) => sum + model.offers.length, 0)
  )
  const hasResults = computed(() => resultCount.value > 0)
  const pricingIssues = computed(() =>
    (catalog.value?.models ?? [])
      .filter((model) =>
        model.offers.some(
          (offer) => offer.pricing.billingMode === 'unconfigured'
        )
      )
      .map((model) => model.id)
  )
  return {
    loading,
    error,
    catalog,
    keyword,
    channel,
    billing,
    type,
    sort,
    view,
    filtered,
    shelves,
    visibleChannels,
    resultCount,
    offerCount,
    hasResults,
    pricingIssues,
    performanceFor,
    performanceError,
    vendorLabel,
    load,
    loadPerformance,
    resetFilters,
  }
}
