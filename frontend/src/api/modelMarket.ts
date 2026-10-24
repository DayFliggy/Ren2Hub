import { api } from './client'
import { createApiClient } from './createClient'
import { publicHttpTransport } from './httpTransport'
import {
  parsePerfMetricsSummaryResult,
  parsePerfMetricsSummary,
} from './liveContracts'
import { getModelCatalog } from './modelCatalog'
export { parseModelCatalog as parseModelMarket } from './modelCatalog'
export function getModelMarket(signal?: AbortSignal) {
  return getModelCatalog(undefined, signal)
}

/** Window requested from /api/perf-metrics/summary and shown in the UI. */
export const MODEL_PERFORMANCE_HOURS = 24

/** Native per-model metrics aggregated across all channels. */
export async function getModelPerformance(signal?: AbortSignal) {
  return parsePerfMetricsSummary(
    await api.get(
      '/api/perf-metrics/summary',
      { hours: MODEL_PERFORMANCE_HOURS },
      { signal }
    )
  )
}

const publicPerformanceClient = createApiClient(publicHttpTransport)

export async function getPerformanceSummary(
  authenticated: boolean,
  signal?: AbortSignal
) {
  return parsePerfMetricsSummaryResult(
    await (authenticated ? api : publicPerformanceClient).get(
      '/api/perf-metrics/summary',
      { hours: MODEL_PERFORMANCE_HOURS },
      { signal }
    )
  )
}
