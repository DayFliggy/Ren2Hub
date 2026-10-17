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
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import ModelCard from '@/components/console/models/ModelCard.vue'
import ModelChannelShelves from '@/components/console/models/ModelChannelShelves.vue'
import ModelPriceMatrix from '@/components/console/models/ModelPriceMatrix.vue'
import ModelMarketDetails from '@/components/console/models/ModelMarketDetails.vue'
import { useModelMarket } from '@/composables/useModelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { PRICE_SORTS } from '@/types/pricingReference'
import type { MarketModelType } from '@/types/modelMarket'

const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const {
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
} = useModelMarket()
const groupOptions = computed(() => [
  { value: '', label: t('allGroups') },
  ...(catalog.value?.groups ?? []).map((item) => ({
    value: item.name,
    label: `${item.name} · ${item.description}`,
  })),
])
const vendorOptions = computed(() => [
  { value: '', label: t('allVendors') },
  ...vendors.value.map((value) => ({ value, label: value })),
])
const billingOptions = computed(() => [
  { value: '', label: t('allBilling') },
  ...(['token', 'per_call', 'dynamic', 'unconfigured'] as const).map(
    (value) => ({ value, label: t(`billingModes.${value}`) })
  ),
])
const sortOptions = computed(() =>
  PRICE_SORTS.map((value) => ({ value, label: t(`sorts.${value}`) }))
)
const modelTypes: MarketModelType[] = [
  'chat',
  'image',
  'embedding',
  'audio',
  'video',
  'rerank',
  'other',
]
const viewOptions = computed(() => [
  {
    value: 'models',
    label: t('views.models'),
    icon: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
  },
  {
    value: 'channels',
    label: t('views.channels'),
    icon: 'M3 4h18v6H3zM3 14h18v6H3zM8 4v6M8 14v6',
  },
  {
    value: 'comparison',
    label: t('views.comparison'),
    icon: 'M3 4h18v16H3zM3 9h18M9 4v16M15 9v11',
  },
])
const detailId = ref<string | null>(null)
const detailModel = computed(
  () =>
    catalog.value?.models.find((model) => model.id === detailId.value) ?? null
)
watch(catalog, () => {
  if (!detailModel.value) detailId.value = null
})
onMounted(load)
</script>

