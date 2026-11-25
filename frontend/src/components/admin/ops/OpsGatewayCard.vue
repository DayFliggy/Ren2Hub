<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { seriesRange, type OpsOverview } from '@/api/opsMonitor'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { loadTone, shareTone } from '@/utils/loadTone'
import {
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRate,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsTrendChart from './OpsTrendChart.vue'

const props = withDefaults(
  defineProps<{
    overview: OpsOverview | null
    channelNames?: Record<number, string>
    loading: boolean
    error: string
    updatedAt: number
  }>(),
  { channelNames: () => ({}) }
)
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const { t: globalT } = useI18n({ useScope: 'global' })

const hasServerHistory = computed(() =>
  (props.overview?.series.points ?? []).some(
    (point) => point.users !== null || point.channels !== null
  )
)

const historyTrend = computed(() => {
  const overview = props.overview
  if (!overview || !hasServerHistory.value) return null
  return {
    range: seriesRange(overview),
    bucketSeconds: overview.series.bucket_seconds,
    window: t('scope.window', {
      window: t(`window.${overview.series.window}`),
    }),
    series: [
      {
        name: t('native.userInFlight'),
        color: 'signal' as const,
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value: point.users ?? null,
        })),
      },
      {
        name: t('native.channelInFlight'),
        color: 'accent' as const,
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value: point.channels ?? null,
        })),
      },
    ],
  }
})

const counters = computed(() => [
  {
    key: 'users' as const,
    title: t('native.userLimits'),
    rows: [...(props.overview?.gateway?.users ?? [])].sort(
      (a, b) => b.in_flight - a.in_flight || a.id - b.id
    ),
  },
  {
    key: 'channels' as const,
    title: t('native.channelLimits'),
    rows: [...(props.overview?.gateway?.channels ?? [])].sort(
      (a, b) => b.in_flight - a.in_flight || a.id - b.id
    ),
  },
])

function rowDisplayName(
  kind: 'users' | 'channels',
  row: { id: number; name?: string }
): string {
  const resolved =
    row.name || (kind === 'channels' ? props.channelNames[row.id] : undefined)
  return resolved ? `${resolved} (#${row.id})` : `#${row.id}`
}

function rowSharePercent(row: {
  in_flight: number
  limit: number
}): number | null {
  if (row.limit <= 0) return null
  return Math.min(100, Math.round((row.in_flight / row.limit) * 100))
}

function rowBarColor(row: { in_flight: number; limit: number }): string {
  const pct = rowSharePercent(row)
  if (pct === null) return 'var(--signal)'
  if (pct >= 90) return loadTone('danger').fill
  if (pct >= 75) return loadTone('warning').fill
  return shareTone(pct).fill
}

const routeQuality = computed(() => props.overview?.gateway?.quality ?? [])
const routeCache = computed(() => props.overview?.gateway?.cache ?? null)
const hasRoutingMatrix = computed(
  () =>
    routeQuality.value.length > 0 ||
    (routeCache.value?.bands.length ?? 0) > 0 ||
    (routeCache.value?.free_requests ?? 0) > 0
)

function bandChipTone(band: number): 'success' | 'info' | 'warning' | 'danger' {
  if (band <= 1) return 'success'
  if (band === 2) return 'info'
  if (band === 3) return 'warning'
  return 'danger'
}

function routeReasonLabel(reason: string): string {
  const key = `channels.qualityReasons.${reason}`
  const translated = globalT(key)
  return translated === key ? reason : translated
}

function channelLabel(id: number): string {
  return props.channelNames[id]
    ? `${props.channelNames[id]} (#${id})`
    : `#${id}`
}
</script>

