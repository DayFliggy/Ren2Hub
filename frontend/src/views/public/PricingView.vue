<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { publicCatalogApi, type PricingCatalog } from '@/api/publicCatalog'
import { modelPricingMessages } from '@/i18n/modelPricing'
import {
  compareReferencePrices,
  publicPricingReferences,
} from '@/features/pricing/reference'
import { isAudioEndpoint } from '@/utils/modelMarket'
import { PRICE_SORTS, type PriceSort } from '@/types/pricingReference'
import { pricingReferenceMessages } from '@/i18n/pricingReference'
import PublicPage from '@/components/public/PublicPage.vue'
import ModelPriceItems from '@/components/console/models/ModelPriceItems.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
const messages = {
  'zh-CN': {
    ...modelPricingMessages['zh-CN'],
    reference: pricingReferenceMessages['zh-CN'],
  },
  en: { ...modelPricingMessages.en, reference: pricingReferenceMessages.en },
}
const { t } = useI18n({ useScope: 'local', messages }),
  route = useRoute()
const catalog = ref<PricingCatalog | null>(null),
  loading = ref(false),
  error = ref('')
const search = ref(typeof route.query.q === 'string' ? route.query.q : ''),
  vendor = ref(''),
  mode = ref(''),
  unit = ref<'M' | 'K'>(route.query.tokenUnit === 'K' ? 'K' : 'M'),
  sort = ref<PriceSort>('default'),
  page = ref(1),
  pageSize = ref(20)
