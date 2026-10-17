<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsMonitorApi, type OpsTraffic } from '@/api/opsMonitor'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  HEALTH_COLORS,
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRate,
  healthTone,
  type HealthTone,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'
import OpsTrendChart from './OpsTrendChart.vue'

const HOURS = 24
const INTERVAL_MS = 60_000
const HEALTH_TONES: HealthTone[] = ['good', 'degraded', 'poor', 'idle']

const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const feed = usePolledFeed(
  (signal) => opsMonitorApi.requests(HOURS, signal),
  () => INTERVAL_MS
)
const report = computed(() => feed.data.value)

/** Averages over zero requests are undefined, not zero. */
function averages(traffic: OpsTraffic) {
  const idle = traffic.requests === 0
  return {
    latency: formatLatencyMs(
      idle ? null : traffic.avg_latency_ms,
      locale.value
    ),
    tps: idle ? '--' : formatRate(traffic.avg_tps, locale.value),
  }
}

const hourClock = computed(
  () =>
    new Intl.DateTimeFormat(locale.value, {
      month: 'numeric',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
)
const cells = computed(() =>
  (report.value?.series ?? []).map((bucket) => {
    const tone = healthTone(bucket.requests, bucket.success_rate)
    const rate = formatPercent(bucket.success_rate, locale.value)
    return {
      ts: bucket.ts,
      tone,
      title: t('quality.bucketTitle', {
        time: hourClock.value.format(bucket.ts * 1000),
        requests: formatCount(bucket.requests, locale.value),
        rate: rate.value === '--' ? '--' : `${rate.value}%`,
      }),
    }
  })
)
const stripSummary = computed(() => {
  const counts = { good: 0, degraded: 0, poor: 0, idle: 0 }
  for (const cell of cells.value) counts[cell.tone]++
  return t('quality.stripSummary', {
    hours: report.value?.hours ?? HOURS,
    ...counts,
  })
})
const trend = computed(() => {
  const series = report.value?.series ?? []
  if (!series.length || !report.value) return null
  // Each hourly reading represents a bucket, so include half a bucket on both
  // sides. This keeps the bar centers aligned with the health strip cells.
  const halfBucket = report.value.bucket_seconds / 2
  return {
    range: [series[0]!.ts - halfBucket, series.at(-1)!.ts + halfBucket] as [
      number,
      number,
    ],
    bucketSeconds: report.value.bucket_seconds,
    series: [
      {
        name: t('quality.requests'),
        color: 'signal' as const,
        points: series.map((bucket) => ({
          ts: bucket.ts,
          value: bucket.requests,
        })),
      },
    ],
  }
})
const peak = computed(() =>
  Math.max(
    1,
    ...(report.value?.top_models ?? []).map((model) => model.requests)
  )
)
</script>

<template>
  <OpsCard
    :title="t('quality.title')"
    :scope="t('scope.hours', { hours: report?.hours ?? 24 })"
    :loading="feed.loading.value"
    :loaded="report !== null"
    :error="feed.error.value"
    :updated-at="feed.updatedAt.value"
    data-ops-section="quality"
    @retry="feed.refresh"
  >
    <template v-if="report">
      <p v-if="!report.enabled" class="ops-empty" data-ops-empty>
        {{ t('quality.disabled') }}
      </p>
      <template v-else>
        <dl class="ops-metric-grid ops-metric-grid--4">
          <OpsMetric
            :label="t('quality.requests')"
            :value="formatCount(report.summary.requests, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.successRate')"
            v-bind="formatPercent(report.summary.success_rate, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.avgLatency')"
            v-bind="averages(report.summary).latency"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.throughput')"
            :value="averages(report.summary).tps"
            :unit="t('quality.tps')"
            size="lg"
          />
        </dl>
        <div class="ops-trend">
          <p class="ops-trend-title">
            {{ t('quality.strip')
            }}<span class="ops-scope">{{ t('scope.perHour') }}</span>
          </p>
          <div
            class="ops-strip"
            role="img"
            :aria-label="stripSummary"
            data-ops-health-strip
          >
            <span
              v-for="cell in cells"
              :key="cell.ts"
              class="ops-strip-cell"
              :data-tone="cell.tone"
              :title="cell.title"
              :style="{ background: HEALTH_COLORS[cell.tone] }"
            />
          </div>
          <ul class="ops-legend" aria-hidden="true">
            <li v-for="tone in HEALTH_TONES" :key="tone">
              <span
                class="ops-dot"
                :style="{ background: HEALTH_COLORS[tone] }"
              />{{ t(`quality.health.${tone}`) }}
            </li>
          </ul>
        </div>
        <p
          v-if="report.summary.requests === 0"
          class="ops-empty"
          data-ops-empty
        >
          {{ t('quality.noTraffic') }}
        </p>
        <template v-else>
          <div v-if="trend" class="ops-trend">
            <p class="ops-trend-title">{{ t('quality.trend') }}</p>
            <OpsTrendChart
              kind="bar"
              :label="t('quality.trend')"
              :series="trend.series"
              :bucket-seconds="trend.bucketSeconds"
              :range="trend.range"
              :height="110"
            />
          </div>
          <div class="ops-trend">
            <p class="ops-trend-title">{{ t('quality.topModels') }}</p>
            <table class="ops-model-table" data-ops-top-models>
              <thead>
                <tr>
                  <th scope="col">{{ t('quality.model') }}</th>
                  <th scope="col">{{ t('quality.requests') }}</th>
                  <th scope="col">{{ t('quality.successRate') }}</th>
                  <th scope="col">{{ t('quality.avgLatency') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in report.top_models" :key="model.model_name">
                  <th scope="row">
                    <span class="ops-model-name">{{
                      model.model_name || t('quality.unnamedModel')
                    }}</span>
                    <span class="ops-model-bar" aria-hidden="true"
                      ><span
                        :style="{ width: `${(model.requests / peak) * 100}%` }"
                    /></span>
                  </th>
                  <td>{{ formatCount(model.requests, locale) }}</td>
                  <td>
                    {{ formatPercent(model.success_rate, locale).value
                    }}<span v-if="model.success_rate !== null" class="ops-unit"
                      >%</span
                    >
                  </td>
                  <td>
                    {{ averages(model).latency.value
                    }}<span class="ops-unit">{{
                      averages(model).latency.unit
                    }}</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </template>
    </template>
  </OpsCard>
</template>
