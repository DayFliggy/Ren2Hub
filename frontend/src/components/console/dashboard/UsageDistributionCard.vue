<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import UsageHeatmapGrid from '@/components/console/dashboard/UsageHeatmapGrid.vue'
import UsageSegmentBar from '@/components/console/dashboard/UsageSegmentBar.vue'
import type { UsageDistributionPoint } from '@/composables/useUsageDistribution'
import { quotaInteger } from '@/utils/quota'
import {
  USAGE_TOP_DAY_LIMIT,
  buildUsageDistributionView,
  formatUsageDate,
  formatUsageMetric,
  usageHeatStyle,
  usageWeekdayLabels,
  type UsageDistributionMetric,
  type UsageDistributionPeriod,
} from '@/utils/usageDistribution'

const props = withDefaults(
  defineProps<{
    points: UsageDistributionPoint[]
    loading?: boolean
    error?: string
  }>(),
  { loading: false, error: '' }
)

const { t, locale } = useI18n()
const period = ref<UsageDistributionPeriod>('month')
const metric = ref<UsageDistributionMetric>('requests')

const view = computed(() =>
  buildUsageDistributionView(
    props.points.map((point) => ({
      ...point,
      // Lossy at the chart boundary only; formatQuota applies the currency.
      consume: Number(quotaInteger(point.consume)),
    })),
    period.value,
    metric.value
  )
)

const periodOptions = computed(() => [
  { value: 'month', label: t('dashboard.distribution.periodMonth') },
  { value: 'quarter', label: t('dashboard.distribution.periodQuarter') },
  { value: 'year', label: t('dashboard.distribution.periodYear') },
])
const metricOptions = computed(() => [
  { value: 'requests', label: t('dashboard.distribution.metricRequests') },
  { value: 'consume', label: t('dashboard.distribution.metricConsume') },
  { value: 'tokens', label: t('dashboard.distribution.metricTokens') },
])

const metricColor = computed(() => `var(--heatmap-${metric.value})`)

const periodLabel = computed(() =>
  t(`dashboard.distribution.periodSummary.${period.value}`)
)

// From sm, month and quarter sit beside the analytics column, so a tablet-wide
// card has no empty half beside the fixed-width month calendar. Quarter cells
// stretch, so its heatmap track grows to the cell cap before the analytics
// column takes the rest and ends flush with the card's right edge.
const stageColumns = computed(() => {
  if (period.value === 'month') return 'sm:grid-cols-[auto_minmax(0,1fr)]'
  if (period.value === 'quarter') {
    return 'sm:grid-cols-[minmax(0,max-content)_minmax(14rem,1fr)]'
  }
  return ''
})

const analyticsClasses = computed(() => {
  if (period.value === 'year') return 'sm:gap-6 sm:pt-1'
  // Month and quarter both end flush with the card's right edge; surplus
  // width opens before the divider, never after it.
  return 'sm:flex sm:w-full sm:max-w-[22rem] sm:flex-col sm:justify-center sm:justify-self-end sm:border-l sm:border-t-0 sm:pl-4 sm:pt-0'
})
const metricLabel = computed(
  () =>
    metricOptions.value.find((option) => option.value === metric.value)!.label
)

function formatMetric(value: number): string {
  return formatUsageMetric(metric.value, value)
}

const weekdays = computed(() => usageWeekdayLabels(locale.value))
const weekdayNames = computed(() => usageWeekdayLabels(locale.value, 'short'))

const topSegments = computed(() =>
  view.value.topDays.map((day, index) => ({
    key: day.date,
    label: formatUsageDate(day.date, locale.value),
    value: day.value,
    displayValue: formatMetric(day.value),
    intensity: [90, 68, 46][index]!,
  }))
)

const weekdaySegments = computed(() => {
  const max = Math.max(...view.value.weekdays.map((entry) => entry.value), 0)
  return view.value.weekdays.map((entry) => ({
    key: String(entry.weekday),
    label: weekdays.value[entry.weekday]!,
    value: entry.value,
    displayValue: formatMetric(entry.value),
    intensity: max ? Math.round(28 + (entry.value / max) * 62) : 28,
  }))
})

const peakWeekday = computed(
  () =>
    [...view.value.weekdays].sort(
      (left, right) => right.value - left.value || left.weekday - right.weekday
    )[0]
)

const weekdaySummary = computed(() => {
  const peak = peakWeekday.value
  if (!peak || peak.value <= 0) return ''
  return t('dashboard.distribution.weekdayPeak', {
    weekday: weekdayNames.value[peak.weekday],
    value: formatMetric(peak.value),
  })
})

