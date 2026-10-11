<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useClipboard } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import type { MarketModel } from '@/types/console'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import HealthMeter from '@/components/common/HealthMeter.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle, {
  type SegmentOption,
} from '@/components/common/SegmentedToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import VendorSection from '@/components/console/models/VendorSection.vue'
import { useModelMarket } from '@/composables/useModelMarket'
import { useToast } from '@/composables/useToast'
import { formatContext, formatLatency } from '@/utils/format'
import ModelPriceItems from '@/components/console/models/ModelPriceItems.vue'

const { t } = useI18n()
const toast = useToast()
const { copy } = useClipboard()

const {
  loading,
  keyword,
  vendor,
  type,
  sort,
  view,
  groups,
  resultCount,
  hasResults,
  pricingIssues,
  vendorOptions,
  vendorLabel,
  load,
} = useModelMarket()

const detailModel = ref<MarketModel | null>(null)

const typeOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'chat', label: t('models.type.chat') },
  { value: 'image', label: t('models.type.image') },
  { value: 'embedding', label: t('models.type.embedding') },
  { value: 'audio', label: t('models.type.audio') },
  { value: 'video', label: t('models.type.video') },
  { value: 'rerank', label: t('models.type.rerank') },
])

const vendorSelectOptions = computed(() => [
  { value: '', label: t('models.allVendors') },
  ...vendorOptions.value.map((v) => ({ value: v, label: vendorLabel(v) })),
])

const sortOptions = computed(() => [
  { value: 'default', label: t('models.sortDefault') },
  { value: 'health', label: t('models.sortHealth') },
  { value: 'latency', label: t('models.sortLatency') },
  { value: 'tps', label: t('models.sortTps') },
  { value: 'price', label: t('models.sortPrice') },
])

const viewOptions = computed<SegmentOption[]>(() => [
  {
    value: 'grid',
    ariaLabel: t('models.viewGrid'),
    icon: 'M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z',
  },
  {
    value: 'list',
    ariaLabel: t('models.viewList'),
    icon: 'M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01',
  },
])

const detailTypeLabel = computed(() =>
  detailModel.value ? t(`models.type.${detailModel.value.type}`) : ''
)
const detailBillingLabel = computed(() =>
  detailModel.value ? t(`models.billing.${detailModel.value.billing}`) : ''
)

async function copyName(name: string) {
  await copy(name)
  toast.success(t('models.copied', { name }))
}

onMounted(load)
</script>

