<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpsOverview, OpsSeriesField } from '@/api/opsMonitor'
import { useEChart } from '@/charts/useEChart'
import { lineMood } from '@/charts/themePreset'
import PrecisionTickDial from '@/components/console/dashboard/PrecisionTickDial.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  GAUGE_COLORS,
  bucketCounts,
  checkStatus,
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRate,
  seriesRange,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'
import OpsTrendChart from './OpsTrendChart.vue'

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

const gateway = computed(() => props.overview?.gateway ?? null)
/** The API reports per-minute rates; the page reads them per second. */
const perSecond = (value: number | null) => (value === null ? null : value / 60)
const successStatus = computed(() =>
  checkStatus(props.overview, 'success_rate')
)
const admissionStatus = computed(() => checkStatus(props.overview, 'admission'))

function boundLabel(bound: number) {
  const latency = formatLatencyMs(bound, locale.value)
  return `${latency.value}${latency.unit}`
}

const waitBars = computed(() => {
  const value = gateway.value
  if (!value) return []
  const counts = bucketCounts(value.admission.wait_duration_ms_buckets)
  const bounds = value.wait_bucket_bounds_ms
  return counts.map((count, i) => {
    const bound = bounds[i]
    const previous = bounds[i - 1]
    return {
      label:
        bound === null || bound === undefined
          ? t('gateway.waitOver', { bound: boundLabel(previous ?? 0) })
          : t('gateway.waitUpTo', { bound: boundLabel(bound) }),
      count,
    }
  })
})

