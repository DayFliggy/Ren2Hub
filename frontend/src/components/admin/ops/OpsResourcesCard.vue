<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  checkStatus,
  seriesRange,
  type OpsOverview,
  type OpsSeriesField,
} from '@/api/opsMonitor'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { verdictTone } from '@/utils/loadTone'
import {
  formatBytes,
  formatCount,
  formatLatencyMs,
  formatMbps,
  formatPercent,
  formatClock,
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

const joined = (value: { value: string; unit: string }) =>
  value.value === '--' ? '--' : `${value.value} ${value.unit}`

const gauges = computed(() => {
  const resources = props.overview?.resources
  if (!resources) return []
  const usage = (used: number | null, total: number | null) =>
    used === null || total === null
      ? ''
      : t('resources.usedOfTotal', {
          used: joined(formatBytes(used, locale.value)),
          total: joined(formatBytes(total, locale.value)),
        })
  return [
    {
      key: 'cpu' as const,
      percent: resources.cpu.percent,
      threshold: resources.cpu.threshold,
      detail: '',
    },
    {
      key: 'memory' as const,
      percent: resources.memory.percent,
      threshold: resources.memory.threshold,
      detail: usage(resources.memory.used_bytes, resources.memory.total_bytes),
    },
    {
      key: 'disk' as const,
      percent: resources.disk.percent,
      threshold: resources.disk.threshold,
      detail: [
        usage(resources.disk.used_bytes, resources.disk.total_bytes),
        resources.disk.path,
      ]
        .filter(Boolean)
        .join(' · '),
    },
  ].map((gauge) => {
    const status = checkStatus(props.overview, gauge.key)
    // The dashboard's colour language: a check turned off by threshold 0 keeps
    // its reading, unjudged; stale or unknown has no reading to colour.
    const tone = verdictTone(
      status === 'disabled' ? 'unchecked' : status,
      gauge.percent
    )
    return {
      ...gauge,
      scope:
        props.overview?.observation?.resources[gauge.key].scope ?? 'unknown',
      sampledAt:
        props.overview?.observation?.resources[gauge.key].sampled_at ?? null,
      status,
      tone,
      reading: formatPercent(gauge.percent, locale.value),
      thresholdLabel:
        gauge.threshold !== null && gauge.threshold > 0
          ? t('resources.threshold', { value: gauge.threshold })
          : gauge.threshold === null
            ? t('unavailable')
            : t('resources.thresholdOff'),
    }
  })
})

const trend = computed(() => {
  const overview = props.overview
  if (!overview) return null
  const line = (field: OpsSeriesField) =>
    overview.series.points.map((point) => ({
      ts: point.ts,
      value: point[field] ?? null,
    }))
  return {
    range: seriesRange(overview),
    bucketSeconds: overview.series.bucket_seconds,
    max: Math.max(
      100,
      ...overview.series.points.flatMap((point) => [
        point.cpu ?? 0,
        point.memory ?? 0,
        point.disk ?? 0,
      ])
    ),
    series: [
      // Lines take hues outside the load ramp, so history never reads as a
      // gauge verdict.
      {
        name: t('resources.cpu'),
        color: 'trendCpu' as const,
        points: line('cpu'),
      },
      {
        name: t('resources.memory'),
        color: 'trendMemory' as const,
        points: line('memory'),
      },
      {
        name: t('resources.disk'),
        color: 'trendDisk' as const,
        points: line('disk'),
      },
    ],
  }
})
const trafficTrend = computed(() => {
  const overview = props.overview
  if (!overview) return null
  return {
    range: seriesRange(overview),
    series: (
      [
        ['request_mbps', 'down', 'accent'],
        ['response_mbps', 'up', 'signal'],
      ] as const
    ).map(([field, label, color]) => ({
      name: t(`resources.${label}`),
      color,
      points: overview.series.points.map((point) => ({
        ts: point.ts,
        value: point[field] ?? null,
      })),
    })),
  }
})
</script>

<template>
  <OpsCard
    :title="t('resources.title')"
    :scope="t('scope.instant')"
    :scope-hint="t('native.resourceCoverage')"
    :loading="loading"
    :loaded="overview?.resources != null"
    :error="error"
    :updated-at="updatedAt"
    :retryable="false"
    data-ops-section="resources"
  >
    <template v-if="overview?.resources">
      <p v-if="overview.resources.monitor_enabled === false" class="ops-note">
        {{ t('resources.monitorOff') }}
      </p>
      <ul class="ops-gauges">
        <li
          v-for="gauge in gauges"
          :key="gauge.key"
          class="ops-gauge"
          :data-ops-gauge="gauge.key"
          :data-load-tier="gauge.tone.tier"
        >
          <MiniRing
            :percent="gauge.percent ?? 0"
            :color="gauge.tone.fill"
            :size="64"
            :stroke-width="5"
            :indeterminate="gauge.percent === null"
          >
            <span
              class="ops-gauge-value"
              :style="{
                color:
                  gauge.status === 'critical' ? gauge.tone.text : undefined,
              }"
              >{{ gauge.reading.value
              }}<span v-if="gauge.reading.unit" class="ops-unit">{{
                gauge.reading.unit
              }}</span></span
            >
          </MiniRing>
          <p class="ops-gauge-label">
            <span
              v-if="gauge.status === 'warning' || gauge.status === 'critical'"
              class="ops-dot"
              :style="{ background: gauge.tone.fill }"
              aria-hidden="true"
            />{{ t(`resources.${gauge.key}`)
            }}<span
              v-if="gauge.status === 'warning' || gauge.status === 'critical'"
              class="sr-only"
              >({{ t(`status.${gauge.status}`) }})</span
            >
          </p>
          <p
            v-if="gauge.percent === null"
            class="ops-gauge-detail"
            data-ops-unavailable
          >
            {{ t('errors.unavailable') }}
          </p>
          <p class="ops-gauge-detail">{{ gauge.thresholdLabel }}</p>
          <p class="ops-gauge-detail" data-ops-resource-scope>
            {{ t(`resourceScope.${gauge.scope}`)
            }}<span v-if="gauge.sampledAt">
              · {{ formatClock(gauge.sampledAt * 1000, locale) }}</span
            >
          </p>
          <p v-if="gauge.detail" class="ops-gauge-detail" :title="gauge.detail">
            {{ gauge.detail }}
          </p>
        </li>
      </ul>
      <dl
        class="ops-metric-grid ops-metric-grid--3"
        :aria-description="t('native.trafficScope')"
        :title="t('native.trafficScope')"
      >
        <OpsMetric
          :label="t('resources.up')"
          v-bind="
            overview.resources.network.up_mbps === null
              ? { value: t('unavailable'), unit: '' }
              : formatMbps(overview.resources.network.up_mbps, locale)
          "
          size="sm"
        />
        <OpsMetric
          :label="t('resources.down')"
          v-bind="
            overview.resources.network.down_mbps === null
              ? { value: t('unavailable'), unit: '' }
              : formatMbps(overview.resources.network.down_mbps, locale)
          "
          size="sm"
        />
        <OpsMetric
          :label="t('resources.goroutines')"
          :value="formatCount(overview.resources.process.goroutines, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('resources.heap')"
          v-bind="
            formatBytes(overview.resources.process.heap_alloc_bytes, locale)
          "
          :hint="
            t('native.runtimeMemory', {
              value: joined(
                formatBytes(
                  overview.resources.process.runtime_sys_bytes,
                  locale
                )
              ),
            })
          "
          size="sm"
        />
        <OpsMetric
          :label="t('resources.gcPause')"
          v-bind="
            overview.resources.process.last_gc_pause_ms === null
              ? { value: t('unavailable'), unit: '' }
              : formatLatencyMs(
                  overview.resources.process.last_gc_pause_ms,
                  locale
                )
          "
          size="sm"
        />
        <OpsMetric
          :label="t('resources.gc')"
          :value="formatCount(overview.resources.process.gc_count, locale)"
          :hint="t('scope.process')"
          size="sm"
        />
      </dl>
      <div v-if="trend" class="ops-trend">
        <p class="ops-trend-title">
          {{ t('resources.trend')
          }}<span class="ops-scope">{{
            t('scope.window', { window: t(`window.${overview.series.window}`) })
          }}</span>
        </p>
        <OpsTrendChart
          :title="t('native.instanceHistory')"
          :label="t('resources.trend')"
          :series="trend.series"
          :bucket-seconds="trend.bucketSeconds"
          :range="trend.range"
          unit="%"
          :max="trend.max"
          :height="120"
        />
      </div>
      <div v-if="trafficTrend" class="ops-trend">
        <p class="ops-trend-title">{{ t('resources.network') }}</p>
        <OpsTrendChart
          :label="t('resources.network')"
          :series="trafficTrend.series"
          :bucket-seconds="overview.series.bucket_seconds"
          :range="trafficTrend.range"
          unit="Mbps"
        />
      </div>
    </template>
  </OpsCard>
</template>
