<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  QUALITY_HOURS,
  type PerformanceQuality,
  type RoutingSnapshot,
} from '@/api/opsMonitor'
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

const HEALTH_TONES: HealthTone[] = ['good', 'degraded', 'poor', 'idle']

const props = defineProps<{
  quality: PerformanceQuality | null
  snapshot: RoutingSnapshot | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const { t: globalT } = useI18n({ useScope: 'global' })
function routeBand(row: { band: number; reason: string }) {
  return t('native.routeBand', {
    band: row.band,
    reason: globalT('channels.qualityReasons.' + row.reason),
  })
}
const summary = computed(() => props.quality?.summary ?? null)
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
  (props.quality?.series ?? []).map((bucket) => {
    const tone = healthTone(bucket.request_count, bucket.success_rate)
    const rate = formatPercent(bucket.success_rate, locale.value)
    return {
      ts: bucket.ts,
      tone,
      title: t('quality.bucketTitle', {
        time: hourClock.value.format(bucket.ts * 1000),
        requests: formatCount(bucket.request_count, locale.value),
        rate: rate.value === '--' ? '--' : `${rate.value}%`,
      }),
    }
  })
)
const stripSummary = computed(() => {
  const counts = { good: 0, degraded: 0, poor: 0, idle: 0 }
  for (const cell of cells.value) counts[cell.tone]++
  return t('quality.stripSummary', { hours: QUALITY_HOURS, ...counts })
})
const trend = computed(() => {
  const quality = props.quality
  if (!quality?.series.length) return null
  // Bars stand for whole hours; half a bucket of padding keeps them aligned
  // with the health strip cells above.
  const half = quality.bucket_seconds / 2
  return {
    range: [quality.series[0]!.ts - half, quality.series.at(-1)!.ts + half] as [
      number,
      number,
    ],
    bucketSeconds: quality.bucket_seconds,
    series: [
      {
        name: t('quality.requests'),
        color: 'signal' as const,
        points: quality.series.map((bucket) => ({
          ts: bucket.ts,
          value: bucket.request_count,
        })),
      },
    ],
  }
})
const models = computed(() =>
  [...(props.snapshot?.observations ?? [])]
    .sort((a, b) => b.samples - a.samples)
    .slice(0, 6)
)
const samples = computed(
  () =>
    props.snapshot?.observations.reduce((sum, row) => sum + row.samples, 0) ??
    null
)
const known = computed(
  () => props.snapshot?.observations.filter((row) => row.known).length ?? null
)
const latencyKnown = computed(
  () =>
    props.snapshot?.observations.filter((row) => row.latency_known).length ??
    null
)
const peak = computed(() =>
  Math.max(1, ...models.value.map((row) => row.samples))
)
</script>

