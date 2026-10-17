<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import MiniSparkline from './MiniSparkline.vue'
import { kpiDividerClasses } from './kpiDividers'
import {
  useBalanceVisibility,
  type DashboardStats,
  type FlowPoint,
  type TotalTokenPoint,
} from '@/composables/useDashboard'
import { formatQuota, formatNumber } from '@/utils/format'
import { quotaForChart } from '@/utils/quota'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
const props = defineProps<{
  stats: DashboardStats | null
  loading?: boolean
  flow?: FlowPoint[]
  tokenTrend?: TotalTokenPoint[]
}>()
const emit = defineEmits<{ switchTab: [tab: string] }>()
const { t } = useI18n({ useScope: 'global' })
const { t: r } = useI18n({
  useScope: 'local',
  messages: restoredConsoleMessages,
})
const { hidden } = useBalanceVisibility()
const cells = computed(() => [
  {
    key: 'spend',
    label: t('dashboard.statTodaySpend'),
    value: !props.stats
      ? '—'
      : hidden.value
        ? '••••'
        : formatQuota(props.stats.today_quota),
    points: hidden.value
      ? []
      : (props.flow ?? [])
          .slice(-14)
          .map((point) => quotaForChart(point.consume)),
  },
  {
    key: 'requests',
    label: t('dashboard.statTodayRequests'),
    value: props.stats ? formatNumber(props.stats.today_requests) : '—',
    points: (props.flow ?? []).slice(-14).map((point) => point.requests),
  },
  {
    key: 'tokens',
    label: t('dashboard.statTodayTokens'),
    value: props.stats ? formatNumber(props.stats.today_tokens ?? 0) : '—',
    points: (props.tokenTrend ?? []).slice(-14).map((point) => point.total),
  },
  { key: 'rpm', label: r('restored.rpm'), value: '—', points: null },
  { key: 'tpm', label: r('restored.tpm'), value: '—', points: null },
])
</script>
<template>
  <section
    class="pencil-surface overflow-hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
    data-overview-kpi
    :aria-busy="loading"
  >
    <div class="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-5">
      <component
        :is="cell.points === null ? 'div' : 'button'"
        v-for="(cell, index) in cells"
        :key="cell.key"
        :type="cell.points === null ? undefined : 'button'"
        :data-kpi-cell="cell.key"
        class="relative flex min-w-0 flex-col px-5 py-4 text-left"
        :class="[
          ...kpiDividerClasses(index),
          cell.points === null
            ? ''
            : 'hover:bg-[var(--surface-muted)] focus-ring',
        ]"
        @click="cell.points !== null && emit('switchTab', 'stats')"
      >
        <p class="text-xs text-[var(--text-tertiary)]">{{ cell.label }}</p>
        <p class="display-number mt-3 truncate text-2xl">
          {{ loading && cell.points !== null ? '…' : cell.value }}
        </p>
        <MiniSparkline
          v-if="cell.points && cell.points.length > 1 && !loading"
          class="mt-3"
          :points="cell.points"
        />
        <p
          v-if="cell.points === null"
          class="mt-3 text-xs leading-5 text-[var(--text-tertiary)]"
        >
          {{ r('restored.rateHint') }}
        </p>
      </component>
    </div>
  </section>
</template>
