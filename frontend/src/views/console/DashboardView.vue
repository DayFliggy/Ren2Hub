<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import OverviewKpiStrip from '@/components/console/dashboard/OverviewKpiStrip.vue'
import BalanceCard from '@/components/console/dashboard/BalanceCard.vue'
import DiscountCard from '@/components/console/dashboard/DiscountCard.vue'
import UsageDistributionCard from '@/components/console/dashboard/UsageDistributionCard.vue'
import TokenTrendCard from '@/components/console/dashboard/TokenTrendCard.vue'
import ModelDistributionCard from '@/components/console/dashboard/ModelDistributionCard.vue'
import SystemStatusCard from '@/components/console/dashboard/SystemStatusCard.vue'
import StatsKpiRow from '@/components/console/dashboard/stats/StatsKpiRow.vue'
import StatsModelTable from '@/components/console/dashboard/stats/StatsModelTable.vue'
import StatsHourlyChart from '@/components/console/dashboard/stats/StatsHourlyChart.vue'
import StatsDualTrend from '@/components/console/dashboard/stats/StatsDualTrend.vue'
import AutoRoutePanel from '@/components/console/dashboard/autoroute/AutoRoutePanel.vue'
import ContactFloatBall from '@/components/console/ContactFloatBall.vue'
import PageHero from '@/components/console/PageHero.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { useDashboard } from '@/composables/useDashboard'
import { useDashboardStats } from '@/composables/useDashboardStats'
import { useUsageDistribution } from '@/composables/useUsageDistribution'
import type { StatsRange } from '@/composables/useDashboardStats'
import { useAuthStore } from '@/stores/auth'
import { dateInputValue } from '@/utils/format'
import { greetingBucketForHour } from '@/utils/greeting'

const { t } = useI18n()
const auth = useAuthStore()
const {
  loading,
  stats,
  share,
  flow,
  tokenTrend,
  flushIntervalMinutes,
  recentOutOfRange,
  usageUnavailable,
  system,
  limits,
  discounts,
  error: dashboardError,
  load,
} = useDashboard()
const statsComposable = useDashboardStats()
const distribution = useUsageDistribution()

onMounted(() => {
  void load()
  void distribution.load()
})

/**
 * Rolling 7-day burn feeds the balance runway estimate — steadier than
 * extrapolating from a single day, which swings wildly on quiet weekends.
 */
const dailyBurn = computed(() => {
  const tail = flow.value.slice(-7)
  if (!tail.length) return undefined
  return (
    tail.reduce((sum, point) => sum + BigInt(point.consume), 0n) /
    BigInt(tail.length)
  ).toString()
})

const greeting = computed(() =>
  t(`dashboard.greeting.${greetingBucketForHour(new Date().getHours())}`)
)

const greetingName = computed(
  () => auth.user?.display_name || auth.user?.username || ''
)

// Without a name the sentence would read "早上好，。", so the separator and the
// closing punctuation only appear alongside the accent.
const greetingSuffix = computed(() =>
  greetingName.value ? t('dashboard.greetingSuffix') : ''
)

const tabs = computed(() => {
  const items = [
    { key: 'overview', label: t('dashboard.tabOverview') },
    { key: 'stats', label: t('dashboard.tabStats') },
  ]
  if (auth.isAdmin) {
    items.push({ key: 'autoroute', label: t('dashboard.autoRoute.tabLabel') })
  }
  return items
})
const activeTab = ref('overview')
const dashboardPanelId = 'dashboard-tab-panel'

watch(activeTab, (tab) => {
  if (
    tab === 'stats' &&
    !statsComposable.data.value &&
    !statsComposable.loading.value
  ) {
    void statsComposable.load()
  }
})

watch(statsComposable.range, (range) => {
  if (activeTab.value !== 'stats') return
  if (range === 'custom') {
    // Seed a sensible window on first switch so the panel is never blank.
    if (
      !statsComposable.customStart.value ||
      !statsComposable.customEnd.value
    ) {
      statsComposable.customStart.value = dateInputValue(-13)
      statsComposable.customEnd.value = dateInputValue()
    }
    return // the date watcher below issues the request
  }
  void statsComposable.load()
})

// Refetch as the custom window changes, but only once both ends are set.
watch(
  [statsComposable.customStart, statsComposable.customEnd],
  ([start, end]) => {
    if (activeTab.value !== 'stats') return
    if (statsComposable.range.value !== 'custom') return
    if (!start || !end) return
    void statsComposable.load()
  }
)

const rangeOptions = computed(() => [
  { value: 'today', label: t('dashboard.stats.rangeToday') },
  { value: '7d', label: t('dashboard.stats.range7d') },
  { value: '30d', label: t('dashboard.stats.range30d') },
  { value: 'custom', label: t('dashboard.stats.rangeCustom') },
])
</script>

