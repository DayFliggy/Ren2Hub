import { ref } from 'vue'
import { fetchSelfUsage } from './useDashboard'
import { useLatestRequest } from './useLatestRequest'
import { useToast } from './useToast'
import { addQuota } from '@/utils/quota'
export interface UsageDistributionPoint {
  date: string
  requests: number
  consume: string
  tokens: number
}
export function useUsageDistribution() {
  const loading = ref(true),
    points = ref<UsageDistributionPoint[]>([])
  const request = useLatestRequest(),
    toast = useToast()
  async function load() {
    loading.value = true
    const now = new Date(),
      start = new Date(now.getFullYear(), 0, 1)
    const result = await request.run((signal) =>
      fetchSelfUsage(
        start.getTime() / 1000,
        Math.floor(now.getTime() / 1000),
        signal
      )
    )
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      toast.error(
        String(
          result.error instanceof Error ? result.error.message : result.error
        )
      )
      return
    }
    const days = new Map<string, UsageDistributionPoint>()
    for (const row of result.value) {
      const d = new Date(row.created_at * 1000),
        date = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
      const day = days.get(date) ?? {
        date,
        requests: 0,
        consume: '0',
        tokens: 0,
      }
      day.requests += row.count
      day.consume = addQuota(day.consume, row.quota)
      day.tokens += row.token_used
      days.set(date, day)
    }
    points.value = [...days.values()].sort((a, b) =>
      a.date.localeCompare(b.date)
    )
  }
  return { loading, points, load }
}
