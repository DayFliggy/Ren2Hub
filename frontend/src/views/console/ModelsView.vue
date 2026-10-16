<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SegmentedToggle, {
  type SegmentOption,
} from '@/components/common/SegmentedToggle.vue'
import ModelCard from '@/components/console/models/ModelCard.vue'
import ModelChannelShelves from '@/components/console/models/ModelChannelShelves.vue'
import ModelPriceMatrix from '@/components/console/models/ModelPriceMatrix.vue'
import ModelMarketDetails from '@/components/console/models/ModelMarketDetails.vue'
import { useModelMarket } from '@/composables/useModelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import type { MarketModelType } from '@/types/modelMarket'

const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const {
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
  load,
  resetFilters,
} = useModelMarket()
const channelValue = computed({
  get: () => (channel.value === '' ? '' : String(channel.value)),
  set: (value: string) => {
    channel.value = value === '' ? '' : Number(value)
  },
})
const channelOptions = computed(() => [
  { value: '', label: t('allChannels') },
  ...(catalog.value?.channels ?? []).map((item) => ({
    value: String(item.id),
    label: item.name,
  })),
])
const billingOptions = computed(() => [
  { value: '', label: t('allBilling') },
  ...(['token', 'per_call', 'dynamic', 'unconfigured'] as const).map(
    (value) => ({ value, label: t(`billingModes.${value}`) })
  ),
])
const sortOptions = computed(() =>
  (['default', 'name', 'input', 'call'] as const).map((value) => ({
    value,
    label: t(`sorts.${value}`),
  }))
)
const modelTypes: MarketModelType[] = [
  'chat',
  'image',
  'embedding',
  'audio',
  'video',
  'rerank',
]
const viewPaths = {
  models:
    'M5 7.5h9.5a2 2 0 0 1 2 2V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9.5a2 2 0 0 1 2-2ZM7.5 7.5V5a2 2 0 0 1 2-2H19a2 2 0 0 1 2 2v9.5a2 2 0 0 1-2 2h-2.5M6.5 13h6.5M6.5 16.5h4.5',
  channels:
    'M4.5 4h15A1.5 1.5 0 0 1 21 5.5V9a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 9V5.5A1.5 1.5 0 0 1 4.5 4ZM8.5 4v6.5M12 7.25h5.5M4.5 13.5h15A1.5 1.5 0 0 1 21 15v3.5a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18.5V15a1.5 1.5 0 0 1 1.5-1.5ZM8.5 13.5V20M12 16.75h5.5',
  comparison:
    'M5 4h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2ZM3 9h18M9 4v16M15 9v11M9 14.5h12M18 16v2.5',
}
const viewOptions = computed<SegmentOption[]>(() =>
  (['models', 'channels', 'comparison'] as const).map((value) => ({
    value,
    ariaLabel: t(`views.${value}`),
    icon: viewPaths[value],
  }))
)
const hasCatalogSupply = computed(() =>
  (catalog.value?.models ?? []).some((model) => model.offers.length > 0)
)
const detailId = ref<string | null>(null)
const detailOfferId = ref<string>()
const detailModel = computed(
  () =>
    catalog.value?.models.find((model) => model.id === detailId.value) ?? null
)
watch(catalog, (value) => {
  if (
    detailId.value &&
    !value?.models.some((model) => model.id === detailId.value)
  )
    detailId.value = null
})
function openDetails(modelId: string, offerId?: string) {
  detailOfferId.value = offerId
  detailId.value = modelId
}
onMounted(load)
</script>

