import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUsageSummary } from '@/api/dashboard'
import { usageDistributionRange } from '@/utils/usageDistribution'
import { useLatestRequest } from './useLatestRequest'
import { useToast } from './useToast'
export interface UsageDistributionPoint {
  date: string
  requests: number
  consume: string
  tokens: number
}
export function useUsageDistribution() {
  const { t } = useI18n()
  const loading = ref(true),
    points = ref<UsageDistributionPoint[]>([]),
    error = ref(''),
    request = useLatestRequest(),
    toast = useToast()
  async function load() {
    loading.value = true
    error.value = ''
    const range = usageDistributionRange('year')
    const result = await request.run((signal) =>
      getUsageSummary(
        {
          range: 'custom',
          include_comparison: 'false',
          ...range,
        },
        signal
      )
    )
    if (result.stale) return
    loading.value = false
    if (result.ok && result.value.meta.available)
      points.value = result.value.distribution
    else {
      points.value = []
      error.value = result.ok
        ? t('dashboard.stats.unavailable')
        : result.error instanceof Error
          ? result.error.message
          : String(result.error)
      if (!result.ok) toast.error(error.value)
    }
  }
  return { loading, points, error, load }
}
