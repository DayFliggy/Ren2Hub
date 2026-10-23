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
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

export interface DashboardStats {
  quota: string
  used_quota: string
  today_quota?: string
  month_quota?: string
  today_requests?: number
  today_tokens?: number
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
    flushIntervalMinutes = ref<number | null>(null),
    // Set when the 30-day window exceeds the log limit; names that limit.
    recentOutOfRange = ref<string | null>(null)
  const usageUnavailable = ref(false)
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
    recentOutOfRange.value = null
    usageUnavailable.value = false
    const result = await request.run(async (signal) => {
      const limit = logRangeLimit(app.status)
      // No overview card shows deltas, so no window asks for a comparison.
      // A window past the log limit is skipped rather than shortened; the
      // today window and the balance do not depend on it.
      const fitting = (range: SummaryRange) =>
        summaryFits(range, limit, false)
          ? getUsageSummary(range, false, signal)
          : null
      const [recent, month, today] = await Promise.all([
        fitting({ range: '30d' }),
        fitting(currentMonthRange()),
        getUsageSummary({ range: 'today' }, false, signal),
      ])
      return { limit, recent, month, today }
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
    const { limit, recent, month, today } = result.value
    if (!auth.user) return
    // Keep account facts visible even when the optional quota_data source is
    // disabled. Usage-backed fields remain absent rather than becoming zero.
    usageUnavailable.value = [recent, month, today].some(
      (summary) => summary?.meta.available === false
    )
    stats.value = {
      quota: String(auth.user.quota),
      used_quota: String(auth.user.used_quota),
      total_requests: auth.user.request_count,
      ...(today.meta.available
        ? {
            today_quota: today.kpi.totalQuota,
            today_requests: today.kpi.totalRequests,
            today_tokens: today.kpi.totalTokens,
          }
        : {}),
      ...(month?.meta.available ? { month_quota: month.kpi.totalQuota } : {}),
    }
    // The flush interval is a server setting, the same in every window.
    flushIntervalMinutes.value = today.meta.flush_interval_minutes
    if (!recent) {
      recentOutOfRange.value = t('dashboard.stats.recentOutOfRange', {
        days: limit,
      })
      return
    }
    if (!recent.meta.available) return
    share.value = recent.models.map((row) => ({
      model: row.model,
      ratio: row.share,
      quota: row.quota,
      requests: row.requests,
      tokens: row.tokens,
    }))
    flow.value = recent.flow
    tokenTrend.value = recent.token_trend
  }
  return {
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
    error,
    load,
  }
}
