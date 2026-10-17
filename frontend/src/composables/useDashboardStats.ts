import { ref } from 'vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { fetchSelfUsage, type FlowPoint } from '@/composables/useDashboard'
import type { UsageRow } from '@/api/liveContracts'
import { addQuota, quotaRatio, sumQuota } from '@/utils/quota'

export type StatsRange = 'today' | '7d' | '30d' | 'custom'
export interface StatsKpi {
  totalTokens: number
  totalQuota: string
  totalRequests: number
}
export interface StatsModelRow {
  model: string
  tokens: number
  quota: string
  requests: number
  share: number
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
  comparison: StatsComparison
  models: StatsModelRow[]
  hourly: HourlyPoint[]
  flow: FlowPoint[]
}
export function aggregateUsage(
  rows: UsageRow[],
  previous: UsageRow[] = []
): StatsPeriod {
  const totalQuota = sumQuota(rows.map((row) => row.quota))
  const totalRequests = rows.reduce((sum, row) => sum + row.count, 0)
  const previousQuota = sumQuota(previous.map((row) => row.quota))
  const previousRequests = previous.reduce((sum, row) => sum + row.count, 0)
  const models = new Map<string, StatsModelRow>()
  const dates = new Map<string, FlowPoint>()
  const hourly = Array.from({ length: 24 }, (_, hour) => ({
    hour: String(hour).padStart(2, '0'),
    requests: 0,
  }))
  for (const row of rows) {
    const model = models.get(row.model_name) ?? {
      model: row.model_name,
      tokens: 0,
      quota: '0',
      requests: 0,
      share: 0,
    }
    model.tokens += row.token_used
    model.quota = addQuota(model.quota, row.quota)
    model.requests += row.count
    models.set(row.model_name, model)
    const date = new Date(row.created_at * 1000)
    const key = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
    const day = dates.get(key) ?? { date: key, consume: '0', requests: 0 }
    day.consume = addQuota(day.consume, row.quota)
    day.requests += row.count
    dates.set(key, day)
    hourly[date.getHours()]!.requests += row.count
  }
  return {
    kpi: {
      totalQuota,
      totalRequests,
      totalTokens: rows.reduce((sum, row) => sum + row.token_used, 0),
    },
    comparison: {
      quotaDelta:
        BigInt(previousQuota) > 0n
          ? (quotaRatio(totalQuota, previousQuota) - 1) * 100
          : null,
      requestsDelta: previousRequests
        ? (totalRequests / previousRequests - 1) * 100
        : null,
    },
    models: [...models.values()].map((row) => ({
      ...row,
      share: quotaRatio(row.quota, totalQuota),
    })),
    hourly,
    flow: [...dates.values()].sort((a, b) => a.date.localeCompare(b.date)),
  }
}
export function useDashboardStats() {
  const loading = ref(false),
    range = ref<StatsRange>('30d'),
    customStart = ref(''),
    customEnd = ref(''),
    data = ref<StatsPeriod | null>(null),
    error = ref<string | null>(null)
  const request = useLatestRequest()
  async function load() {
    loading.value = true
    error.value = null
    const now = new Date()
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    if (range.value === '7d') start.setDate(start.getDate() - 6)
    if (range.value === '30d') start.setDate(start.getDate() - 29)
    const from =
      range.value === 'custom'
        ? new Date(`${customStart.value}T00:00:00`).getTime() / 1000
        : start.getTime() / 1000
    const to =
      range.value === 'custom'
        ? new Date(`${customEnd.value}T23:59:59`).getTime() / 1000
        : Math.floor(now.getTime() / 1000)
    if (!Number.isFinite(from) || !Number.isFinite(to) || from > to) {
      loading.value = false
      data.value = null
      return
    }
    const result = await request.run(async (signal) => {
      const [current, previous] = await Promise.all([
        fetchSelfUsage(from, to, signal),
        fetchSelfUsage(from - (to - from + 1), from - 1, signal),
      ])
      return aggregateUsage(current, previous)
    })
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      error.value = String(
        result.error instanceof Error ? result.error.message : result.error
      )
      return
    }
    data.value = result.value
  }
  return { loading, range, customStart, customEnd, data, error, load }
}
