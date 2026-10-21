import { useLatestRequest } from './useLatestRequest'
import { ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { getUsageSummary, type UsageSummary } from '@/api/dashboard'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'

export interface DashboardStats {
  quota: string
  used_quota: string
  today_quota: string
  month_quota?: string
  today_requests: number
  total_requests: number
  month_quota_delta: number | null
  month_requests_delta: number | null
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
  input: number | null
  output: number | null
  cache_create: number | null
  cache_read: number | null
  hit_rate: number | null
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
    request = useLatestRequest()
  const loading = ref(true),
    error = ref<string | null>(null),
    stats = ref<DashboardStats | null>(null),
    share = ref<ModelShare[]>([]),
    flow = ref<FlowPoint[]>([]),
    tokenTrend = ref<TokenTrendPoint[]>([]),
    cacheCoverage = ref<UsageSummary['meta']['cache_coverage'] | null>(null)
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
    cacheCoverage.value = null
    const now = new Date(),
      start = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`,
      end = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    const result = await request.run((signal) =>
      Promise.all([
        getUsageSummary({ range: '30d' }, signal),
        getUsageSummary({ range: 'custom', start, end }, signal),
        getUsageSummary({ range: 'today' }, signal),
      ])
    )
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
    if ([recent, month, today].some((summary) => !summary.meta.available)) {
      error.value = t('dashboard.stats.unavailable')
      return
    }
    if (!auth.user) return
    stats.value = {
      quota: String(auth.user.quota),
      used_quota: String(auth.user.used_quota),
      today_quota: today.kpi.totalQuota,
      month_quota: month.kpi.totalQuota,
      today_requests: today.kpi.totalRequests,
      total_requests: auth.user.request_count,
      month_quota_delta: month.comparison?.quotaDelta ?? null,
      month_requests_delta: month.comparison?.requestsDelta ?? null,
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
    cacheCoverage.value = recent.meta.cache_coverage
  }
  return {
    loading,
    stats,
    share,
    flow,
    tokenTrend,
    cacheCoverage,
    system,
    limits,
    discounts,
    error,
    load,
  }
}
