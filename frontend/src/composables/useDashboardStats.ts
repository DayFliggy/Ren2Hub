import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  getUsageSummary,
  logRangeLimit,
  SUMMARY_MAX_DAYS,
  summaryFits,
  type SummaryRange,
} from '@/api/dashboard'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import type { FlowPoint } from '@/composables/useDashboard'
import { useAppStore } from '@/stores/app'

export type StatsRange = 'today' | '7d' | '30d' | 'custom'

export interface StatsKpi {
  totalTokens: number
  totalQuota: string
  totalRequests: number
  avgLatency: number | null
  successRate: number | null
}

export interface StatsModelRow {
  model: string
  tokens: number
  quota: string
  requests: number
  share: number
  avgLatency: number | null
}

/** One hour of the selected range; `ts` is the bucket start in Unix seconds. */
export interface HourlyPoint {
  ts: number
  requests: number
  tokens: number
  quota: string
}

export interface StatsComparison {
  quotaDelta: number | null
  requestsDelta: number | null
}

export interface StatsPeriod {
  kpi: StatsKpi
  comparison: StatsComparison | null
  models: StatsModelRow[]
  hourly: HourlyPoint[]
  flow: FlowPoint[]
}

export function useDashboardStats() {
  const { t } = useI18n()
  const toast = useToast()
  const app = useAppStore()
  const loading = ref(false)
  const range = ref<StatsRange>('30d')
  const customStart = ref<string>('')
  const customEnd = ref<string>('')
  const data = ref<StatsPeriod | null>(null)
  const error = ref<string | null>(null)
  const statsRequest = useLatestRequest()

  function selectedRange(): SummaryRange {
    if (range.value !== 'custom') return { range: range.value }
    return { range: 'custom', start: customStart.value, end: customEnd.value }
  }

  async function load() {
    loading.value = true
    error.value = null
    data.value = null
    const request = selectedRange()
    // Serialized via useLatestRequest: rapid range switches abort the older
    // request, and a late response can never overwrite the newer period.
    const result = await statsRequest.run((signal) => {
      const limit = logRangeLimit(app.status)
      if (!summaryFits(request, limit, false)) {
        const days = Math.min(limit, SUMMARY_MAX_DAYS)
        throw new ApiError(t('dashboard.stats.outOfRange', { days }))
      }
      // The prior window shares the log limit; drop it rather than the period.
      const comparison = summaryFits(request, limit, true)
      return getUsageSummary(request, comparison, signal)
    })
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      const message =
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      error.value = message
      toast.error(message)
      return
    }
    if (!result.value.meta.available) {
      error.value = t('dashboard.stats.unavailable')
      return
    }
    data.value = result.value
  }

  return { loading, range, customStart, customEnd, data, error, load }
}