<template>
  <OpsCard
    :title="t('gateway.title')"
    :scope="t('scope.instant')"
    :loading="loading"
    :loaded="overview?.gateway != null"
    :error="error"
    :updated-at="updatedAt"
    :retryable="false"
    data-ops-section="gateway"
  >
    <div v-if="overview?.gateway" class="ops-gateway-layout">
      <div
        v-if="historyTrend"
        class="ops-trend ops-gateway-history"
        :title="t('gatewayLive.serverHistoryNote')"
      >
        <p class="ops-trend-title">
          {{ t('gatewayLive.historyTitle')
          }}<span class="ops-scope">{{ historyTrend.window }}</span>
        </p>
        <OpsTrendChart
          :label="t('gatewayLive.historyTitle')"
          :series="historyTrend.series"
          :bucket-seconds="historyTrend.bucketSeconds"
          :range="historyTrend.range"
          :height="112"
        />
      </div>

      <div class="ops-gateway-split">
        <div class="ops-gateway-concurrency">
          <div
            v-for="counter in counters"
            :key="counter.key"
            class="ops-trend"
            :data-ops-concurrency="counter.key"
          >
            <p class="ops-trend-title">
              {{ counter.title
              }}<span class="ops-scope">{{
                t('gatewayLive.totalActive', {
                  count: formatCount(
                    counter.rows.reduce((sum, r) => sum + r.in_flight, 0),
                    locale
                  ),
                })
              }}</span>
            </p>
            <p v-if="!counter.rows.length" class="ops-empty">
              {{ t('native.noOccupancy') }}
            </p>
            <dl
              v-else
              class="ops-counter-list ops-counter-list--scroll subtle-scroll"
              tabindex="0"
              :aria-label="counter.title"
            >
              <div
                v-for="row in counter.rows"
                :key="row.id"
                class="ops-counter-row"
              >
                <div class="ops-counter-row-top">
                  <dt :title="rowDisplayName(counter.key, row)">
                    {{ rowDisplayName(counter.key, row) }}
                  </dt>
                  <dd class="font-mono">
                    {{ formatCount(row.in_flight, locale) }} /
                    {{
                      row.limit === 0
                        ? t('storage.unlimited')
                        : formatCount(row.limit, locale)
                    }}
                  </dd>
                </div>
                <div
                  v-if="row.limit > 0"
                  class="ops-capacity-bar"
                  aria-hidden="true"
                >
                  <span
                    :style="{
                      width: `${rowSharePercent(row)}%`,
                      background: rowBarColor(row),
                    }"
                  />
                </div>
              </div>
            </dl>
          </div>
        </div>

        <div class="ops-trend ops-gateway-routing" data-ops-routing-matrix>
          <p class="ops-trend-title">
            {{ t('gatewayLive.routingTitle') }}
            <span
              v-if="routeCache && routeCache.free_requests > 0"
              class="ops-scope"
              >{{
                t('native.routeExploration', {
                  count: routeCache.explorations,
                  total: routeCache.free_requests,
                  rate: formatPercent(
                    (100 * routeCache.explorations) / routeCache.free_requests,
                    locale
                  ).value,
                })
              }}</span
            >
            <span
              v-if="routeCache?.cells != null && routeCache.cell_limit != null"
              class="ops-scope"
              >{{
                t('gatewayLive.cacheCells', {
                  cells: formatCount(routeCache.cells, locale),
                  limit: formatCount(routeCache.cell_limit, locale),
                })
              }}</span
            >
          </p>

          <p v-if="!hasRoutingMatrix" class="ops-empty">
            {{ t('gatewayLive.noRouteQuality') }}
          </p>

          <ul
            v-else
            class="ops-cooling ops-routing-list subtle-scroll"
            tabindex="0"
            :aria-label="t('gatewayLive.routingTitle')"
          >
            <li
              v-for="row in routeQuality"
              :key="`q:${row.channel_id}:${row.model}:${row.operation}:${row.stream}`"
            >
              <div class="ops-cooling-head">
                <strong
                  :title="`${channelLabel(row.channel_id)} · ${row.model}`"
                  >{{ channelLabel(row.channel_id) }} · {{ row.model }}</strong
                >
                <StatusChip :tone="bandChipTone(row.band)">{{
                  t('gatewayLive.bandLabel', { band: row.band })
                }}</StatusChip>
              </div>
              <p class="ops-cooling-meta">
                <span>{{ routeReasonLabel(row.reason) }}</span>
                <span>{{ t('native.sampleCoverage', row) }}</span>
                <span v-if="row.latency_ms != null" class="font-mono"
                  >{{ formatLatencyMs(row.latency_ms, locale).value
                  }}{{ formatLatencyMs(row.latency_ms, locale).unit }}</span
                >
                <span v-if="row.tokens_per_second != null" class="font-mono"
                  >{{ formatRate(row.tokens_per_second, locale) }}
                  {{ t('quality.tps') }}</span
                >
              </p>
            </li>
            <li
              v-for="row in routeCache?.bands"
              :key="`c:${row.channel_id}:${row.model}:${row.mode}:${row.operation}:${row.stream}`"
            >
              <div class="ops-cooling-head">
                <strong
                  :title="`${channelLabel(row.channel_id)} · ${row.model}`"
                  >{{ channelLabel(row.channel_id) }} · {{ row.model }}</strong
                >
                <StatusChip
                  :tone="row.known ? bandChipTone(row.band) : 'neutral'"
                  >{{
                    row.known
                      ? t('gatewayLive.bandLabel', { band: row.band })
                      : t('native.cacheUnknown')
                  }}</StatusChip
                >
              </div>
              <p class="ops-cooling-meta">
                <code>{{ row.mode }}</code>
                <span>{{
                  t('native.cacheCoverage', {
                    rate: formatPercent(
                      row.samples > 0 ? row.token_rate * 100 : null,
                      locale
                    ).value,
                    coverage: formatPercent(row.coverage * 100, locale).value,
                    samples: row.samples,
                  })
                }}</span>
              </p>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </OpsCard>
</template>
