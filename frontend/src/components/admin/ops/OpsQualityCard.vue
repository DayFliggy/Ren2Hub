<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PerfModelSummaryContract } from '@/api/liveContracts'
import { getModelPerformance } from '@/api/modelMarket'
import {
  OPS_QUALITY_HOURS,
  type OpsQualityHours,
  type PerformanceQuality,
  type RoutingSnapshot,
} from '@/api/opsMonitor'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
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
import OpsTrendChart, { type OpsTrendSeries } from './OpsTrendChart.vue'

const HEALTH_TONES: HealthTone[] = ['good', 'degraded', 'poor', 'idle']

const props = withDefaults(
  defineProps<{
    quality: PerformanceQuality | null
    snapshot: RoutingSnapshot | null
    hours?: OpsQualityHours
    loading: boolean
    error: string
    updatedAt: number
  }>(),
  { hours: 24 }
)
const emit = defineEmits<{
  retry: []
  'update:hours': [hours: OpsQualityHours]
}>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const selectedHours = computed({
  get: () => String(props.hours),
  set: (value: string) => {
    const parsed = Number(value) as OpsQualityHours
    if (OPS_QUALITY_HOURS.includes(parsed)) emit('update:hours', parsed)
  },
})

const hourOptions = computed(() => [
  { value: '6', label: t('qualityTabs.h6') },
  { value: '24', label: t('qualityTabs.h24') },
  { value: '72', label: t('qualityTabs.h72') },
])

const activeTab = ref<'observations' | 'models'>('observations')
const tabOptions = computed(() => [
  { value: 'observations', label: t('qualityTabs.observations') },
  { value: 'models', label: t('qualityTabs.models') },
])

const modelPerf = ref<PerfModelSummaryContract[] | null>(null)
const modelPerfLoading = ref(false)
const modelPerfError = ref('')
let modelPerfController: AbortController | null = null

async function loadModelLeaderboard() {
  modelPerfController?.abort()
  const controller = new AbortController()
  modelPerfController = controller
  modelPerfLoading.value = true
  modelPerfError.value = ''
  try {
    const data = await getModelPerformance(controller.signal)
    if (modelPerfController === controller) {
      modelPerf.value = data
    }
  } catch (cause) {
    if (modelPerfController === controller && !controller.signal.aborted) {
      modelPerfError.value =
        cause instanceof Error ? cause.message : String(cause)
    }
  } finally {
    if (modelPerfController === controller) {
      modelPerfLoading.value = false
    }
  }
}

watch(activeTab, (tab) => {
  if (tab === 'models' && !modelPerf.value && !modelPerfLoading.value) {
    void loadModelLeaderboard()
  }
})

onBeforeUnmount(() => {
  modelPerfController?.abort()
  modelPerfController = null
})

const summary = computed(() => props.quality?.summary ?? null)
const summaryRateStatus = computed(() => {
  const rate = summary.value?.success_rate
  if (rate == null) return null
  if (rate < 95) return 'critical' as const
  if (rate < 99) return 'warning' as const
  return null
})