<template>
  <div class="model-market" data-model-market>
    <PageBreadcrumb :crumbs="[t('console'), t('title')]">
      <template #action
        ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
          loading ? t('refreshing') : t('refresh')
        }}</ConsoleButton></template
      >
    </PageBreadcrumb>
    <ConsoleCard class="market-filter-card" :padded="false">
      <div class="market-toolbar">
        <SearchInput
          v-model="keyword"
          class="market-search"
          :placeholder="t('search')"
          :aria-label="t('search')"
        />
        <SegmentedToggle
          v-model="view"
          class="market-view-switch"
          :options="viewOptions"
          :label="t('viewLabel')"
        />
      </div>
      <div class="market-filters">
        <FilterSelect
          v-model="group"
          :options="groupOptions"
          :label="t('group')"
        />
        <FilterSelect
          v-model="vendor"
          :options="vendorOptions"
          :label="t('vendor')"
        />
        <FilterSelect
          v-model="billing"
          :options="billingOptions"
          :label="t('billing')"
        />
        <FilterSelect
          v-model="sort"
          :options="sortOptions"
          :label="t('sort')"
        />
      </div>
      <div class="market-type-tabs" role="group" :aria-label="t('typeLabel')">
        <button
          class="market-type-tab focus-ring"
          :class="{ active: type === '' }"
          type="button"
          :aria-pressed="type === ''"
          @click="type = ''"
        >
          {{ t('allTypes') }}
        </button>
        <button
          v-for="item in modelTypes"
          :key="item"
          class="market-type-tab focus-ring"
          :class="{ active: type === item }"
          type="button"
          :aria-pressed="type === item"
          @click="type = item"
        >
          {{ t(`types.${item}`) }}
        </button>
      </div>
    </ConsoleCard>
    <div class="market-result-meta">
      <p>
        {{ t('resultCount', { count: filtered.length }) }} ·
        {{ t('groupCount', { count: visibleGroups.length }) }}
      </p>
      <button class="focus-ring" type="button" @click="resetFilters">
        {{ t('resetFilters') }}
      </button>
    </div>
    <details v-if="catalog?.missingPrices.length" class="market-pricing-issues">
      <summary>
        {{ t('missingPrices', { count: catalog.missingPrices.length }) }}
      </summary>
      <p>{{ t('missingPricesHint') }}</p>
      <ul>
        <li v-for="name in catalog.missingPrices" :key="name">{{ name }}</li>
      </ul>
    </details>
    <div v-if="loading" class="market-model-grid" aria-busy="true">
      <SkeletonBlock v-for="n in 6" :key="n" class="h-72" />
    </div>
    <ConsoleCard v-else-if="error"
      ><div class="market-load-error" role="alert">
        <h2>{{ t('loadError') }}</h2>
        <p>{{ error }}</p>
        <ConsoleButton @click="load">{{ t('retry') }}</ConsoleButton>
      </div></ConsoleCard
    >
    <ModelChannelShelves v-else-if="view === 'channels'" />
    <EmptyState
      v-else-if="!catalog?.groups.length"
      :title="t('noCatalogTitle')"
      :hint="t('noGroups')"
    />
    <EmptyState
      v-else-if="!catalog.models.length"
      :title="t('noCatalogTitle')"
      :hint="t('noCatalogHint')"
    />
    <EmptyState
      v-else-if="!filtered.length"
      :title="t('emptyTitle')"
      :hint="t('emptyHint')"
    />
    <div v-else-if="view === 'models'" class="market-model-grid">
      <ModelCard
        v-for="model in filtered"
        :key="model.id"
        :model="model"
        :group="selectedGroup"
        @detail="detailId = $event"
      />
    </div>
    <ModelPriceMatrix
      v-else
      :models="filtered"
      :groups="visibleGroups"
      @detail="detailId = $event"
    />
    <footer class="market-page-note">
      <p>{{ t('priceNote') }}</p>
      <p>{{ t('sortNote') }}</p>
      <p>{{ t('requestNote') }}</p>
    </footer>
    <ModelMarketDetails
      v-if="detailModel"
      :model="detailModel"
      :initial-group="group"
      @close="detailId = null"
    />
  </div>
</template>

<style scoped>
.model-market {
  container-type: inline-size;
  container-name: model-market;
}
.market-filter-card {
  position: relative;
  z-index: 5;
}
.market-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem;
}
.market-search {
  flex: 1 1 16rem;
  min-width: 10rem;
}
.market-view-switch {
  flex: none;
  margin-left: auto;
}
.market-filters {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.75rem;
  padding: 0 1rem 1rem;
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
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.35rem 1rem;
  margin: 1rem 0;
  color: var(--text-tertiary);
  font-size: 0.75rem;
}
.market-result-meta button {
  min-height: 2rem;
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
.market-pricing-issues {
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.market-pricing-issues summary {
  cursor: pointer;
}
.market-pricing-issues ul {
  margin-top: 0.5rem;
  padding-left: 1rem;
}
.market-load-error {
  display: grid;
  justify-items: start;
  gap: 0.8rem;
}
.market-load-error h2 {
  font-size: 1rem;
  font-weight: 600;
}
.market-load-error p {
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
@container model-market (max-width: 1100px) {
  .market-model-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@container model-market (max-width: 720px) {
  .market-model-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .market-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .market-view-switch {
    width: 100%;
    margin-left: 0;
  }
  .market-type-tabs {
    gap: 0 1rem;
  }
}
@container model-market (max-width: 520px) {
  .market-filters {
    grid-template-columns: minmax(0, 1fr);
  }
  .market-view-switch {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .market-view-switch :deep([role='radio']) {
    min-width: 0;
    height: auto;
    min-height: 3.25rem;
    padding: 0.375rem;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.75rem;
    line-height: 1.25;
  }
  .market-view-switch :deep(svg) {
    flex-shrink: 0;
  }
}
</style>