<template>
  <div class="model-market" data-model-market>
    <PageBreadcrumb :crumbs="[t('console'), t('title')]">
      <template #action>
        <span class="market-view-label"
          ><svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path :d="viewPaths[view]" /></svg
          >{{ t(`views.${view}`) }}</span
        >
      </template>
    </PageBreadcrumb>

    <ConsoleCard :padded="false" class="market-filter-card">
      <div class="market-toolbar">
        <SearchInput
          v-model="keyword"
          :placeholder="t('search')"
          :aria-label="t('search')"
          name="model-search"
          class="market-search"
        />
        <FilterSelect
          v-model="channelValue"
          :options="channelOptions"
          :label="t('channel')"
          class="market-channel-filter"
        />
        <FilterSelect
          v-model="billing"
          :options="billingOptions"
          :label="t('billing')"
          class="market-billing-filter"
        />
        <FilterSelect
          v-model="sort"
          :options="sortOptions"
          :label="t('sort')"
          class="market-sort-filter"
        />
        <SegmentedToggle
          v-model="view"
          :options="viewOptions"
          :label="t('viewLabel')"
          size="sm"
          class="market-view-switch"
        />
      </div>
      <div class="market-type-tabs" role="group" :aria-label="t('typeLabel')">
        <button
          type="button"
          class="market-type-tab focus-ring"
          :class="{ active: type === '' }"
          :aria-pressed="type === ''"
          @click="type = ''"
        >
          {{ t('allTypes') }}
        </button>
        <button
          v-for="modelType in modelTypes"
          :key="modelType"
          type="button"
          class="market-type-tab focus-ring"
          :class="{ active: type === modelType }"
          :aria-pressed="type === modelType"
          @click="type = modelType"
        >
          {{ t(`types.${modelType}`) }}
        </button>
      </div>
    </ConsoleCard>

    <div class="market-result-meta" aria-live="polite">
      <p>{{ t('resultCount', { models: resultCount, offers: offerCount }) }}</p>
      <span>{{ t('quotaNotice') }}</span>
    </div>
    <details
      v-if="pricingIssues.length"
      class="market-pricing-issues"
      data-pricing-issues
    >
      <summary class="focus-ring">
        {{ t('partialPrices', { count: pricingIssues.length }) }}
      </summary>
      <ul>
        <li v-for="name in pricingIssues" :key="name">
          {{ name }} · {{ t('unconfigured') }}
        </li>
      </ul>
    </details>

    <div v-if="loading" class="market-model-grid" aria-busy="true">
      <SkeletonBlock v-for="index in 6" :key="index" bordered class="h-72" />
    </div>
    <ConsoleCard v-else-if="error">
      <div class="market-load-error" role="alert">
        <h2>{{ t('loadError') }}</h2>
        <p>{{ error || t('loadErrorHint') }}</p>
        <ConsoleButton variant="secondary" @click="load">{{
          t('retry')
        }}</ConsoleButton>
      </div>
    </ConsoleCard>
    <ConsoleCard v-else-if="!hasResults"
      ><EmptyState
        :title="t(hasCatalogSupply ? 'emptyTitle' : 'noCatalogTitle')"
        :hint="t(hasCatalogSupply ? 'emptyHint' : 'noCatalogHint')"
        illustration="empty-models"
      />
      <div v-if="hasCatalogSupply" class="market-reset">
        <ConsoleButton variant="secondary" @click="resetFilters">{{
          t('resetFilters')
        }}</ConsoleButton>
      </div></ConsoleCard
    >
    <div
      v-else-if="view === 'models'"
      class="market-model-grid"
      data-market-models
    >
      <ModelCard
        v-for="model in filtered"
        :key="model.id"
        :model="model"
        @detail="openDetails"
      />
    </div>
    <ModelChannelShelves
      v-else-if="view === 'channels'"
      :shelves="shelves"
      @detail="openDetails"
    />
    <ModelPriceMatrix
      v-else
      :models="filtered"
      :channels="visibleChannels"
      @detail="openDetails"
    />

    <footer v-if="!loading && !error && hasResults" class="market-page-note">
      <p>{{ t(view === 'comparison' ? 'comparisonNote' : 'catalogNote') }}</p>
      <p>{{ t('supplyNote') }}</p>
    </footer>
    <ModelMarketDetails
      :model="detailModel"
      :offer-id="detailOfferId"
      @close="detailId = null"
    />
  </div>
