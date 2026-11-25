<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  checkStatus,
  seriesRange,
  type OpsOverview,
  type OpsSeriesField,
} from '@/api/opsMonitor'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { verdictTone } from '@/utils/loadTone'
import {
  formatBytes,
  formatClock,
  formatCount,
  formatLatencyMs,
  formatMbps,
  formatPercent,
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

const secondaryView = ref<'traffic' | 'process'>('traffic')
const secondaryOptions = computed(() => [
  { value: 'traffic', label: t('resourceTabs.traffic') },
  { value: 'process', label: t('resourceTabs.process') },
])

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

/** Neutral traffic lines matching SystemStatusCard (THEMES.md):
 * inbound (↓) primary & solid, outbound (↑) tertiary & dashed. */
const trafficTrend = computed(() => {
  const overview = props.overview
  if (!overview) return null
  return {
    range: seriesRange(overview),
    series: [
      {
        name: t('resources.down'),
        color: 'textPrimary' as const,
        dashed: false,
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value: point.inbound_mbps ?? null,
        })),
      },
      {
        name: t('resources.up'),
        color: 'textTertiary' as const,
        dashed: true,
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value: point.outbound_mbps ?? null,
        })),
      },
    ],
  }
})

const processTrend = computed(() => {
  const overview = props.overview
  if (!overview) return null
  return {
    range: seriesRange(overview),
    series: [
      {
        name: t('resourceTabs.goroutines'),
        color: 'signal' as const,
        yAxisIndex: 0 as const,
        unit: '',
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value: point.goroutines ?? null,
        })),
      },
      {
        name: t('resourceTabs.heapAlloc'),
        color: 'trendMemory' as const,
        yAxisIndex: 1 as const,
        unit: ' MiB',
        points: overview.series.points.map((point) => ({
          ts: point.ts,
          value:
            point.heap_alloc_bytes == null
              ? null
              : Math.round((point.heap_alloc_bytes / (1024 * 1024)) * 10) / 10,
        })),
      },
    ],
  }
})

const trafficNote = computed(() => {
  const traffic = props.overview?.observation?.traffic
  if (!traffic) return undefined
  const scopeKey =
    traffic.scope === 'host_interface' && traffic.interface === ''
      ? 'host_interface_unnamed'
      : traffic.scope
  const scope = t(`native.trafficScope.${scopeKey}`, {
    interface: traffic.interface,
  })
  if (traffic.reason === '') return scope
  return t('native.trafficUnavailable', {
    scope,
    reason: t(`trafficReasons.${traffic.reason}`, {
      interface: traffic.interface,
    }),
  })
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
          :style="{ '--mark-glow': gauge.tone.fill }"
        >
          <MiniRing
            :percent="gauge.percent ?? 0"
            :color="gauge.tone.fill"
            :size="64"
            :stroke-width="5"
            :indeterminate="gauge.percent === null"
          >
            <span
              class="ops-gauge-value display-number"
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
        :aria-description="trafficNote"
        :title="trafficNote"
        data-ops-traffic
      >
        <OpsMetric
          :label="t('resources.down')"
          color="var(--text-primary)"
          v-bind="
            overview.resources.network.down_mbps === null
              ? { value: t('unavailable'), unit: '' }
              : formatMbps(overview.resources.network.down_mbps, locale)
          "
          size="sm"
        />
        <OpsMetric
          :label="t('resources.up')"
          color="var(--text-tertiary)"
          v-bind="
            overview.resources.network.up_mbps === null
              ? { value: t('unavailable'), unit: '' }
              : formatMbps(overview.resources.network.up_mbps, locale)
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
      <div v-if="trafficTrend && processTrend" class="ops-trend">
        <div class="ops-subbar">
          <p class="ops-trend-title">
            {{
              secondaryView === 'traffic'
                ? t('resources.network')
                : t('resourceTabs.process')
            }}
          </p>
          <SegmentedToggle
            v-model="secondaryView"
            :options="secondaryOptions"
            :label="t('resourceTabs.label')"
            size="sm"
          />
        </div>
        <OpsTrendChart
          v-if="secondaryView === 'traffic'"
          :label="t('resources.network')"
          :series="trafficTrend.series"
          :bucket-seconds="overview.series.bucket_seconds"
          :range="trafficTrend.range"
          unit="Mbps"
          :height="120"
        />
        <OpsTrendChart
          v-else
          :label="t('resourceTabs.process')"
          :series="processTrend.series"
          :bucket-seconds="overview.series.bucket_seconds"
          :range="processTrend.range"
          :secondary-axis="{ unit: ' MiB' }"
          :height="120"
        />
      </div>
    </template>
  </OpsCard>
</template>