<template>
  <div data-dashboard-page>
    <PageHero
      v-model:tab="activeTab"
      :title="greeting"
      :title-accent="greetingName"
      :title-separator="$t('dashboard.greetingSeparator')"
      title-accent-prefix=""
      :title-suffix="greetingSuffix"
      accent-variant="underline"
      :crumbs="[$t('dashboard.breadcrumb.0'), $t('dashboard.breadcrumb.1')]"
      :tabs="tabs"
      :tab-panel-id="dashboardPanelId"
    />

    <div
      :id="dashboardPanelId"
      class="min-w-0"
      role="tabpanel"
      :aria-labelledby="`${dashboardPanelId}-tab-${activeTab}`"
    >
      <!-- ══════════════════════════════════════════
         Tab: Overview
    ══════════════════════════════════════════ -->
      <div
        v-if="activeTab === 'overview'"
        class="sibling-space-y-4 sm:sibling-space-y-5"
      >
        <!-- KPI strip — 4 clickable chips, jumps to Stats tab -->
        <OverviewKpiStrip
          :stats="stats"
          :flow="flow"
          :token-trend="tokenTrend"
          :limits="limits"
          :loading="loading"
          @switch-tab="activeTab = $event"
        />

        <!-- Error State -->
        <EmptyState
          v-if="dashboardError"
          class="min-h-64"
          :title="$t('dashboard.loadFailed')"
          :hint="dashboardError"
        />

        <div v-else class="grid gap-4 sm:gap-5 xl:grid-cols-3">
          <!-- 总额度（限速已并入 KPI 条的 RPM 格） -->
          <BalanceCard
            class="min-w-0"
            :quota="stats?.quota ?? null"
            :used-quota="stats?.used_quota ?? null"
            :today-quota="stats?.today_quota"
            :month-quota="stats?.month_quota"
            :daily-burn="dailyBurn"
          />
          <!-- Usage distribution -->
          <UsageDistributionCard
            class="min-w-0 xl:col-span-2"
            :points="distribution.points.value"
            :covered-from="distribution.coveredFrom.value"
            :loading="distribution.loading.value"
            :error="distribution.error.value"
            :unavailable-reason="distribution.unavailableReason.value"
          />

          <!-- 系统状态 -->
          <SystemStatusCard class="min-w-0" :metrics="system" />
          <!-- Token 使用趋势 -->
          <TokenTrendCard
            class="min-w-0 xl:col-span-2"
            :points="tokenTrend"
            :flush-interval-minutes="flushIntervalMinutes"
            :loading="loading"
            :unavailable-reason="
              usageUnavailable
                ? t('dashboard.stats.unavailable')
                : recentOutOfRange
            "
          />

          <!-- 折扣卡片：暂无数据源，不跟随 load() 的 loading，直接显示未接入 -->
          <DiscountCard class="min-w-0" :discounts="discounts" />
          <!-- 模型消费分布 -->
          <ModelDistributionCard
            class="min-w-0 xl:col-span-2"
            :items="share"
            :loading="loading"
            :unavailable-reason="
              usageUnavailable
                ? t('dashboard.stats.unavailable')
                : recentOutOfRange
            "
          />
        </div>
      </div>

      <!-- ══════════════════════════════════════════
         Tab: Statistics
    ══════════════════════════════════════════ -->
      <div
        v-else-if="activeTab === 'stats'"
        class="sibling-space-y-4 sm:sibling-space-y-5"
      >
        <!-- Range picker — same segmented control as the trend card's mode toggle -->
        <div class="flex flex-wrap items-center gap-3">
          <SegmentedToggle
            :model-value="statsComposable.range.value"
            :options="rangeOptions"
            :label="t('dashboard.stats.rangeLabel')"
            @update:model-value="
              statsComposable.range.value = $event as StatsRange
            "
          />

          <DateRangePicker
            v-if="statsComposable.range.value === 'custom'"
            v-model:start="statsComposable.customStart.value"
            v-model:end="statsComposable.customEnd.value"
            class="w-full sm:w-64"
          />
        </div>

        <StatsKpiRow
          :kpi="statsComposable.data.value?.kpi ?? null"
          :flow="statsComposable.data.value?.flow ?? []"
          :loading="statsComposable.loading.value"
        />
        <StatsDualTrend
          :kpi="statsComposable.data.value?.kpi ?? null"
          :comparison="statsComposable.data.value?.comparison ?? null"
          :flow="statsComposable.data.value?.flow ?? []"
          :loading="statsComposable.loading.value"
          :error="statsComposable.error.value"
        />
        <!--
        Row-aligned pair: the capped model table sets the height and the hourly
        chart grows to share its bottom edge. min-w-0 for the same canvas
        reason as the overview grid.
      -->
        <div class="grid gap-4 sm:gap-5 xl:grid-cols-2">
          <StatsModelTable
            class="min-w-0"
            :models="statsComposable.data.value?.models ?? []"
            :loading="statsComposable.loading.value"
          />
          <StatsHourlyChart
            class="min-w-0"
            :hourly="statsComposable.data.value?.hourly ?? []"
            :loading="statsComposable.loading.value"
            :error="statsComposable.error.value"
          />
        </div>
      </div>

      <!-- ══════════════════════════════════════════
         Tab: Auto Route
    ══════════════════════════════════════════ -->
      <div
        v-else-if="activeTab === 'autoroute' && auth.isAdmin"
        class="min-w-0"
      >
        <AutoRoutePanel />
      </div>
    </div>

    <ContactFloatBall />
  </div>
</template>