</template>

<style scoped>
.model-market {
  container: model-market / inline-size;
  min-width: 0;
}
.market-view-label {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.25rem 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  background: var(--surface-muted);
  font-size: 0.6875rem;
}
.market-view-label svg {
  color: var(--accent-text);
}
.market-filter-card {
  position: relative;
  z-index: 5;
}
.market-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem 1rem 0.75rem;
}
.market-search {
  flex: 1 1 16rem;
  min-width: 10rem;
  max-width: 22rem;
}
.market-channel-filter {
  flex: 0 1 10rem;
  min-width: 7.5rem;
}
.market-billing-filter {
  flex: 0 1 9rem;
  min-width: 7rem;
}
.market-sort-filter {
  flex: 0 1 12rem;
  min-width: 9rem;
}
.market-view-switch {
  flex: none;
  margin-left: auto;
}
.market-type-tabs {
  display: flex;
  gap: 1.1rem;
  flex-wrap: wrap;
  padding: 0.35rem 1.1rem 0;
  border-top: 1px dashed var(--border-subtle);
}
.market-type-tab {
  position: relative;
  padding: 0.6rem 0 0.7rem;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  min-height: 2.75rem;
}
.market-type-tab:hover {
  color: var(--text-primary);
}
.market-type-tab.active {
  color: var(--accent-text);
  font-weight: 600;
}
.market-type-tab.active::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--accent);
  border-radius: 50%;
}
.market-result-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.35rem 1rem;
  margin: 1.2rem 0 0.85rem;
  color: var(--text-tertiary);
  font-size: 0.75rem;
}
.market-result-meta > span {
  font-size: 0.6875rem;
}
.market-model-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1rem;
}
.market-page-note {
  margin-top: 1.2rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.9;
}
.market-page-note p + p {
  margin-top: 0.2rem;
}
.market-pricing-issues {
  border: 1px solid var(--border-default);
  border-radius: var(--shape-control);
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-pricing-issues summary {
  cursor: pointer;
}
.market-pricing-issues ul {
  margin-top: 0.5rem;
  padding-left: 1rem;
  overflow-wrap: anywhere;
}
.market-load-error {
  display: grid;
  justify-items: start;
  gap: 0.8rem;
  padding: 1rem;
}
.market-load-error h2 {
  color: var(--text-primary);
  font-size: 1rem;
  font-weight: 600;
}
.market-load-error p {
  color: var(--text-secondary);
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.market-reset {
  display: flex;
  justify-content: center;
  padding-bottom: 1.5rem;
}
@container model-market (max-width: 1100px) {
  .market-model-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .market-toolbar {
    flex-wrap: wrap;
  }
  .market-search {
    flex-basis: calc(100% - 12rem);
    max-width: none;
  }
  .market-view-switch {
    order: 1;
  }
  .market-channel-filter {
    order: 2;
    flex: 1;
  }
  .market-billing-filter {
    order: 3;
    flex: 1;
  }
  .market-sort-filter {
    order: 4;
    flex: 1.5;
  }
}
@container model-market (max-width: 720px) {
  .market-model-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.85rem;
  }
  .market-toolbar {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 0.7rem;
    padding: 0.9rem 0.9rem 0.75rem;
  }
  .market-search {
    grid-column: 1 / -1;
    min-width: 0;
    width: 100%;
    order: 0;
  }
  .market-channel-filter {
    order: 1;
    min-width: 0;
  }
  .market-billing-filter {
    order: 2;
    min-width: 0;
  }
  .market-sort-filter {
    order: 3;
    min-width: 0;
  }
  .market-view-switch {
    order: 4;
    min-width: 0;
    justify-self: end;
    margin-left: 0;
  }
  .market-type-tabs {
    gap: 0 1rem;
    padding-inline: 0.9rem;
  }
  .market-result-meta {
    margin-top: 1rem;
  }
  .market-result-meta > span {
    font-size: 0.625rem;
  }
}
</style>
