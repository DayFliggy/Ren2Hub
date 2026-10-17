<script setup lang="ts">
import { quotaForChart } from '@/utils/quota'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import MiniSparkline from '@/components/console/dashboard/MiniSparkline.vue'
import { kpiDividerClasses } from '@/components/console/dashboard/kpiDividers'
import type { FlowPoint } from '@/composables/useDashboard'
import type { StatsKpi } from '@/composables/useDashboardStats'
import { formatQuota, formatNumber } from '@/utils/format'

const props = defineProps<{
  kpi: StatsKpi | null
  /** Daily flow of the selected window, feeding the spend/request sparklines. */
  flow?: FlowPoint[]
  loading?: boolean
}>()

const { t } = useI18n()

interface Cell {
  key: string
  label: string
  /** lucide 24×24 path data */
  icon: string
  value: string
  /** small unit rendered after the figure */
  suffix?: string
  color: string
  /** widens the loading skeleton to roughly match the real value */
  skeleton: string
  /** window trend; the success cell uses a ring instead */
  series?: number[]
}

const cells = computed<Cell[]>(() => [
  {
    key: 'tokens',
    label: t('dashboard.stats.totalTokens'),
    icon: 'M12 2 2 7l10 5 10-5-10-5ZM2 17l10 5 10-5M2 12l10 5 10-5',
    value: props.kpi ? formatNumber(props.kpi.totalTokens) : '—',
    color: 'var(--accent-text)',
    skeleton: 'w-28',
  },
  {
    key: 'spend',
    label: t('dashboard.stats.totalSpend'),
    icon: 'M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6',
    value: props.kpi ? formatQuota(props.kpi.totalQuota) : '—',
    color: 'var(--accent-text)',
    skeleton: 'w-24',
    series: (props.flow ?? []).map((f) => quotaForChart(f.consume)),
  },
  {
    key: 'requests',
    label: t('dashboard.stats.totalRequests'),
    icon: 'M4 17h4l3-9 3 14 3-11 2 6h3',
    value: props.kpi ? formatNumber(props.kpi.totalRequests) : '—',
    color: 'var(--signal)',
    skeleton: 'w-20',
    series: (props.flow ?? []).map((f) => f.requests),
  },
])
</script>

<template>
  <!--
    Same divided strip as the Overview tab, so the two tabs read as one
    system: hairline separators rather than card gaps, every cell label /
    figure / mini visual. Spend and requests chart the selected window; the
    success rate reads out as a ring; cells with no series keep the visual
    slot's height so the strip stays level.
  -->
  <section
    class="pencil-surface overflow-hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
    data-handdrawn="surface-clipped"
    data-stats-kpi
  >
    <div class="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-3">
      <div
        v-for="(cell, i) in cells"
        :key="cell.key"
        class="flex min-w-0 flex-col px-5 py-4"
        :class="[
          ...kpiDividerClasses(i),
          i === cells.length - 1 ? 'col-span-2 sm:col-span-1' : '',
        ]"
      >
        <div
          :class="
            i === cells.length - 1
              ? 'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 sm:block'
              : ''
          "
        >
          <div class="min-w-0">
            <p
              class="flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]"
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path :d="cell.icon" />
              </svg>
              <span class="truncate">{{ cell.label }}</span>
            </p>

            <p
              v-if="loading"
              class="mt-1.5 h-7 animate-pulse rounded-sm bg-[var(--surface-muted)]"
              :class="cell.skeleton"
            />
            <p
              v-else
              class="display-number mt-1 truncate text-2xl leading-tight tabular-nums tracking-tight"
              :style="{ color: cell.color }"
            >
              {{ cell.value
              }}<span
                v-if="cell.suffix"
                class="text-xs font-normal text-[var(--text-tertiary)]"
              >
                {{ cell.suffix }}</span
              >
            </p>
          </div>

          <div
            v-if="cell.key === 'success'"
            class="mt-2 flex items-center"
          ></div>
          <div v-else class="mt-2">
            <MiniSparkline
              v-if="!loading && cell.series && cell.series.length > 1"
              :points="cell.series"
              :color="cell.color"
              :height="30"
            />
            <div v-else class="h-[1.875rem]" />
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
