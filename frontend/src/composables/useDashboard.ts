import { useLatestRequest } from './useLatestRequest'
import { ref, toValue, type MaybeRefOrGetter } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import {
  currentMonthRange,
  getUsageSummary,
  logRangeLimit,
  summaryFits,
  type SummaryRange,
  type DashboardScope,
} from '@/api/dashboard'
import { usageDiscountApi } from '@/api/usageDiscount'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { UsageDiscountSelfStatus } from '@/types/usageDiscount'

export interface DashboardStats {
  quota?: string
  used_quota?: string
  today_quota?: string
  month_quota?: string
  today_requests?: number
  today_tokens?: number
  total_requests?: number
}

/**
 * Price multipliers from GET /api/user/usage-discount: 0.95 means 5% off.
 * Display only; billing reads the exact decimal factor on the server.
 */
export interface UserDiscounts {
  status: UsageDiscountSelfStatus
  global_ratio: number // fixed and site parts, e.g. 0.88
  effective_ratio: number // the factor the next request is priced with
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
export function useDashboard(scope: MaybeRefOrGetter<DashboardScope> = 'self') {
  const { t } = useI18n(),
    toast = useToast(),
    auth = useAuthStore(),
    app = useAppStore(),
    request = useLatestRequest(),
    discountRequest = useLatestRequest()
  const loading = ref(true),
    error = ref<string | null>(null),
    stats = ref<DashboardStats | null>(null),
    share = ref<ModelShare[]>([]),
    flow = ref<FlowPoint[]>([]),
    tokenTrend = ref<TokenTrendPoint[]>([]),
    // Set when the 30-day window exceeds the log limit; names that limit.
    recentOutOfRange = ref<string | null>(null)
  const usageUnavailable = ref(false)
  const discounts = ref<UserDiscounts | null>(null),
    discountsLoading = ref(true),
    discountsError = ref<string | null>(null)
  // The discount has its own request, so a failed or partial usage summary
  // never hides it.
  async function load() {
    await Promise.all([loadStats(), loadDiscounts()])
  }
  function reset() {
    request.cancel()
    discountRequest.cancel()
    loading.value = false
    error.value = null
    stats.value = null
    share.value = []
    flow.value = []
    tokenTrend.value = []
    recentOutOfRange.value = null
    usageUnavailable.value = false
    discounts.value = null
    discountsLoading.value = false
    discountsError.value = null
  }
  async function loadDiscounts() {
    discountRequest.cancel()
    discounts.value = null
    discountsError.value = null
    if (toValue(scope) === 'all') {
      discountsLoading.value = false
      return
    }
    discountsLoading.value = true
    const result = await discountRequest.run((signal) =>
      usageDiscountApi.self(signal)
    )
    if (result.stale) return
    discountsLoading.value = false
    if (!result.ok) {
      discountsError.value =
        result.error instanceof Error
          ? result.error.message
          : t('common.failed')
      return
    }
    discounts.value = {
      status: result.value.status,
      global_ratio: Number(result.value.global_factor),
      effective_ratio: Number(result.value.factor),
    }
  }
  async function loadStats() {
    const selectedScope = toValue(scope)
    loading.value = true
    error.value = null
    stats.value = null
    share.value = []
    flow.value = []
    tokenTrend.value = []
    recentOutOfRange.value = null
    usageUnavailable.value = false
    const result = await request.run(async (signal) => {
      const limit = logRangeLimit(app.status)
      // No overview card shows deltas, so no window asks for a comparison.
      // A window past the log limit is skipped rather than shortened; the
      // today window and the balance do not depend on it.
      const fitting = (range: SummaryRange) =>
        summaryFits(range, limit, false)
          ? getUsageSummary(range, false, signal, selectedScope)
          : null
      const [recent, month, today] = await Promise.all([
        fitting({ range: '30d' }),
        fitting(currentMonthRange()),
        getUsageSummary({ range: 'today' }, false, signal, selectedScope),
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
      ...(selectedScope === 'self'
        ? {
            quota: String(auth.user.quota),
            used_quota: String(auth.user.used_quota),
            total_requests: auth.user.request_count,
          }
        : {}),
      ...(today.meta.available
        ? {
            today_quota: today.kpi.totalQuota,
            today_requests: today.kpi.totalRequests,
            today_tokens: today.kpi.totalTokens,
          }
        : {}),
      ...(month?.meta.available ? { month_quota: month.kpi.totalQuota } : {}),
    }
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
    recentOutOfRange,
    usageUnavailable,
    error,
    discounts,
    discountsLoading,
    discountsError,
    load,
    reset,
  }
}
