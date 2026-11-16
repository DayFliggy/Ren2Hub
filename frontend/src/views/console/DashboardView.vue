<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import OverviewKpiStrip from '@/components/console/dashboard/OverviewKpiStrip.vue'
import BalanceCard from '@/components/console/dashboard/BalanceCard.vue'
import BonusCard from '@/components/console/dashboard/BonusCard.vue'
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
import { useDashboardResources } from '@/composables/useDashboardResources'
import { useDashboardStats } from '@/composables/useDashboardStats'
import { useUsageDistribution } from '@/composables/useUsageDistribution'
import type { StatsRange } from '@/composables/useDashboardStats'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { dateInputValue } from '@/utils/format'
import { greetingBucketForHour } from '@/utils/greeting'
import type { DashboardScope } from '@/api/dashboard'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
// A page-local choice cannot carry privileged data across accounts or sessions.
const selectedScope = ref<DashboardScope>('self')
const scope = computed(() => (auth.isAdmin ? selectedScope.value : 'self'))
const scopeOptions = computed(() => [
  { value: 'self', label: t('dashboard.scopeSelf') },
  { value: 'all', label: t('dashboard.scopeAll') },
])
const {
  loading,
  stats,
  share,
  flow,
  tokenTrend,
  recentOutOfRange,
  usageUnavailable,
  error: dashboardError,
  topupBonus,
  topupBonusLoading,
  topupBonusError,
  load,
  reset,
} = useDashboard(scope)
const resources = useDashboardResources()
const statsComposable = useDashboardStats(scope)
const distribution = useUsageDistribution(scope)

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
  items.push({ key: 'autoroute', label: t('dashboard.autoRoute.tabLabel') })
  return items
})
const activeTab = ref('overview')
const dashboardPanelId = 'dashboard-tab-panel'

watch(
  [() => auth.user?.id, () => auth.user?.role],
  () => {
    selectedScope.value = 'self'
  },
  { flush: 'sync' }
)

watch([scope, () => auth.user?.id, () => auth.user?.role], () => {
  reset()
  distribution.reset()
  statsComposable.reset()
  if (!auth.user) return
  void load()
  void distribution.load()
  if (activeTab.value === 'stats') void statsComposable.load()
})

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
  <div data-dashboard-page :data-dashboard-scope="scope">
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
    >
      <template #actions>
        <SegmentedToggle
          v-if="auth.isAdmin && activeTab !== 'autoroute'"
          class="sm:mt-2"
          :model-value="scope"
          :options="scopeOptions"
          :label="t('dashboard.scopeLabel')"
          @update:model-value="
            selectedScope = $event === 'all' ? 'all' : 'self'
          "
        />
      </template>
    </PageHero>

    <div
      :id="dashboardPanelId"
      :key="activeTab"
      class="console-panel-enter min-w-0"
      role="tabpanel"
      :aria-labelledby="`${dashboardPanelId}-tab-${activeTab}`"
    >
      <!-- ══════════════════════════════════════════
         Tab: Overview
    ══════════════════════════════════════════ -->
      <div
        v-if="activeTab === 'overview'"
        class="dashboard-overview sibling-space-y-4 sm:sibling-space-y-5"
      >
        <!-- KPI strip — 4 clickable chips, jumps to Stats tab -->
        <OverviewKpiStrip
          :stats="stats"
          :flow="flow"
          :token-trend="tokenTrend"
          :loading="loading"
          @switch-tab="activeTab = $event"
        />

        <div class="dashboard-cards grid gap-4 sm:gap-5 xl:grid-cols-3">
          <!-- 个人汇总失败只替换个人卡片；系统状态来自独立接口，继续显示。 -->
          <EmptyState
            v-if="dashboardError"
            class="min-h-64 min-w-0 xl:col-span-3"
            :title="$t('dashboard.loadFailed')"
            :hint="dashboardError"
          />
          <template v-else>
            <!-- 总额度（限速已并入 KPI 条的 RPM 格） -->
            <BalanceCard
              v-if="scope === 'self'"
              class="min-w-0"
              :quota="stats?.quota ?? null"
              :used-quota="stats?.used_quota ?? null"
              :today-quota="stats?.today_quota"
              :month-quota="stats?.month_quota"
              :daily-burn="dailyBurn"
            />
            <!-- Usage distribution: the calendar and its analytics are both
                 width-capped, so it keeps two columns and system status fills
                 the third in either scope. -->
            <UsageDistributionCard
              class="min-w-0 xl:col-span-2"
              :points="distribution.points.value"
              :covered-from="distribution.coveredFrom.value"
              :loading="distribution.loading.value"
              :error="distribution.error.value"
              :unavailable-reason="distribution.unavailableReason.value"
            />
          </template>

          <!-- 系统状态：所有登录用户可读的共享快照 -->
          <SystemStatusCard
            class="min-w-0"
            :metrics="resources.system.value"
            :error="resources.systemError.value"
            :loading="resources.systemLoading.value"
          />
          <!-- Token 使用趋势 -->
          <TokenTrendCard
            v-if="!dashboardError"
            class="min-w-0"
            :class="scope === 'all' ? 'xl:col-span-3' : 'xl:col-span-2'"
            :points="tokenTrend"
            :loading="loading"
            :unavailable-reason="
              usageUnavailable
                ? t('dashboard.stats.unavailable')
                : recentOutOfRange
            "
          />
          <!-- 折扣来自独立接口，个人汇总失败时照常显示 -->
          <BonusCard
            v-if="scope === 'self'"
            class="min-w-0"
            :bonus="topupBonus"
            :loading="topupBonusLoading"
            :error="topupBonusError"
            :can-configure="auth.isRoot && app.isFeatureEnabled('admin')"
          />
          <template v-if="!dashboardError">
            <!-- 模型消费分布 -->
            <ModelDistributionCard
              class="min-w-0"
              :class="scope === 'all' ? 'xl:col-span-3' : 'xl:col-span-2'"
              :items="share"
              :loading="loading"
              :unavailable-reason="
                usageUnavailable
                  ? t('dashboard.stats.unavailable')
                  : recentOutOfRange
              "
            />
          </template>
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
      <div v-else-if="activeTab === 'autoroute'" class="min-w-0">
        <AutoRoutePanel />
      </div>
    </div>

    <ContactFloatBall />
  </div>
</template>

<style scoped>
.dashboard-overview {
  container: dashboard-overview / inline-size;
}

/* Sidebar width and enlarged text can leave too little space for three cards,
   even when the viewport still satisfies the desktop media breakpoint. */
@container dashboard-overview (max-width: 58rem) {
  .dashboard-cards {
    grid-template-columns: minmax(0, 1fr);
  }
  .dashboard-cards > * {
    grid-column: auto;
  }
}
</style>
