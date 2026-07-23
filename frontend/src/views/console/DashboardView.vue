<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import BalanceCard from '@/components/console/dashboard/BalanceCard.vue'
import ModelShareDonut from '@/components/console/dashboard/ModelShareDonut.vue'
import MyPlans from '@/components/console/dashboard/MyPlans.vue'
import RecentActivity from '@/components/console/dashboard/RecentActivity.vue'
import ServiceStatus from '@/components/console/dashboard/ServiceStatus.vue'
import TrendChart from '@/components/console/dashboard/TrendChart.vue'
import ContactFloatBall from '@/components/console/ContactFloatBall.vue'
import PageHero from '@/components/console/PageHero.vue'
import { useDashboard } from '@/composables/useDashboard'
import { useAuthStore } from '@/stores/auth'
import { formatNumber, formatQuota } from '@/utils/format'

const { t } = useI18n()
const auth = useAuthStore()
const { loading, stats, share, flow, recent, load } = useDashboard()

onMounted(() => {
  void load()
})

const greeting = computed(() =>
  t('dashboard.greeting', {
    name: auth.user?.display_name || auth.user?.username || '',
  })
)

const tabs = computed(() => [
  { key: 'overview', label: t('dashboard.tabOverview') },
  { key: 'stats', label: t('dashboard.tabStats') },
])
const activeTab = ref('overview')

const categories = computed(() => flow.value.map((f) => f.date))
const consumeSeries = computed(() => flow.value.map((f) => f.consume))
const requestSeries = computed(() => flow.value.map((f) => f.requests))
</script>

<template>
  <div>
    <PageHero
      v-model:tab="activeTab"
      :title="greeting"
      :crumbs="[$t('dashboard.breadcrumb.0'), $t('dashboard.breadcrumb.1')]"
      :tabs="tabs"
    />

    <!-- desktop: 3 columns · tablet: 2 · mobile: 1 (design images 02/03/04) -->
    <div v-if="loading" class="grid gap-5 lg:grid-cols-3">
      <div
        v-for="i in 6"
        :key="i"
        class="h-56 animate-pulse rounded-2xl bg-[var(--surface-muted)]"
      />
    </div>

    <div v-else class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
      <!-- column 1 -->
      <div class="space-y-5">
        <BalanceCard
          :quota="stats?.quota ?? 0"
          :delta="stats?.month_quota_delta"
        />
        <RecentActivity :items="recent" />
      </div>

      <!-- column 2 -->
      <div class="space-y-5">
        <ModelShareDonut :items="share" />
        <TrendChart
          :title="t('dashboard.consumeTrend')"
          :headline="formatQuota(stats?.used_quota ?? 0)"
          :delta="stats?.month_quota_delta"
          :note="
            t('dashboard.monthConsume', {
              value: formatQuota(stats?.today_quota ?? 0),
            })
          "
          :categories="categories"
          :series="consumeSeries"
          :value-formatter="(v: number) => formatQuota(v)"
        />
        <TrendChart
          :title="t('dashboard.requestTrend')"
          :headline="formatNumber(stats?.total_requests ?? 0)"
          :delta="stats?.month_requests_delta"
          :note="
            t('dashboard.monthRequests', {
              value: formatNumber(stats?.today_requests ?? 0),
            })
          "
          :categories="categories"
          :series="requestSeries"
          color-role="signal"
        />
      </div>

      <!-- column 3 -->
      <div class="space-y-5 md:col-span-2 xl:col-span-1">
        <MyPlans />
        <ServiceStatus />
      </div>
    </div>

    <!-- Contact float ball (bottom-right) -->
    <ContactFloatBall />
  </div>
</template>