<template>
  <OpsCard
    :title="t('quality.title')"
    :scope="t('scope.hours', { hours: QUALITY_HOURS })"
    :scope-hint="t('native.qualityScope')"
    :loading="loading"
    :loaded="quality !== null || snapshot !== null"
    :error="error"
    :updated-at="updatedAt"
    data-ops-section="quality"
    @retry="emit('retry')"
  >
    <template v-if="quality">
      <p v-if="!quality.enabled" class="ops-empty" data-ops-quality-disabled>
        {{ t('quality.disabled') }}
      </p>
      <template v-else>
        <dl class="ops-metric-grid ops-metric-grid--4" data-ops-quality-summary>
          <OpsMetric
            :label="t('quality.requests')"
            :value="formatCount(summary?.requests ?? 0, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.successRate')"
            v-bind="formatPercent(summary?.success_rate ?? null, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.avgLatency')"
            v-bind="formatLatencyMs(summary?.avg_latency_ms ?? null, locale)"
            size="lg"
          />
          <OpsMetric
            :label="t('quality.throughput')"
            :value="formatRate(summary?.avg_tps ?? null, locale)"
            :unit="summary?.avg_tps == null ? '' : t('quality.tps')"
            :hint="summary?.avg_tps == null ? t('errors.unavailable') : ''"
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
        <p v-if="summary === null" class="ops-empty" data-ops-no-traffic>
          {{ t('quality.noTraffic') }}
        </p>
        <div v-else-if="trend" class="ops-trend">
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
      </template>
    </template>
    <template v-if="snapshot">
      <div
        v-if="snapshot.quality?.length || snapshot.cache?.bands.length"
        class="ops-trend"
      >
        <p class="ops-trend-title">{{ t('native.routeQuality') }}</p>
        <p
          v-for="row in snapshot.quality"
          :key="
            JSON.stringify([
              row.channel_id,
              row.model,
              row.operation,
              row.stream,
            ])
          "
          class="ops-footnote"
        >
          #{{ row.channel_id }} · {{ row.model }} · {{ routeBand(row) }} ·
          {{ t('native.sampleCoverage', row) }}
        </p>
        <p
          v-for="row in snapshot.cache?.bands"
          :key="
            JSON.stringify([
              row.channel_id,
              row.model,
              row.mode,
              row.operation,
              row.stream,
            ])
          "
          class="ops-footnote"
        >
          #{{ row.channel_id }} · {{ row.model }} · {{ row.mode }} ·
          {{
            t('native.cacheCoverage', {
              rate: formatPercent(
                row.samples > 0 ? row.token_rate * 100 : null,
                locale
              ).value,
              coverage: formatPercent(row.coverage * 100, locale).value,
              samples: row.samples,
            })
          }}
          ·
          {{ row.known ? routeBand(row) : t('native.cacheUnknown') }}
        </p>
        <p
          v-if="snapshot.cache && snapshot.cache.free_requests > 0"
          class="ops-footnote"
        >
          {{
            t('native.routeExploration', {
              count: snapshot.cache.explorations,
              total: snapshot.cache.free_requests,
              rate: formatPercent(
                (100 * snapshot.cache.explorations) /
                  snapshot.cache.free_requests,
                locale
              ).value,
            })
          }}
        </p>
      </div>
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="t('native.sampleCount')"
          :value="formatCount(samples, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('native.knownObservations')"
          :value="formatCount(known, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('native.latencyObservations')"
          :value="formatCount(latencyKnown, locale)"
          size="sm"
        />
      </dl>
      <div class="ops-trend">
        <p
          class="ops-trend-title"
          :aria-description="t('native.observationNote')"
          :title="t('native.observationNote')"
        >
          {{ t('native.topObservations') }}
        </p>
        <p v-if="!models.length" class="ops-empty">
          {{ t('native.noObservations') }}
        </p>
        <div
          v-else
          class="ops-table-scroll"
          tabindex="0"
          :aria-label="t('native.topObservations')"
        >
          <table class="ops-model-table" data-ops-top-models>
            <thead>
              <tr>
                <th scope="col">{{ t('quality.model') }}</th>
                <th scope="col">{{ t('native.sampleCount') }}</th>
                <th scope="col">{{ t('native.errorEWMA') }}</th>
                <th scope="col">{{ t('native.latencyEWMA') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="model in models"
                :key="`${model.channel_id}:${model.model}:${model.operation}:${model.stream}`"
              >
                <th scope="row">
                  <span class="ops-model-name" :title="model.model">{{
                    model.model || t('quality.unnamedModel')
                  }}</span>
                  <span class="ops-footnote"
                    >#{{ model.channel_id }} ·
                    {{
                      model.operation === null
                        ? t('native.unknownOperation')
                        : t('native.operation', { id: model.operation })
                    }}
                    ·
                    {{
                      model.stream
                        ? t('native.streaming')
                        : t('native.nonStreaming')
                    }}</span
                  >
                  <span class="ops-model-bar" aria-hidden="true"
                    ><span
                      :style="{ width: `${(model.samples / peak) * 100}%` }"
                  /></span>
                </th>
                <td>{{ formatCount(model.samples, locale) }}</td>
                <td>
                  {{
                    formatPercent(
                      model.known ? model.error_rate * 100 : null,
                      locale
                    ).value
                  }}<span v-if="model.known" class="ops-unit">%</span>
                </td>
                <td>
                  {{
                    formatLatencyMs(
                      model.latency_known ? model.latency_ms : null,
                      locale
                    ).value
                  }}<span class="ops-unit">{{
                    formatLatencyMs(
                      model.latency_known ? model.latency_ms : null,
                      locale
                    ).unit
                  }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </OpsCard>
</template>
