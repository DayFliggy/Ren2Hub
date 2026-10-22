import { useLatestRequest } from './useLatestRequest'
import { ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import {
  currentMonthRange,
  getUsageSummary,
  logRangeLimit,
  summaryFits,
  type SummaryRange,
} from '@/api/dashboard'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

export interface DashboardStats {
  quota: string
  used_quota: string
  today_quota: string
  month_quota?: string
  today_requests: number
  total_requests: number
}

export interface UserLimits {
  /** Account request ceiling; 0 = unmetered. */
  rate_limit: number
  current_rpm: number // observed throughput
}

export interface UserDiscounts {
  global_ratio: number // platform-wide pricing multiplier, e.g. 0.88
  effective_ratio: number // effective pricing multiplier
}

export interface ModelShare {
  model: string
  ratio: number
  quota: string
  requests: number
  tokens: number
}

export interface TokenTrendPoint {
  total: number
  date: string
  cache_create: number
  cache_read: number
}

export interface SystemMetrics {
  cpu_percent: number | null
  /** Host logical CPUs — the scale a load average is read against. */
  cpu_cores: number | null
  /** Host run-queue averages over 1 / 5 / 15 minutes; null off Unix hosts. */
  load_average: { one: number; five: number; fifteen: number } | null
  memory_used_gb: number | null
  memory_total_gb: number | null
  bandwidth_up_mbps: number | null
  bandwidth_down_mbps: number | null
  disk_used_gb: number | null
  disk_total_gb: number | null
  api_success_rate: number | null
  /** Recent throughput samples, oldest → newest; the last pair is the live figure. */
  bandwidth_series: { up: number[]; down: number[] } | null
}

export interface FlowPoint {
  date: string
  consume: string
  requests: number
}

export function useBalanceVisibility() {
  const hidden = useLocalStorage('renren_hide_balance', false)
  return {
    hidden,
    toggle: () => {
      hidden.value = !hidden.value
    },
  }
}
export function useDashboard() {
  const { t } = useI18n(),
    toast = useToast(),
    auth = useAuthStore(),
    app = useAppStore(),
    request = useLatestRequest()
  const loading = ref(true),
    error = ref<string | null>(null),
    stats = ref<DashboardStats | null>(null),
    share = ref<ModelShare[]>([]),
    flow = ref<FlowPoint[]>([]),
    tokenTrend = ref<TokenTrendPoint[]>([]),
    flushIntervalMinutes = ref<number | null>(null)
  // No public native endpoint exposes host metrics, account RPM limits or discounts.
  const system = ref<SystemMetrics | null>(null),
    limits = ref<UserLimits | null>(null),
    discounts = ref<UserDiscounts | null>(null)
  async function load() {
    loading.value = true
    error.value = null
    stats.value = null
    share.value = []
    flow.value = []
    tokenTrend.value = []
    flushIntervalMinutes.value = null
    const result = await request.run((signal) => {
      const limit = logRangeLimit(app.status)
      const recentRange: SummaryRange = { range: '30d' }
      if (!summaryFits(recentRange, limit, false)) {
        throw new ApiError(t('dashboard.stats.outOfRange', { days: limit }))
      }
      // No overview card shows deltas, so no window asks for a comparison.
      // A month past the log limit is skipped and its card shows no figure.
      const month = currentMonthRange()
      return Promise.all([
        getUsageSummary(recentRange, false, signal),
        summaryFits(month, limit, false)
          ? getUsageSummary(month, false, signal)
          : null,
        getUsageSummary({ range: 'today' }, false, signal),
      ])
    })
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      error.value =
        result.error instanceof Error
          ? result.error.message
          : t('common.failed')
      toast.error(error.value)
      return
    }
    const [recent, month, today] = result.value
    if (
      [recent, month, today].some(
        (summary) => summary?.meta.available === false
      )
    ) {
      error.value = t('dashboard.stats.unavailable')
      return
    }
    if (!auth.user) return
    stats.value = {
      quota: String(auth.user.quota),
      used_quota: String(auth.user.used_quota),
      today_quota: today.kpi.totalQuota,
      month_quota: month?.kpi.totalQuota,
      today_requests: today.kpi.totalRequests,
      total_requests: auth.user.request_count,
    }
    share.value = recent.models.map((row) => ({
      model: row.model,
      ratio: row.share,
      quota: row.quota,
      requests: row.requests,
      tokens: row.tokens,
    }))
    flow.value = recent.flow
    tokenTrend.value = recent.token_trend
    flushIntervalMinutes.value = recent.meta.flush_interval_minutes
  }
  return {
    loading,
    stats,
    share,
    flow,
    tokenTrend,
    flushIntervalMinutes,
    system,
    limits,
    discounts,
    error,
    load,
  }
}
