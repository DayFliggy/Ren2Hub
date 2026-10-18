import { ref } from 'vue'
import { getUsageSummary } from '@/api/dashboard'
import { useLatestRequest } from './useLatestRequest'
import { useToast } from './useToast'
export interface UsageDistributionPoint {
  date: string
  requests: number
  consume: string
  tokens: number
}
export function useUsageDistribution() {
  const loading = ref(true),
    points = ref<UsageDistributionPoint[]>([]),
    request = useLatestRequest(),
    toast = useToast()
  async function load() {
    loading.value = true
    const today = new Date()
    const end = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
    const result = await request.run(
      async (signal) =>
        (
          await getUsageSummary(
            {
              range: 'custom',
              start: `${today.getFullYear()}-01-01`,
              end,
            },
            signal
          )
        ).distribution
    )
    if (result.stale) return
    loading.value = false
    if (result.ok) points.value = result.value
    else {
      points.value = []
      toast.error(
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
      )
    }
  }
  return { loading, points, load }
}
