import { computed, ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { getModelMarket } from '@/api/modelMarket'
import { useLatestRequest } from './useLatestRequest'
import { compareMarketModels, normalizeMarketView } from '@/utils/modelMarket'
import type {
  MarketBillingMode,
  MarketModelType,
  ModelMarketCatalog,
  ModelMarketSort,
} from '@/types/modelMarket'

export function useModelMarket() {
  const loading = ref(true),
    error = ref('')
  const catalog = ref<ModelMarketCatalog | null>(null)
  const keyword = ref(''),
    group = ref(''),
    vendor = ref('')
  const billing = ref<MarketBillingMode | ''>(''),
    type = ref<MarketModelType | ''>('')
  const sort = ref<ModelMarketSort>('default')
  const savedView = useLocalStorage<string>('ren2hub_models_view', 'models')
  const view = computed({
    get: () => normalizeMarketView(savedView.value),
    set: (value: string) => {
      savedView.value = normalizeMarketView(value)
    },
  })
  const request = useLatestRequest()
  async function load() {
    loading.value = true
    error.value = ''
    // Do not retain data from a previous permission scope while refreshing.
    catalog.value = null
    const result = await request.run(getModelMarket)
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      error.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
      return
    }
    catalog.value = result.value
    if (!result.value.groups.some((item) => item.name === group.value))
      group.value = ''
    if (!result.value.models.some((item) => item.vendor === vendor.value))
      vendor.value = ''
  }
  const filtered = computed(() => {
    const query = keyword.value.trim().toLowerCase()
    return (catalog.value?.models ?? [])
      .filter(
        (model) =>
          (!group.value ||
            model.groups.some((item) => item.name === group.value)) &&
          (!vendor.value || model.vendor === vendor.value) &&
          (!billing.value || model.billingMode === billing.value) &&
          (!type.value || model.types.includes(type.value)) &&
          (!query ||
            `${model.name} ${model.description} ${model.vendor} ${model.tags.join(' ')}`
              .toLowerCase()
              .includes(query))
      )
      .sort((a, b) => compareMarketModels(a, b, sort.value, group.value))
  })
  const visibleGroups = computed(() =>
    (catalog.value?.groups ?? []).filter(
      (item) => !group.value || item.name === group.value
    )
  )
  const selectedGroup = computed(() =>
    catalog.value?.groups.find((item) => item.name === group.value)
  )
  const vendors = computed(() =>
    [
      ...new Set(
        catalog.value?.models.map((model) => model.vendor).filter(Boolean) ?? []
      ),
    ].sort()
  )
  function resetFilters() {
    keyword.value = ''
    group.value = ''
    vendor.value = ''
    billing.value = ''
    type.value = ''
    sort.value = 'default'
  }
  return {
    loading,
    error,
    catalog,
    keyword,
    group,
    vendor,
    billing,
    type,
    sort,
    view,
    filtered,
    visibleGroups,
    selectedGroup,
    vendors,
    load,
    resetFilters,
  }
}
