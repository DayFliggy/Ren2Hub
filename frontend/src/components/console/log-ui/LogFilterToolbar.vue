<script setup lang="ts">
import { computed, ref } from 'vue'
import { RotateCcw, SlidersHorizontal, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import LogUnifiedSearch, { type LogSearchField } from './LogUnifiedSearch.vue'

export interface ActiveFilterChip {
  key: string
  label: string
  value: string
}

const props = withDefaults(
  defineProps<{
    searchFields: LogSearchField[]
    searchValues: Record<string, string>
    searchField?: string
    searchInputName?: string
    /** Number of active non-search secondary filters (dropdowns / dates). */
    secondaryActiveCount?: number
    /** Extra active filter chips (e.g. non-search filters or combined search fields). */
    activeChips?: ActiveFilterChip[]
    /** Whether a global reset button should be offered. */
    hasActiveFilters?: boolean
    /** Whether secondary filter controls exist in the #filters slot. */
    hasSecondaryFilters?: boolean
  }>(),
  {
    searchField: '',
    searchInputName: 'log-unified-search',
    secondaryActiveCount: 0,
    activeChips: () => [],
    hasActiveFilters: false,
    hasSecondaryFilters: true,
  }
)

const emit = defineEmits<{
  'update:searchValue': [key: string, value: string]
  'update:searchField': [key: string]
  'clear-chip': [key: string]
  reset: []
}>()

const { t } = useI18n()
const mobileFiltersOpen = ref(false)

const searchChips = computed<ActiveFilterChip[]>(() =>
  props.searchFields.flatMap((field) => {
    const val = (props.searchValues[field.key] ?? '').trim()
    return val ? [{ key: field.key, label: field.label, value: val }] : []
  })
)

const allChips = computed<ActiveFilterChip[]>(() => [
  ...searchChips.value,
  ...props.activeChips,
])

const showChipRow = computed(
  () => allChips.value.length > 0 || props.hasActiveFilters
)
</script>

<template>
  <div
    data-log-toolbar
    class="mb-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3 sm:p-3.5"
  >
    <div class="flex flex-wrap items-center gap-2.5">
      <!-- Primary row: single unified search box + mobile filter toggle -->
      <div
        class="flex min-w-0 flex-1 basis-full items-center gap-2 lg:min-w-[216px] lg:basis-56"
      >
        <LogUnifiedSearch
          :fields="searchFields"
          :values="searchValues"
          :model-field="searchField"
          :input-name="searchInputName"
          class="min-w-0 flex-1"
          @update:value="(key, val) => emit('update:searchValue', key, val)"
          @update:model-field="(key) => emit('update:searchField', key)"
        />
        <ConsoleButton
          v-if="hasSecondaryFilters"
          type="button"
          variant="secondary"
          class="shrink-0 lg:hidden"
          :aria-expanded="mobileFiltersOpen"
          data-log-mobile-filters-toggle
          @click="mobileFiltersOpen = !mobileFiltersOpen"
        >
          <SlidersHorizontal :size="15" aria-hidden="true" />
          <span>{{ t('logs.moreFilters') }}</span>
          <span
            v-if="secondaryActiveCount > 0"
            class="inline-flex min-w-5 items-center justify-center rounded-full bg-[var(--accent-soft)] px-1.5 py-0.5 font-mono text-[0.6875rem] font-semibold leading-none text-[var(--accent-text)]"
          >
            {{ secondaryActiveCount }}
          </span>
        </ConsoleButton>
      </div>

      <!-- Secondary filters: inline on desktop, collapsible on mobile -->
      <div
        v-if="hasSecondaryFilters"
        class="w-full flex-wrap items-center gap-2.5 lg:flex lg:w-auto"
        :class="mobileFiltersOpen ? 'flex pt-1 lg:pt-0' : 'hidden lg:flex'"
        data-log-secondary-filters
      >
        <slot name="filters" />
      </div>

      <!-- Right-aligned actions (column settings, export, etc.) -->
      <div
        v-if="$slots.actions"
        class="ml-auto flex w-full items-center justify-end gap-2 sm:w-auto"
      >
        <slot name="actions" />
      </div>
    </div>

    <!-- Active filter chips + one-click reset -->
    <div
      v-if="showChipRow"
      data-log-active-filters
      class="mt-2.5 flex flex-wrap items-center gap-1.5 border-t border-[var(--border-subtle)] pt-2.5"
    >
      <span
        v-for="chip in allChips"
        :key="chip.key"
        data-log-filter-chip
        class="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] px-2.5 py-1 text-xs text-[var(--text-secondary)]"
      >
        <span class="shrink-0 text-[var(--text-tertiary)]"
          >{{ chip.label }}:</span
        >
        <span class="truncate font-mono text-[var(--text-primary)]">{{
          chip.value
        }}</span>
        <button
          type="button"
          :aria-label="t('logs.clearFilterValue', { label: chip.label })"
          class="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
          @click="emit('clear-chip', chip.key)"
        >
          <X :size="12" aria-hidden="true" />
        </button>
      </span>
      <button
        type="button"
        data-log-reset-filters
        class="ml-auto inline-flex min-h-7 items-center gap-1 rounded-lg px-2 text-xs font-medium text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
        @click="emit('reset')"
      >
        <RotateCcw :size="12" aria-hidden="true" />
        {{ t('logs.resetFilters') }}
      </button>
    </div>
  </div>
</template>