const hourClock = computed(
  () =>
    new Intl.DateTimeFormat(locale.value, {
      month: 'numeric',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
)

const denseSeries = computed(() => {
  const raw = props.quality?.series ?? []
  const step = props.quality?.bucket_seconds ?? 3600
  if (raw.length < 2 || step <= 0) return raw
  const byTs = new Map(raw.map((bucket) => [bucket.ts, bucket]))
  const start = raw[0]!.ts
  const end = raw.at(-1)!.ts
  const spanCount = Math.floor((end - start) / step) + 1
  if (spanCount <= raw.length || spanCount > 720) return raw
  const filled = []
  for (let ts = start; ts <= end; ts += step) {
    filled.push(
      byTs.get(ts) ?? {
        ts,
        request_count: 0,
        success_count: 0,
        success_rate: null,
        avg_latency_ms: null,
        avg_tps: null,
      }
    )
  }
  return filled
})

const cells = computed(() =>
  denseSeries.value.map((bucket) => {
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
  return t('quality.stripSummary', { hours: props.hours, ...counts })
})

const hasLatencySeries = computed(() =>
  (props.quality?.series ?? []).some((bucket) => bucket.avg_latency_ms !== null)
)

const trend = computed(() => {
  const quality = props.quality
  if (!quality?.series.length) return null
  // Bars stand for whole hours; half a bucket of padding aligns each bar with
  // the health strip cell directly above it.
  const half = quality.bucket_seconds / 2
  const series: OpsTrendSeries[] = [
    {
      name: t('quality.requests'),
      kind: 'bar',
      color: 'signal',
      yAxisIndex: 0,
      points: quality.series.map((bucket) => ({
        ts: bucket.ts,
        value: bucket.request_count,
      })),
    },
  ]
  if (hasLatencySeries.value) {
    series.push({
      name: t('qualityTabs.avgLatencyLine'),
      kind: 'line',
      color: 'accent',
      yAxisIndex: 1,
      unit: 'ms',
      points: quality.series.map((bucket) => ({
        ts: bucket.ts,
        value: bucket.avg_latency_ms,
      })),
    })
  }
  return {
    range: [quality.series[0]!.ts - half, quality.series.at(-1)!.ts + half] as [
      number,
      number,
    ],
    bucketSeconds: quality.bucket_seconds,
    series,
  }
})

const observations = computed(() =>
  [...(props.snapshot?.observations ?? [])]
    .sort((a, b) => b.samples - a.samples)
    .slice(0, 6)
)

const peak = computed(() =>
  Math.max(1, ...observations.value.map((row) => row.samples))
)

const topPerfModels = computed(() => (modelPerf.value ?? []).slice(0, 8))

function errorRateToneClass(known: boolean, errorRate: number): string {
  if (!known) return ''
  if (errorRate >= 0.2) return 'ops-cell--danger'
  if (errorRate >= 0.05) return 'ops-cell--warning'
  return ''
}

function latencyToneClass(known: boolean, latencyMs: number): string {
  if (!known) return ''
  if (latencyMs >= 10_000) return 'ops-cell--danger'
  if (latencyMs >= 3000) return 'ops-cell--warning'
  return ''
}

function successRateToneClass(rate: number): string {
  if (rate < 95) return 'ops-cell--danger'
  if (rate < 99) return 'ops-cell--warning'
  return 'ops-cell--ok'
}
</script>

<template>
  <OpsCard
    :title="t('quality.title')"
    :scope="t('scope.hours', { hours })"
    :scope-hint="t('native.qualityScope')"
    :loading="loading"
    :loaded="quality !== null || snapshot !== null"
    :error="error"
    :updated-at="updatedAt"
    data-ops-section="quality"
    @retry="emit('retry')"
  >
    <template v-if="quality?.enabled" #action>
      <div data-ops-quality-window>
        <SegmentedToggle
          v-model="selectedHours"
          :options="hourOptions"
          :label="t('qualityTabs.windowLabel')"
          size="sm"
        />
      </div>
    </template>

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
            :status="summaryRateStatus"
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
        <div v-if="cells.length" class="ops-trend">
          <p class="ops-trend-title">
            {{ t('quality.strip')
            }}<span class="ops-scope">{{ t('scope.perHour') }}</span>
          </p>
          <div
            class="ops-strip-wrapper"
            :class="{ 'ops-strip-wrapper--dual': hasLatencySeries }"
          >
            <div
              class="ops-strip"
              :class="{ 'ops-strip--dense': cells.length > 36 }"
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
            :secondary-axis="hasLatencySeries ? { unit: 'ms' } : null"
            :grid-left="40"
            :grid-right="hasLatencySeries ? 44 : 8"
            :height="124"
          />
        </div>
      </template>
    </template>

    <template v-if="snapshot">
      <div class="ops-trend ops-quality-bottom">
        <div class="ops-subbar">
          <p
            class="ops-trend-title"
            :aria-description="t('native.observationNote')"
            :title="t('native.observationNote')"
          >
            {{
              activeTab === 'observations'
                ? t('native.topObservations')
                : t('qualityTabs.models')
            }}
          </p>
          <SegmentedToggle
            v-model="activeTab"
            :options="tabOptions"
            :label="t('native.topObservations')"
            size="sm"
          />
        </div>

        <template v-if="activeTab === 'observations'">
          <p v-if="!observations.length" class="ops-empty">
            {{ t('native.noObservations') }}
          </p>
          <div
            v-else
            class="ops-table-scroll subtle-scroll"
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
                  v-for="model in observations"
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
                  <td class="font-mono">
                    {{ formatCount(model.samples, locale) }}
                  </td>
                  <td
                    class="font-mono"
                    :class="errorRateToneClass(model.known, model.error_rate)"
                  >
                    {{
                      formatPercent(
                        model.known ? model.error_rate * 100 : null,
                        locale
                      ).value
                    }}<span v-if="model.known" class="ops-unit">%</span>
                  </td>
                  <td
                    class="font-mono"
                    :class="
                      latencyToneClass(model.latency_known, model.latency_ms)
                    "
                  >
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
        </template>

        <template v-else>
          <p v-if="modelPerfLoading" class="ops-empty">{{ t('loading') }}</p>
          <p v-else-if="modelPerfError" class="ops-note">
            {{ modelPerfError }}
          </p>
          <p v-else-if="!topPerfModels.length" class="ops-empty">
            {{ t('qualityTabs.noModels') }}
          </p>
          <div
            v-else
            class="ops-table-scroll subtle-scroll"
            tabindex="0"
            :aria-label="t('qualityTabs.models')"
          >
            <table class="ops-model-table" data-ops-perf-models>
              <thead>
                <tr>
                  <th scope="col">{{ t('quality.model') }}</th>
                  <th scope="col">{{ t('quality.successRate') }}</th>
                  <th scope="col">{{ t('quality.avgLatency') }}</th>
                  <th scope="col">{{ t('quality.throughput') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in topPerfModels" :key="item.model_name">
                  <th scope="row">
                    <span class="ops-model-name" :title="item.model_name">{{
                      item.model_name
                    }}</span>
                  </th>
                  <td
                    class="font-mono"
                    :class="successRateToneClass(item.success_rate)"
                  >
                    {{ formatPercent(item.success_rate, locale).value
                    }}<span class="ops-unit">%</span>
                  </td>
                  <td class="font-mono">
                    {{ formatLatencyMs(item.avg_latency_ms, locale).value
                    }}<span class="ops-unit">{{
                      formatLatencyMs(item.avg_latency_ms, locale).unit
                    }}</span>
                  </td>
                  <td class="font-mono">
                    {{ formatRate(item.avg_tps, locale)
                    }}<span v-if="item.avg_tps > 0" class="ops-unit">{{
                      t('quality.tps')
                    }}</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </div>
    </template>
  </OpsCard>
</template>
