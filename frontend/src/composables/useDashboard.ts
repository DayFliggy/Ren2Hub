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
import { topupBonusApi } from '@/api/topupBonus'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { TopupBonusSelf } from '@/types/topupBonus'

export interface DashboardStats {
  quota?: string
  used_quota?: string
  today_quota?: string
  month_quota?: string
  today_requests?: number
  today_tokens?: number
  total_requests?: number
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
    topupBonusRequest = useLatestRequest()
  const loading = ref(true),
    error = ref<string | null>(null),
    stats = ref<DashboardStats | null>(null),
    share = ref<ModelShare[]>([]),
    flow = ref<FlowPoint[]>([]),
    tokenTrend = ref<TokenTrendPoint[]>([]),
    // Set when the 30-day window exceeds the log limit; names that limit.
    recentOutOfRange = ref<string | null>(null)
  const usageUnavailable = ref(false)
  const topupBonus = ref<TopupBonusSelf | null>(null),
    topupBonusLoading = ref(true),
    topupBonusError = ref<string | null>(null)
  // The bonus has its own request, so a failed or partial usage summary
  // never hides it.
  async function load() {
    await Promise.all([loadStats(), loadTopupBonus()])
  }
  function reset() {
    request.cancel()
    topupBonusRequest.cancel()
    loading.value = false
    error.value = null
    stats.value = null
    share.value = []
    flow.value = []
    tokenTrend.value = []
    recentOutOfRange.value = null
    usageUnavailable.value = false
    topupBonus.value = null
    topupBonusLoading.value = false
    topupBonusError.value = null
  }
  async function loadTopupBonus() {
    topupBonusRequest.cancel()
    topupBonus.value = null
    topupBonusError.value = null
    if (toValue(scope) === 'all') {
      topupBonusLoading.value = false
      return
    }
    topupBonusLoading.value = true
    const result = await topupBonusRequest.run((signal) =>
      topupBonusApi.self(signal)
    )
    if (result.stale) return
    topupBonusLoading.value = false
    if (!result.ok) {
      topupBonusError.value =
        result.error instanceof Error
          ? result.error.message
          : t('common.failed')
      return
    }
    topupBonus.value = result.value
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
    topupBonus,
    topupBonusLoading,
    topupBonusError,
    load,
    reset,
  }
}