let controller: AbortController | undefined
const detailId = computed(() => String(route.params.modelId ?? ''))
const detail = computed(() =>
  catalog.value?.models.find((model) => model.model_name === detailId.value)
)
const filteredModels = computed(() =>
  (catalog.value?.models ?? []).filter(
    (model) =>
      (!search.value ||
        `${model.model_name} ${model.description} ${model.tags}`
          .toLowerCase()
          .includes(search.value.toLowerCase())) &&
      (!vendor.value || String(model.vendor_id) === vendor.value) &&
      (!mode.value ||
        (model.billing_mode === 'unconfigured'
          ? 'unconfigured'
          : model.billing_mode === 'tiered_expr'
            ? 'expression'
            : model.quota_type === 1
              ? 'request'
              : 'token') === mode.value)
  )
)
const referencePrices = computed(
  () =>
    new Map(
      (catalog.value?.models ?? []).map((model) => [
        model.model_name,
        publicPricingReferences(model),
      ])
    )
)
const models = computed(() => {
  const rows = [...filteredModels.value]
  if (sort.value === 'name')
    return rows.sort((a, b) => a.model_name.localeCompare(b.model_name))
  if (sort.value === 'default') return rows
  const audio = (model: PricingCatalog['models'][number]) =>
    model.supported_endpoint_types.length > 0 &&
    model.supported_endpoint_types.every(isAudioEndpoint)
  return rows.sort((a, b) =>
    compareReferencePrices(
      referencePrices.value.get(a.model_name) ?? [],
      referencePrices.value.get(b.model_name) ?? [],
      sort.value,
      audio(a),
      audio(b)
    )
  )
})
const sortOptions = computed(() =>
  PRICE_SORTS.map((value) => ({ value, label: t(`reference.sorts.${value}`) }))
)
const sortModel = computed({
  get: () => sort.value,
  set: (value: string) => {
    sort.value = PRICE_SORTS.includes(value as PriceSort)
      ? (value as PriceSort)
      : 'default'
  },
})
const vendorOptions = computed(() =>
  (catalog.value?.vendors ?? []).map((item) => ({
    value: String(item.id),
    label: item.name,
  }))
)
const modeOptions = computed(() => [
  { value: 'expression', label: t('expression') },
  { value: 'token', label: t('perToken') },
  { value: 'request', label: t('perCall') },
  { value: 'unconfigured', label: t('unpriced') },
])
const unitOptions = [
  { value: 'M', label: '1M tokens' },
  { value: 'K', label: '1K tokens' },
]
// FilterSelect speaks strings; bridge to the typed price unit.
const unitModel = computed({
  get: () => unit.value,
  set: (value: string) => {
    unit.value = value === 'K' ? 'K' : 'M'
  },
})
const visible = computed(() =>
  models.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
watch([search, vendor, mode, sort, pageSize], () => {
  page.value = 1
})
async function load() {
  controller?.abort()
  controller = new AbortController()
  const request = controller
  loading.value = true
  error.value = ''
  try {
    const value = await publicCatalogApi.pricing(request.signal)
    if (!request.signal.aborted) catalog.value = value
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (controller === request) loading.value = false
  }
}
async function copy(name: string) {
  try {
    await navigator.clipboard.writeText(name)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
onMounted(load)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <PublicPage :title="detailId || t('title')">
    <!-- The list total is shown once, by the pagination below the grid. -->
    <template #actions
      ><RouterLink v-if="detailId" :to="{ name: 'pricing' }">{{
        t('title')
      }}</RouterLink></template
    >
    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <p v-if="loading" role="status">{{ t('loading') }}</p>
    <p
      v-if="catalog?.issues.length"
      role="status"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('unsupportedPrice') }}: {{ catalog.issues.length }}
    </p>
    <template v-if="detail"
      ><div class="public-pricing-detail">
        <VendorLogo :vendor="detail.owner_by" />
        <div>
          <h2>{{ detail.model_name }}</h2>
          <p>{{ detail.description }}</p>
        </div>
        <ConsoleButton variant="ghost" @click="copy(detail.model_name)">{{
          t('copy')
        }}</ConsoleButton>
      </div>
      <ModelPriceItems
        :pricing="detail"
        :unit="unit"
        class="public-pricing-card"
      />
      <dl class="public-pricing-endpoints">
        <div
          v-for="endpoint in detail.supported_endpoint_types"
          :key="endpoint"
        >
          <dt>{{ endpoint }}</dt>
          <dd>
            {{ catalog?.endpoints[endpoint]?.method }}
            {{ catalog?.endpoints[endpoint]?.path }}
          </dd>
        </div>
      </dl></template
    >
    <template v-else-if="!detailId"
      ><div class="public-pricing-filters">
        <SearchInput
          v-model="search"
          :placeholder="t('search')"
          :aria-label="t('search')"
          class="public-pricing-search"
        />
        <FilterSelect
          v-model="vendor"
          :options="vendorOptions"
          :label="t('provider')"
          :prefix-label="t('provider')"
          :placeholder="t('filterAll')"
        />
        <FilterSelect
          v-model="mode"
          :options="modeOptions"
          :label="t('billingMode')"
          :prefix-label="t('billingMode')"
          :placeholder="t('filterAll')"
        />
        <FilterSelect
          v-model="sortModel"
          :options="sortOptions"
          :label="t('reference.sort')"
          :prefix-label="t('reference.sort')"
        />
        <FilterSelect
          v-model="unitModel"
          :options="unitOptions"
          :label="t('unit')"
          :prefix-label="t('unit')"
        />
      </div>
      <div class="public-pricing-grid">
        <article
          v-for="model in visible"
          :key="model.model_name"
          class="public-pricing-card"
        >
          <RouterLink
            :to="{
              name: 'pricing-model',
              params: { modelId: model.model_name },
              query: { tokenUnit: unit },
            }"
            ><h2>{{ model.model_name }}</h2></RouterLink
          >
          <p>{{ model.description }}</p>
          <ModelPriceItems :pricing="model" :unit="unit" />
        </article>
      </div>
      <TablePagination
        :page="page"
        :page-size="pageSize"
        :total="models.length"
        @update:page="page = $event"
        @update:page-size="pageSize = $event"
    /></template>
    <EmptyState
      v-if="!loading && !error && (detailId ? !detail : !models.length)"
      :title="t('emptyPublished')"
      :hint="t('noDefault')"
    />
  </PublicPage>
</template>
<style scoped>
/* Search spans the row on narrow screens; the three filters share one line
   from 640px and sit beside the search box from 1024px. */
.public-pricing-filters {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
}
@media (min-width: 640px) {
  .public-pricing-filters {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .public-pricing-search {
    grid-column: 1 / -1;
  }
}
@media (min-width: 1024px) {
  .public-pricing-filters {
    grid-template-columns: minmax(0, 1.5fr) repeat(3, minmax(0, 1fr));
  }
  .public-pricing-search {
    grid-column: auto;
  }
}
.public-pricing-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
  gap: 1rem;
}
.public-pricing-card {
  min-width: 0;
  padding: 1.25rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  display: grid;
  /* Cards in a row share its height; keep short price lists at the top. */
  align-content: start;
  gap: 0.75rem;
}
h2 {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.public-pricing-card > p {
  font-size: 0.85rem;
  color: var(--text-secondary);
}
.public-pricing-detail {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1.25rem;
}
.public-pricing-endpoints {
  margin-top: 1rem;
  display: grid;
  gap: 0.5rem;
  overflow-wrap: anywhere;
}
</style>
