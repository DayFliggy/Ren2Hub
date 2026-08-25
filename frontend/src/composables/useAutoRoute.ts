import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import {
  groupByLabGroup,
  scoreChannels,
  type ChannelRoutingMetrics,
  type RouteLabMatch,
  type ScoreBreakdown,
} from '@/utils/routeScore'
import {
  summarizeRouteHealth,
  type RouteHealthSummary,
} from '@/utils/routeHealth'

export interface RouteChannelRow extends ChannelRoutingMetrics {
  rank: number | null
  score: number | null
  breakdown: ScoreBreakdown | null
}

export interface RouteGroupList {
  groupSlug: string
  groupName: string
  labMatches: RouteLabMatch[]
  /** Compatibility alias for consumers that still read the old field. */
  vendor: string
  channels: RouteChannelRow[]
  activeCount: number
  monitor: RouteHealthSummary
}

export function buildRouteGroupList(
  rawChannels: ChannelRoutingMetrics[],
  nowTimestamp?: number
): RouteGroupList[] {
  const result: RouteGroupList[] = []
  groupByLabGroup(rawChannels).forEach((channels, groupSlug) => {
    const scored = scoreChannels(channels)
    const ranked: RouteChannelRow[] = scored.map((channel, index) => ({
      ...channel,
      rank: index + 1,
    }))
    const inactive: RouteChannelRow[] = channels
      .filter((channel) => channel.status !== 1)
      .map((channel) => ({
        ...channel,
        rank: null,
        score: null,
        breakdown: null,
      }))

    const labMatches = channels.reduce<RouteLabMatch[]>((matches, channel) => {
      for (const match of channel.lab_matches ?? []) {
        if (!matches.some((existing) => existing.slug === match.slug)) {
          matches.push(match)
        }
      }
      return matches
    }, [])
    const groupName =
      channels.find((channel) => channel.lab_group_name)?.lab_group_name ||
      channels[0]?.supplier ||
      'Unknown / Provider-specific'

    result.push({
      groupSlug,
      groupName,
      labMatches,
      vendor: groupName,
      channels: [...ranked, ...inactive],
      activeCount: ranked.length,
      monitor: summarizeRouteHealth(channels, nowTimestamp),
    })
  })
  return result.sort(
    (a, b) =>
      b.channels.length - a.channels.length ||
      a.groupName.localeCompare(b.groupName)
  )
}

/** Backward-compatible export for existing dashboard consumers. */
export const buildVendorRouteList = buildRouteGroupList

export function useAutoRoute() {
  const { t } = useI18n()
  const toast = useToast()

  const loading = ref(false)
  const lastUpdated = ref<Date | null>(null)
  const raw = ref<ChannelRoutingMetrics[]>([])
  const modelFilter = ref<string>('')
  const routeRequest = useLatestRequest()

  /**
   * The dashboard compares channels within each resolved lab group. Disabled
   * channels stay visible after ranked active rows so an unavailable group
   * never disappears from the monitoring surface.
   */
  const vendorList = computed<RouteGroupList[]>(() => {
    return buildRouteGroupList(raw.value)
  })

  async function load() {
    loading.value = true
    const result = await routeRequest.run((signal) =>
      api.get<ChannelRoutingMetrics[]>(
        '/api/next/admin/dashboard/routes',
        undefined,
        { signal }
      )
    )
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      toast.error(
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      )
      return
    }
    raw.value = result.value
    lastUpdated.value = new Date()
  }

  return { loading, lastUpdated, vendorList, modelFilter, load }
}
