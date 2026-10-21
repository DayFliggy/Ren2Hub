import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { getUsageSummary } from '@/api/dashboard'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import type { FlowPoint } from '@/composables/useDashboard'

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

export interface HourlyPoint {
  hour: string
  requests: number
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
  const loading = ref(false)
  const range = ref<StatsRange>('30d')
  const customStart = ref<string>('')
  const customEnd = ref<string>('')
  const data = ref<StatsPeriod | null>(null)
  const error = ref<string | null>(null)
  const statsRequest = useLatestRequest()

  async function load() {
    loading.value = true
    error.value = null
    data.value = null
    const params: Record<string, string> = {
      range: range.value,
    }
    if (range.value === 'custom') {
      params.start = customStart.value
      params.end = customEnd.value
    }
    // Serialized via useLatestRequest: rapid range switches abort the older
    // request, and a late response can never overwrite the newer period.
    const result = await statsRequest.run((signal) =>
      getUsageSummary(params, signal)
    )
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