/** The year band hides the ranked rows, so its title names the busiest day. */
const topDaySummary = computed(() => {
  const top = topSegments.value[0]
  if (period.value !== 'year' || !top) return ''
  return `${top.label} ${top.displayValue}`
})
</script>

<template>
  <ConsoleCard stretch data-usage-distribution>
    <div v-if="loading" class="flex grow flex-col" data-usage-loading>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <SkeletonBlock class="h-12 w-40" rounded="xl" />
        <SkeletonBlock class="h-11 w-72 sm:h-10" rounded="xl" />
      </div>
      <SkeletonBlock
        class="usage-stage mt-3 h-64 grow lg:h-auto"
        rounded="xl"
      />
      <SkeletonBlock class="mt-3 h-5" rounded="md" />
    </div>

    <EmptyState
      v-else-if="error"
      :title="t('dashboard.loadFailed')"
      :hint="error"
    />
    <div v-else class="flex grow flex-col" data-usage-content>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ periodLabel }} · {{ metricLabel }}
          </p>
          <p
            class="mt-0.5 truncate text-xl font-bold tabular-nums text-[var(--text-primary)] sm:text-2xl"
            aria-live="polite"
            data-usage-total
          >
            {{ formatMetric(view.total) }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <SegmentedToggle
            :model-value="period"
            :options="periodOptions"
            :label="t('dashboard.distribution.periodControl')"
            size="sm"
            @update:model-value="period = $event as UsageDistributionPeriod"
          />
          <SegmentedToggle
            :model-value="metric"
            :options="metricOptions"
            :label="t('dashboard.distribution.metricControl')"
            size="sm"
            @update:model-value="metric = $event as UsageDistributionMetric"
          />
        </div>
      </div>

      <div
        class="usage-stage mt-3 grid grow content-center gap-3"
        :class="stageColumns"
        data-usage-stage
      >
        <UsageHeatmapGrid
          :view="view"
          :period="period"
          :metric="metric"
          :metric-color="metricColor"
          :period-label="periodLabel"
          :metric-label="metricLabel"
        />

        <aside
          class="grid gap-3 border-t border-[var(--border-subtle)] pt-3 min-[360px]:grid-cols-2"
          :class="analyticsClasses"
          :data-usage-analytics="period === 'year' ? 'bottom' : 'side'"
        >
          <UsageSegmentBar
            :title="t('dashboard.distribution.topDays')"
            :segments="topSegments"
            :color="metricColor"
            :empty-label="t('dashboard.stats.noData')"
            :summary="topDaySummary"
            :reserved-rows="USAGE_TOP_DAY_LIMIT"
            :compact="period === 'year'"
            layout="ranked"
          />
          <UsageSegmentBar
            :title="t('dashboard.distribution.weekdayRhythm')"
            :segments="weekdaySegments"
            :color="metricColor"
            :empty-label="t('dashboard.stats.noData')"
            :summary="weekdaySummary"
            :compact="period === 'year'"
            layout="rhythm"
          />
        </aside>
      </div>

      <div
        class="-mb-2 mt-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-[var(--border-subtle)] pt-2 text-[0.625rem] text-[var(--text-tertiary)]"
        data-usage-footer
      >
        <div class="flex items-center gap-1.5">
          <span>{{ t('dashboard.distribution.less') }}</span>
          <span
            v-for="level in [0, 1, 2, 3, 4, 5] as const"
            :key="level"
            class="size-2.5 rounded-xs border"
            :style="usageHeatStyle(level, metricColor)"
          />
          <span>{{ t('dashboard.distribution.more') }}</span>
        </div>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>{{
            t('dashboard.distribution.activeDays', { count: view.activeDays })
          }}</span>
          <span>
            {{ t('dashboard.distribution.peak') }}
            <strong class="font-semibold text-[var(--text-secondary)]">
              {{ view.peak ? formatMetric(view.peak.value) : '--' }}
            </strong>
          </span>
        </div>
      </div>
    </div>
  </ConsoleCard>
</template>

<style scoped>
/*
 * From sm every period fits one stage height (month 169px, quarter 154px, year
 * within it), so switching periods keeps the card height; from xl this also
 * aligns the overview row with BalanceCard. Surplus row height is centred.
 */
@media (min-width: 640px) {
  .usage-stage {
    min-height: 10.625rem;
  }
}

/* Touch year cells need seven 24px rows plus the analytics band. Reserve the
   same space for every period so switching does not move the cards below. */
@media (min-width: 640px) and (any-pointer: coarse) {
  .usage-stage {
    min-height: 18rem;
  }
}
</style>
