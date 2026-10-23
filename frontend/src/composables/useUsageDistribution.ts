import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUsageSummary, logRangeLimit } from '@/api/dashboard'
import { useAppStore } from '@/stores/app'
import { usageCoverageRange } from '@/utils/usageDistribution'
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
  const app = useAppStore()
  const loading = ref(true),
    points = ref<UsageDistributionPoint[]>([]),
    /** First queried date; the heatmap marks earlier days as not covered. */
    coveredFrom = ref(''),
    error = ref(''),
    unavailableReason = ref(''),
    request = useLatestRequest(),
    toast = useToast()
  async function load() {
    loading.value = true
    error.value = ''
    unavailableReason.value = ''
    const result = await request.run(async (signal) => {
      // A log limit below a year shortens the query instead of failing it.
      const range = usageCoverageRange(logRangeLimit(app.status))
      const summary = await getUsageSummary(
        { range: 'custom', ...range },
        false,
        signal
      )
      return { start: range.start, summary }
    })
    if (result.stale) return
    loading.value = false
    if (result.ok && result.value.summary.meta.available) {
      coveredFrom.value = result.value.start
      points.value = result.value.summary.distribution
    } else if (result.ok) {
      points.value = []
      unavailableReason.value = t('dashboard.stats.unavailable')
    } else {
      points.value = []
      error.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
      toast.error(error.value)
    }
  }
  return { loading, points, coveredFrom, error, unavailableReason, load }
}