<template>
  <div>
    <PageBreadcrumb
      :crumbs="[t('models.breadcrumb.0'), t('models.breadcrumb.1')]"
    />

    <details
      v-if="pricingIssues.length"
      class="mb-4 rounded-lg border border-[var(--border-subtle)] p-3 text-sm"
      data-pricing-issues
    >
      <summary class="cursor-pointer focus-ring">
        {{ t('models.partialPricing', { count: pricingIssues.length }) }}
      </summary>
      <ul class="mt-2 space-y-1">
        <li
          v-for="(issue, index) in pricingIssues"
          :key="index"
          class="break-all"
        >
          {{ issue.model_name || t('models.unknownModel') }} ·
          {{ t(`models.pricingIssues.${issue.reason}`) }}
        </li>
      </ul>
    </details>

    <!-- filter toolbar -->
    <ConsoleCard :padded="false">
      <div class="flex flex-wrap items-center gap-3 p-4">
        <SearchInput
          v-model="keyword"
          :placeholder="t('models.searchPlaceholder')"
          :aria-label="t('models.searchPlaceholder')"
          name="model-search"
          class="w-full sm:w-64"
        />
        <div class="flex flex-1 flex-wrap items-center gap-3">
          <FilterSelect
            v-model="vendor"
            :options="vendorSelectOptions"
            :label="t('models.vendorFilter')"
            class="min-w-[130px] flex-1 sm:flex-none sm:w-36"
          />
          <FilterSelect
            v-model="type"
            :options="typeOptions"
            :label="t('models.typeFilter')"
            class="min-w-[110px] flex-1 sm:flex-none sm:w-32"
          />
          <FilterSelect
            v-model="sort"
            :options="sortOptions"
            :label="t('models.sortFilter')"
            class="min-w-[130px] flex-1 sm:flex-none sm:w-36"
          />
        </div>
        <!-- view toggle -->
        <SegmentedToggle
          v-model="view"
          :options="viewOptions"
          :label="t('models.viewToggle', '视图切换')"
          size="sm"
          class="shrink-0"
        />
      </div>
    </ConsoleCard>

    <!-- result meta -->
    <p class="mb-4 mt-5 text-sm text-[var(--text-tertiary)]">
      {{ t('models.resultCount', { count: resultCount }) }}
    </p>

    <!-- loading skeleton -->
    <div v-if="loading" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <SkeletonBlock v-for="i in 6" :key="i" bordered class="h-56" />
    </div>

    <!-- empty -->
    <ConsoleCard v-else-if="!hasResults">
      <EmptyState
        :title="t('models.emptyTitle')"
        :hint="t('models.emptyHint')"
        illustration="empty-models"
      />
    </ConsoleCard>

    <!-- vendor groups -->
    <template v-else>
      <VendorSection
        v-for="g in groups"
        :key="g.vendor"
        :group="g"
        :view="view"
        @detail="detailModel = $event"
      />
    </template>

    <!-- detail modal -->
    <ConsoleModal
      :open="detailModel != null"
      :title="detailModel?.name ?? ''"
      size="lg"
      @close="detailModel = null"
    >
      <div v-if="detailModel" class="space-y-5">
        <!-- header meta -->
        <div class="flex items-center gap-3">
          <VendorLogo :vendor="vendorLabel(detailModel.vendor)" :size="44" />
          <div class="flex-1">
            <div class="flex items-center gap-2">
              <StatusChip tone="info">{{ detailTypeLabel }}</StatusChip>
              <StatusChip tone="accent">{{ detailBillingLabel }}</StatusChip>
            </div>
            <p class="mt-1 text-sm text-[var(--text-tertiary)]">
              {{ vendorLabel(detailModel.vendor) }}
            </p>
          </div>
          <ConsoleButton
            variant="secondary"
            size="sm"
            @click="copyName(detailModel.name)"
          >
            {{ t('common.copy') }}
          </ConsoleButton>
        </div>

        <p class="text-sm leading-relaxed text-[var(--text-secondary)]">
          {{ detailModel.tagline }}
        </p>

        <!-- metric grid -->
        <div class="grid grid-cols-3 gap-3">
          <div class="rounded-xl bg-[var(--surface-muted)] p-3 text-center">
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('models.latency') }}
            </p>
            <p class="mt-1 text-lg font-bold text-[var(--text-primary)]">
              {{ formatLatency(detailModel.latency) }}
            </p>
          </div>
          <div class="rounded-xl bg-[var(--surface-muted)] p-3 text-center">
            <p class="text-xs text-[var(--text-tertiary)]">TPS</p>
            <p class="mt-1 text-lg font-bold text-[var(--text-primary)]">
              {{ detailModel.tps > 0 ? detailModel.tps.toFixed(1) : '—' }}
            </p>
          </div>
          <div class="rounded-xl bg-[var(--surface-muted)] p-3 text-center">
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('models.contextWindow') }}
            </p>
            <p class="mt-1 text-lg font-bold text-[var(--text-primary)]">
              {{ formatContext(detailModel.context) }}
            </p>
          </div>
        </div>

        <!-- health -->
        <div
          class="flex items-center justify-between rounded-xl bg-[var(--surface-muted)] px-4 py-3"
        >
          <span class="text-sm text-[var(--text-secondary)]">{{
            t('models.status')
          }}</span>
          <div class="flex items-center gap-3">
            <span class="text-sm font-semibold text-[var(--text-primary)]"
              >{{ detailModel.health }}%</span
            >
            <HealthMeter :value="detailModel.health" :bars="16" />
          </div>
        </div>

        <!-- full price table -->
        <div>
          <h3 class="mb-2 text-sm font-semibold text-[var(--text-primary)]">
            {{ t('models.priceTitle') }}
          </h3>
          <ModelPriceItems
            v-if="detailModel.pricing"
            :pricing="detailModel.pricing"
            class="rounded-xl border border-[var(--border-subtle)] p-4"
          />
          <p v-else class="text-sm text-[var(--text-tertiary)]">—</p>
        </div>
      </div>
    </ConsoleModal>
  </div>
</template>
