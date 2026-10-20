<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpsOverview, OpsSeriesField } from '@/api/opsMonitor'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  bucketCounts,
  formatCount,
  formatLatencyMs,
  seriesRange,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsTrendChart from './OpsTrendChart.vue'

/**
 * Admission load over the selected window plus the process-lifetime queue
 * picture. Current values live in the status header; this card holds history.
 */
const props = defineProps<{
  overview: OpsOverview | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const trends = computed(() => {
  const overview = props.overview
  if (!overview) return null
  // Per-minute rate fields are drawn per second, like the header tiles.
  const line = (field: OpsSeriesField, scale = 1) =>
    overview.series.points.map((point) => ({
      ts: point.ts,
      value: point[field] === null ? null : point[field]! / scale,
    }))
  return {
    range: seriesRange(overview),
    bucketSeconds: overview.series.bucket_seconds,
    window: t('scope.window', {
      window: t(`window.${overview.series.window}`),
    }),
    load: [
      {
        name: t('gateway.inFlight'),
        color: 'signal' as const,
        points: line('in_flight'),
      },
      {
        name: t('gateway.queued'),
        color: 'warning' as const,
        points: line('queued'),
      },
    ],
    rate: [
      {
        name: t('gateway.granted'),
        color: 'accent' as const,
        points: line('granted_per_minute', 60),
      },
      {
        name: t('gateway.rejected'),
        color: 'danger' as const,
        points: line('rejected_per_minute', 60),
      },
    ],
  }
})

/** The API sends cumulative bucket counts; bars show each bucket on its own. */
const waits = computed(() => {
  const gateway = props.overview?.gateway
  if (!gateway) return { bars: [], total: 0 }
  const counts = bucketCounts(gateway.admission.wait_duration_ms_buckets)
  const bounds = gateway.wait_bucket_bounds_ms
  const peak = Math.max(1, ...counts)
  const bars = counts.map((count, i) => {
    const bound = bounds[i] ?? null
    const reading = formatLatencyMs(bound ?? bounds[i - 1] ?? 0, locale.value)
    const edge = `${reading.value}${reading.unit}`
    const over = t('gateway.waitOver', { bound: edge })
    return {
      // Narrow bars show the upper bound alone; the full range is announced.
      label: bound === null ? over : t('gateway.waitUpTo', { bound: edge }),
      edge: bound === null ? over : edge,
      count: formatCount(count, locale.value),
      height: `${Math.round((count / peak) * 100)}%`,
    }
  })
  return { bars, total: counts.reduce((sum, count) => sum + count, 0) }
})

const counters = computed(() => {
  const gateway = props.overview?.gateway
  if (!gateway) return []
  const admission = gateway.admission
  return [
    { key: 'queueFull', value: admission.queue_full_total },
    { key: 'totalQueueFull', value: admission.total_queue_full_total },
    { key: 'waitTimeout', value: admission.wait_timeout_total },
    { key: 'canceled', value: admission.canceled_total },
    { key: 'retryRejected', value: gateway.retry_budget.rejected_total },
  ]
})
</script>

<template>
  <OpsCard
    :title="t('gateway.title')"
    :loading="loading"
    :loaded="overview !== null"
    :error="error"
    :updated-at="updatedAt"
    :retryable="false"
    data-ops-section="gateway"
  >
    <div v-if="trends" class="ops-gateway-grid">
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('gateway.trendLoad')
          }}<span class="ops-scope">{{ trends.window }}</span>
        </p>
        <OpsTrendChart
          :label="t('gateway.trendLoad')"
          :series="trends.load"
          :bucket-seconds="trends.bucketSeconds"
          :range="trends.range"
          :height="110"
        />
      </div>
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('gateway.trendRate')
          }}<span class="ops-scope">{{ trends.window }}</span>
        </p>
        <OpsTrendChart
          :label="t('gateway.trendRate')"
          :series="trends.rate"
          :bucket-seconds="trends.bucketSeconds"
          :range="trends.range"
          :unit="t('gateway.perSecond')"
          :height="110"
        />
      </div>
      <div class="ops-trend" data-ops-waits>
        <p class="ops-trend-title">
          {{ t('gateway.waitDistribution')
          }}<span class="ops-scope" :title="t('scope.processHint')">{{
            t('scope.process')
          }}</span>
        </p>
        <p v-if="!waits.total" class="ops-empty">{{ t('gateway.noWaits') }}</p>
        <ol
          v-else
          class="ops-histogram"
          :aria-label="t('gateway.waitDistribution')"
        >
          <li v-for="bar in waits.bars" :key="bar.label" :title="bar.label">
            <span class="ops-histogram-count">{{ bar.count }}</span>
            <span class="ops-histogram-track" aria-hidden="true"
              ><span :style="{ height: bar.height }"
            /></span>
            <span class="ops-histogram-label" aria-hidden="true">{{
              bar.edge
            }}</span>
            <span class="sr-only">{{ bar.label }}</span>
          </li>
        </ol>
        <p v-if="waits.total" class="ops-footnote">
          {{ t('gateway.waitHint') }}
        </p>
      </div>
      <div class="ops-trend" data-ops-counters>
        <p class="ops-trend-title">
          {{ t('gateway.counters')
          }}<span class="ops-scope" :title="t('scope.processHint')">{{
            t('scope.process')
          }}</span>
        </p>
        <dl class="ops-counter-list">
          <div v-for="item in counters" :key="item.key">
            <dt>{{ t(`gateway.${item.key}`) }}</dt>
            <dd>{{ formatCount(item.value, locale) }}</dd>
          </div>
        </dl>
      </div>
    </div>
  </OpsCard>
</template>