const trends = computed(() => {
  const overview = props.overview
  if (!overview) return null
  const line = (field: OpsSeriesField, scale = 1) =>
    overview.series.points.map((point) => ({
      ts: point.ts,
      value: point[field] === null ? null : point[field]! / scale,
    }))
  return {
    range: seriesRange(overview),
    bucketSeconds: overview.series.bucket_seconds,
    load: [
      {
        name: t('gateway.inFlight'),
        color: 'accent' as const,
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
        color: 'signal' as const,
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

const queues = computed(() =>
  gateway.value
    ? [
        { key: 'userQueue', queue: gateway.value.user_queue },
        { key: 'channelQueue', queue: gateway.value.channel_queue },
      ]
    : []
)

const waitChart = ref<HTMLElement | null>(null)
useEChart(
  waitChart,
  (palette) => ({
    animation: false,
    grid: { top: 10, right: 6, bottom: 22, left: 34 },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: palette.surfaceSolid,
      borderColor: palette.borderSubtle,
      textStyle: { color: palette.textPrimary, fontSize: 12 },
    },
    xAxis: {
      type: 'category',
      data: waitBars.value.map((bar) => bar.label),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: palette.borderSubtle } },
      axisLabel: { color: palette.textTertiary, fontSize: 9, interval: 0 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitNumber: 3,
      axisLabel: { color: palette.textTertiary, fontSize: 10 },
      splitLine: { lineStyle: { type: 'solid', color: palette.chartGrid } },
    },
    series: [
      {
        name: t('gateway.waitCount'),
        type: 'bar',
        barMaxWidth: 18,
        data: waitBars.value.map((bar) => bar.count),
        itemStyle: {
          color: palette.signal,
          borderRadius: lineMood(palette).barRadius,
        },
      },
    ],
  }),
  [waitBars]
)
</script>

<template>
  <OpsCard
    :title="t('gateway.title')"
    :scope="t('scope.instant')"
    :loading="loading"
    :loaded="overview !== null"
    :error="error"
    :updated-at="updatedAt"
    :retryable="false"
    data-ops-section="gateway"
  >
    <template v-if="gateway && overview" #action>
      <StatusChip v-if="gateway.admission.draining" tone="warning">{{
        t('gateway.draining')
      }}</StatusChip>
    </template>
    <template v-if="gateway && overview && trends">
      <div class="ops-gateway-kpis">
        <dl class="ops-metric-grid ops-metric-grid--4">
          <OpsMetric
            :label="t('gateway.inFlight')"
            :value="formatCount(gateway.admission.in_flight, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('gateway.queued')"
            :value="formatCount(gateway.admission.queued, locale)"
            :hint="
              t('gateway.queueCapacity', {
                value: formatCount(gateway.admission.total_queue_size, locale),
              })
            "
            :status="admissionStatus"
            size="lg"
          />
          <OpsMetric
            :label="t('gateway.granted')"
            :value="formatRate(perSecond(gateway.granted_per_minute), locale)"
            :unit="t('gateway.perSecond')"
            size="lg"
          />
          <OpsMetric
            :label="t('gateway.rejected')"
            :value="formatRate(perSecond(gateway.rejected_per_minute), locale)"
            :unit="t('gateway.perSecond')"
            size="lg"
          />
        </dl>
        <div class="ops-success" data-ops-success-rate>
          <PrecisionTickDial
            :percent="gateway.success_rate"
            :color="GAUGE_COLORS[successStatus]"
            :size="52"
          />
          <dl>
            <OpsMetric
              :label="t('gateway.successRate')"
              v-bind="formatPercent(gateway.success_rate, locale)"
              :status="successStatus"
              :hint="
                gateway.success_rate === null
                  ? t('gateway.successRateHint')
                  : ''
              "
              size="lg"
            />
          </dl>
        </div>
      </div>
      <p class="ops-footnote">
        {{ t('gateway.rateHint') }} · {{ t('gateway.rejected') }}:
        {{ t('gateway.rejectedHint') }}
      </p>
      <div class="ops-trend-pair">
        <div class="ops-trend">
          <p class="ops-trend-title">
            {{ t('gateway.trendLoad')
            }}<span class="ops-scope">{{
              t('scope.window', {
                window: t(`window.${overview.series.window}`),
              })
            }}</span>
          </p>
          <OpsTrendChart
            :label="t('gateway.trendLoad')"
            :series="trends.load"
            :bucket-seconds="trends.bucketSeconds"
            :range="trends.range"
            :height="120"
          />
        </div>
        <div class="ops-trend">
          <p class="ops-trend-title">
            {{ t('gateway.trendRate')
            }}<span class="ops-scope">{{
              t('scope.window', {
                window: t(`window.${overview.series.window}`),
              })
            }}</span>
          </p>
          <OpsTrendChart
            :label="t('gateway.trendRate')"
            :series="trends.rate"
            :bucket-seconds="trends.bucketSeconds"
            :range="trends.range"
            :unit="t('gateway.perSecond')"
            :height="120"
          />
        </div>
      </div>
      <div class="ops-trend-pair">
        <figure class="ops-trend">
          <figcaption class="ops-trend-title">
            {{ t('gateway.waitDistribution')
            }}<span class="ops-scope" :title="t('scope.processHint')">{{
              t('scope.process')
            }}</span>
          </figcaption>
          <div ref="waitChart" class="ops-wait-chart" aria-hidden="true" />
          <ul class="sr-only">
            <li v-for="bar in waitBars" :key="bar.label">
              {{ bar.label }}: {{ bar.count }}
            </li>
          </ul>
          <p class="ops-footnote">{{ t('gateway.waitHint') }}</p>
        </figure>
        <div class="ops-trend">
          <p class="ops-trend-title">
            {{ t('gateway.retryBudget')
            }}<span class="ops-scope" :title="t('scope.processHint')">{{
              t('scope.process')
            }}</span>
            <StatusChip v-if="!gateway.retry_budget.enabled">{{
              t('gateway.retryDisabled')
            }}</StatusChip>
          </p>
          <dl class="ops-metric-grid ops-metric-grid--3">
            <OpsMetric
              :label="t('gateway.retryAvailable')"
              :value="
                formatRate(
                  gateway.retry_budget.available_hundredths / 100,
                  locale
                )
              "
              :hint="t('scope.instant')"
              size="sm"
            />
            <OpsMetric
              :label="t('gateway.retryReserved')"
              :value="formatCount(gateway.retry_budget.reserved, locale)"
              :hint="t('scope.instant')"
              size="sm"
            />
            <OpsMetric
              :label="t('gateway.firstSends')"
              :value="
                formatCount(gateway.retry_budget.first_sends_total, locale)
              "
              size="sm"
            />
            <OpsMetric
              :label="t('gateway.retrySends')"
              :value="
                formatCount(gateway.retry_budget.retry_sends_total, locale)
              "
              size="sm"
            />
            <OpsMetric
              :label="t('gateway.retryRejected')"
              :value="formatCount(gateway.retry_budget.rejected_total, locale)"
              size="sm"
            />
          </dl>
          <table class="ops-queue-table">
            <thead>
              <tr>
                <th scope="col">
                  <span class="sr-only">{{ t('gateway.queued') }}</span>
                </th>
                <th scope="col">{{ t('gateway.queueActive') }}</th>
                <th scope="col">{{ t('gateway.queueEntered') }}</th>
                <th scope="col">{{ t('gateway.queueCompleted') }}</th>
                <th scope="col">{{ t('gateway.queueFailed') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in queues" :key="row.key">
                <th scope="row">{{ t(`gateway.${row.key}`) }}</th>
                <td>{{ formatCount(row.queue.active, locale) }}</td>
                <td>{{ formatCount(row.queue.entered_total, locale) }}</td>
                <td>{{ formatCount(row.queue.completed_total, locale) }}</td>
                <td>{{ formatCount(row.queue.failed_total, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <details class="ops-details">
        <summary class="focus-ring">
          {{ t('gateway.counters')
          }}<span class="ops-scope" :title="t('scope.processHint')">{{
            t('scope.process')
          }}</span>
        </summary>
        <dl class="ops-metric-grid ops-metric-grid--3">
          <OpsMetric
            v-for="[key, value] in [
              ['grantedTotal', gateway.admission.granted_total],
              ['releasedTotal', gateway.admission.released_total],
              ['queueFull', gateway.admission.queue_full_total],
              ['totalQueueFull', gateway.admission.total_queue_full_total],
              ['waitTimeout', gateway.admission.wait_timeout_total],
              ['canceled', gateway.admission.canceled_total],
            ] as const"
            :key="key"
            :label="t(`gateway.${key}`)"
            :value="formatCount(value, locale)"
            size="sm"
          />
          <OpsMetric
            :label="t('gateway.pools')"
            :value="formatCount(gateway.admission.pools, locale)"
            :hint="t('scope.instant')"
            size="sm"
          />
        </dl>
      </details>
    </template>
  </OpsCard>
</template>
