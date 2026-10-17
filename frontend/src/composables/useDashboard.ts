import { addQuota, quotaRatio, sumQuota } from '@/utils/quota'
import { useLatestRequest } from './useLatestRequest'
import { ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { parseUsageRows, type UsageRow } from '@/api/liveContracts'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'

export interface DashboardStats {
  quota: string
  used_quota: string
  today_quota: string
  month_quota?: string
  today_tokens?: number
  today_requests: number
  total_requests: number
}

export interface ModelShare {
  model: string
  ratio: number
  quota: string
  requests: number
  tokens: number
}

export interface FlowPoint {
  date: string
  consume: string
  requests: number
}

const DAY_SECONDS = 86_400

function localDateKey(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function recentLocalDateKeys(now = new Date(), days = 30): string[] {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() - (days - index - 1))
    return localDateKey(date)
  })
}

export function getLocalDayStartTimestamp(now = new Date()): number {
  return Math.floor(
    new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000
  )
}

export function getLocalMonthStartTimestamp(now = new Date()): number {
  return Math.floor(
    new Date(now.getFullYear(), now.getMonth(), 1).getTime() / 1000
  )
}

export async function fetchSelfUsage(
  startTimestamp: number,
  endTimestamp: number,
  signal?: AbortSignal
): Promise<UsageRow[]> {
  return parseUsageRows(
    await api.get<unknown>(
      '/api/data/self',
      { start_timestamp: startTimestamp, end_timestamp: endTimestamp },
      signal ? { signal } : undefined
    )
  )
}

/** Balance visibility preference, shared across dashboard & wallet. */
export function useBalanceVisibility() {
  const hidden = useLocalStorage('renren_hide_balance', false)
  return { hidden, toggle: () => (hidden.value = !hidden.value) }
}

export function useDashboard() {
  const { t } = useI18n()
  const toast = useToast()
  const auth = useAuthStore()
  const loading = ref(true)
  const stats = ref<DashboardStats | null>(null)
  const share = ref<ModelShare[]>([])
  const flow = ref<FlowPoint[]>([])
  const error = ref<string | null>(null),
    request = useLatestRequest()

  async function load() {
    loading.value = true
    error.value = null
    const endTimestamp = Math.floor(Date.now() / 1000)
    const startTimestamp = endTimestamp - 29 * DAY_SECONDS
    const monthStartTimestamp = getLocalMonthStartTimestamp()
    const result = await request.run(async (signal) =>
      Promise.all([
        fetchSelfUsage(startTimestamp, endTimestamp, signal),
        fetchSelfUsage(monthStartTimestamp, endTimestamp, signal),
      ])
    )
    if (result.stale) return
    if (!result.ok) {
      error.value =
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      toast.error(error.value)
      loading.value = false
      return
    }
    const [usage, monthUsage] = result.value
    const todayStart = getLocalDayStartTimestamp()
    const totalQuota = sumQuota(usage.map((row) => row.quota))
    const totalRequests = usage.reduce((sum, row) => sum + row.count, 0)
    stats.value = {
      quota: String(auth.user?.quota ?? 0),
      used_quota: String(auth.user?.used_quota ?? 0),
      today_quota: usage
        .filter((row) => row.created_at >= todayStart)
        .reduce((sum, row) => addQuota(sum, row.quota), '0'),
      month_quota: sumQuota(monthUsage.map((row) => row.quota)),
      today_tokens: usage
        .filter((row) => row.created_at >= todayStart)
        .reduce((sum, row) => sum + row.token_used, 0),
      today_requests: usage
        .filter((row) => row.created_at >= todayStart)
        .reduce((sum, row) => sum + row.count, 0),
      total_requests: totalRequests,
    }

    const modelRows = new Map<string, ModelShare>()
    const dateKeys = recentLocalDateKeys()
    const dayRows = new Map<string, FlowPoint>(
      dateKeys.map((date) => [date, { date, consume: '0', requests: 0 }])
    )
    for (const row of usage) {
      const current = modelRows.get(row.model_name) ?? {
        model: row.model_name,
        ratio: 0,
        quota: '0',
        requests: 0,
        tokens: 0,
      }
      current.quota = addQuota(current.quota, row.quota)
      current.requests += row.count
      current.tokens += row.token_used
      modelRows.set(row.model_name, current)

      const date = localDateKey(new Date(row.created_at * 1000))
      const day = dayRows.get(date)
      if (day) {
        day.consume = addQuota(day.consume, row.quota)
        day.requests += row.count
      }
    }
    share.value = [...modelRows.values()].map((row) => ({
      ...row,
      ratio: quotaRatio(row.quota, totalQuota),
    }))
    flow.value = dateKeys.map((date) => dayRows.get(date)!)
    loading.value = false
  }

  return {
    loading,
    stats,
    share,
    flow,
    error,
    load,
  }
}
