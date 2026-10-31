<script setup lang="ts">
import { ArrowUpDown } from 'lucide-vue-next'
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'

import ConsoleCard from '@/components/common/ConsoleCard.vue'
import CpuLoadCells, {
  type CpuLoadCell,
} from '@/components/console/dashboard/CpuLoadCells.vue'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import MiniSparkline from '@/components/console/dashboard/MiniSparkline.vue'
import PrecisionTickDial from '@/components/console/dashboard/PrecisionTickDial.vue'
import type {
  SystemLoad,
  SystemResource,
  SystemStatus,
} from '@/api/systemStatus'
import { useAppStore } from '@/stores'
import {
  UNKNOWN_TONE,
  loadTone,
  shareTone,
  verdictTone,
  type LoadTone,
} from '@/utils/loadTone'

const props = defineProps<{
  metrics?: SystemStatus | null
  /** The last snapshot failure; shown on the status pill. */
  error?: string | null
  loading?: boolean
}>()

const { t } = useI18n()
const { versionLabel } = storeToRefs(useAppStore())

interface PillState {
  color: string
  textColor: string
  label: string
  pulse: boolean
}

type PillTone = Pick<LoadTone, 'fill' | 'text'>

/** A verdict takes its tile tier's colour, so a warning pill and a warning
 * tile match. Loading is no verdict and keeps the info colour. */
function pill(tone: PillTone, label: string, pulse = false): PillState {
  return { color: tone.fill, textColor: tone.text, label, pulse }
}

const LOADING_TONE: PillTone = {
  fill: 'var(--status-info)',
  text: 'var(--status-info-text)',
}

/** The pill reports the server's own health verdict, not page reachability.
 * The success rate stays out of it: it is a 24-hour relay figure, not a
 * health check of this instance. */
const apiState = computed<PillState>(() => {
  if (props.error)
    return pill(loadTone('danger'), t('dashboard.systemStatus.health.error'))
  const status = props.metrics?.status
  if (!status) {
    return props.loading
      ? pill(LOADING_TONE, t('dashboard.loading'))
      : pill(UNKNOWN_TONE, t('dashboard.systemStatus.health.unknown'))
  }
  if (status === 'ok')
    return pill(loadTone('normal'), t('dashboard.systemStatus.health.ok'), true)
  if (status === 'warning')
    return pill(loadTone('warning'), t('dashboard.systemStatus.health.warning'))
  // No tile shows the database or Redis, so a critical verdict from them is
  // named on the pill itself.
  if (status === 'critical')
    return pill(
      loadTone('danger'),
      t(
        props.metrics?.storage_status === 'critical'
          ? 'dashboard.systemStatus.health.storageCritical'
          : 'dashboard.systemStatus.health.critical'
      )
    )
  return pill(UNKNOWN_TONE, t(`dashboard.systemStatus.health.${status}`))
})

/** The pill's hover and screen-reader detail: the failure, or which readings a
 * stale snapshot withholds (host load and the success rate are read apart). */
const pillDetail = computed(() => {
  if (props.error) return props.error
  if (props.metrics?.stale) return t('dashboard.systemStatus.staleDetail')
  return null
})

function finiteNonNegative(value: number | null | undefined): number | null {
  if (
    value === null ||
    value === undefined ||
    !Number.isFinite(value) ||
    value < 0
  ) {
    return null
  }
  return value
}

/** Success rate reads the other way round — high is good. */
function rateTone(percent: number | null): LoadTone {
  if (percent === null) return UNKNOWN_TONE
  if (percent >= 99) return loadTone('normal')
  if (percent >= 95) return loadTone('warning')
  return loadTone('danger')
}

function formatMetric(value: number): string {
  if (!Number.isFinite(value)) return '--'
  const rounded = Math.round(value * 10) / 10
  return String(Object.is(rounded, -0) ? 0 : rounded)
}

function formatBandwidth(valueMbps: number): string {
  const safeValue = finiteNonNegative(valueMbps)
  if (safeValue === null) return '--'
  if (safeValue >= 1) return `${formatMetric(safeValue)} Mbps`
  if (safeValue >= 0.001) return `${formatMetric(safeValue * 1_000)} Kbps`
  return `${formatMetric(safeValue * 1_000_000)} bps`
}

/**
 * Used and total in one unit with at most three significant figures, so the
 * reading fits its tile. Byte counts are binary: GiB, and TiB from 1000 GiB.
 * Below 10 two decimals keep the third figure, so 930 / 1000 GiB reads
 * 0.91 / 0.98 TiB beside its 93% badge rather than 0.9 / 1.
 */
function storageReading(
  used: number | null,
  total: number | null
): MetricTile['storage'] {
  if (used === null || total === null) return undefined
  const scale = total >= 1000 ? 1024 : 1
  const format = (value: number) => {
    if (value >= 100) return String(Math.round(value))
    if (value >= 10) return formatMetric(value)
    return String(Math.round(value * 100) / 100)
  }
  return {
    used: format(used / scale),
    total: format(total / scale),
    unit: scale === 1 ? 'GiB' : 'TiB',
  }
}

/** The last twelve one-minute buckets. An unmeasured bucket stays a gap in
 * its own line; a stale snapshot shows no history at all. */
function trafficSeries(
  metrics: SystemStatus | null | undefined
): { down: (number | null)[]; up: (number | null)[] } | undefined {
  if (!metrics || metrics.stale) return undefined
  const points = metrics.traffic.points.slice(-12)
  const up = points.map((point) => finiteNonNegative(point.request_mbps))
  const down = points.map((point) => finiteNonNegative(point.response_mbps))
  const measured = (series: (number | null)[]) =>
    series.filter((value) => value !== null).length >= 2
  return measured(up) || measured(down) ? { up, down } : undefined
}

/** Only fresh, measured samples are shown; anything else reads as unknown. */
function observed(
  metrics: SystemStatus | null | undefined,
  resource: SystemResource | undefined
): boolean {
  return Boolean(
    metrics &&
    !metrics.stale &&
    resource?.available &&
    ['ok', 'warning', 'critical', 'unchecked'].includes(resource.state)
  )
}

/** A tile without a reading says when monitoring is off, not just "--". */
function placeholder(resource: SystemResource | undefined): string {
  return resource?.state === 'disabled'
    ? t('dashboard.systemStatus.resourceOff')
    : '--'
}

/** Memory and disk headline when the byte counts are missing: the share. */
function shareValue(
  percent: number | null,
  resource: SystemResource | undefined
): string {
  return percent === null ? placeholder(resource) : `${formatMetric(percent)}%`
}

/** A fresh measured share of a bounded resource, capped at its ceiling. */
function observedShare(
  metrics: SystemStatus | null | undefined,
  resource: SystemResource | undefined
): number | null {
  const percent = observed(metrics, resource)
    ? finiteNonNegative(resource?.percent)
    : null
  return percent === null ? null : Math.min(100, percent)
}

function gib(value: number | null | undefined): number | null {
  const bytes = finiteNonNegative(value)
  return bytes === null ? null : bytes / 1024 ** 3
}

function cpuCoreCount(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isInteger(value) && value > 0
    ? value
    : null
}

function formatLoad(value: number): string {
  if (value >= 100) return value.toFixed(0)
  if (value >= 10) return value.toFixed(1)
  return value.toFixed(2)
}

/**
 * 1 / 5 / 15 min cells; a cell fills by load per host core. Host load has no
 * health check and may differ from a container CPU figure, so it never takes
 * an alarm tier; load beyond the cores is marked only in its labelled readout.
 */
function buildLoadCells(
  load: SystemLoad | null | undefined,
  cores: number | null
): CpuLoadCell[] {
  const readings = [load?.one, load?.five, load?.fifteen]
  return [1, 5, 15].map((minutes, index) => {
    const value = finiteNonNegative(readings[index])
    if (value === null) {
      const color = 'var(--text-tertiary)'
      return { minutes, value: null, fill: null, color, overloaded: false }
    }
    const fill = cores === null ? null : value / cores
    return {
      minutes,
      value: formatLoad(value),
      fill,
      color: fill === null ? UNKNOWN_TONE.fill : shareTone(fill * 100).fill,
      overloaded: fill !== null && fill > 1,
    }
  })
}

/** Words for the tiers that colour alone would carry; the tile's tone decides,
 * so a reading withheld as unknown never reads as an alarm. */
const SCOPE_VERDICTS: Partial<Record<LoadTone['tier'], string>> = {
  warning: 'dashboard.systemStatus.scopeWarning',
  danger: 'dashboard.systemStatus.scopeCritical',
  unchecked: 'dashboard.systemStatus.thresholdOff',
}

/** Where the reading was measured, or why there is no judged reading. */
function scopeLabel(resource: SystemResource | undefined, tone: LoadTone) {
  if (resource?.state === 'disabled')
    return t('dashboard.systemStatus.monitorOff')
  const scope = t(
    `dashboard.systemStatus.scope.${resource?.scope ?? 'unknown'}`
  )
  const verdict = SCOPE_VERDICTS[tone.tier]
  return verdict ? t(verdict, { scope }) : scope
}

interface MetricTile {
  key: 'cpu' | 'memory' | 'bandwidth' | 'disk'
  label: string
  /** lucide 24x24 path data */
  icon?: string
  /** Where the reading was measured, or that its check is off; for the tile's
   * title and accessible label. */
  scope: string
  /** CPU headline, a memory or disk share without byte counts, or the
   * placeholder of a tile without data. */
  value: string
  unit: string
  bandwidth?: { up: string; down: string }
  /** Memory and disk: used and total in one unit. */
  storage?: { used: string; total: string; unit: string }
  /** CPU only: host load per averaging window and the core count it scales to. */
  load?: { cells: CpuLoadCell[]; cores: number | null }
  /** 0-100 usage against a ceiling; null when the metric is unavailable. */
  percent: number | null
  /** Shared-scale throughput history, for metrics with no ceiling. */
  series?: { down: (number | null)[]; up: (number | null)[] }
  tone: LoadTone
}

const tiles = computed<MetricTile[]>(() => {
  const metrics = props.metrics
  const memoryLive = observed(metrics, metrics?.memory)
  const diskLive = observed(metrics, metrics?.disk)
  const memoryUsed = memoryLive ? gib(metrics?.memory.used_bytes) : null
  const memoryTotal = memoryLive ? gib(metrics?.memory.total_bytes) : null
  const diskUsed = diskLive ? gib(metrics?.disk.used_bytes) : null
  const diskTotal = diskLive ? gib(metrics?.disk.total_bytes) : null
  // Container CPU may exceed 100% of one quota; the figure keeps the measured
  // value and only the gauge is clamped.
  const cpuPercent = observed(metrics, metrics?.cpu)
    ? finiteNonNegative(metrics?.cpu.percent)
    : null
  const cores = cpuCoreCount(metrics?.load?.cores)
  // The badge shows the percent the server judged, not a byte ratio of its own.
  const memoryPercent = observedShare(metrics, metrics?.memory)
  const diskPercent = observedShare(metrics, metrics?.disk)
  const bandwidthUp = metrics?.stale
    ? null
    : finiteNonNegative(metrics?.traffic.request_mbps)
  const bandwidthDown = metrics?.stale
    ? null
    : finiteNonNegative(metrics?.traffic.response_mbps)
  const bandwidth =
    bandwidthUp !== null && bandwidthDown !== null
      ? {
          up: formatBandwidth(bandwidthUp),
          down: formatBandwidth(bandwidthDown),
        }
      : undefined
  const cpuTone = verdictTone(metrics?.cpu.state, cpuPercent)
  const memoryTone = verdictTone(metrics?.memory.state, memoryPercent)
  const diskTone = verdictTone(metrics?.disk.state, diskPercent)

  return [
    {
      key: 'cpu',
      label: t('dashboard.systemStatus.cpu'),
      icon: 'M4 4h16v16H4zM9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3M9 9h6v6H9z',
      value:
        cpuPercent === null
          ? placeholder(metrics?.cpu)
          : formatMetric(cpuPercent),
      unit: cpuPercent === null ? '' : '%',
      scope: scopeLabel(metrics?.cpu, cpuTone),
      load: {
        cells: buildLoadCells(metrics?.load, cores),
        cores,
      },
      percent: cpuPercent === null ? null : Math.min(100, cpuPercent),
      tone: cpuTone,
    },
    {
      key: 'memory',
      label: t('dashboard.systemStatus.memory'),
      icon: 'M3 7h18v10H3zM7 7v10M11 7v10M15 7v10M5 17v3M19 17v3',
      scope: scopeLabel(metrics?.memory, memoryTone),
      value: shareValue(memoryPercent, metrics?.memory),
      unit: '',
      storage: storageReading(memoryUsed, memoryTotal),
      percent: memoryPercent,
      tone: memoryTone,
    },
    {
      key: 'bandwidth',
      label: t('dashboard.systemStatus.bandwidth'),
      scope: t('dashboard.systemStatus.scope.application_http_body'),
      value: bandwidth ? '' : '--',
      unit: '',
      bandwidth,
      percent: null,
      series: trafficSeries(metrics),
      tone: UNKNOWN_TONE,
    },
    {
      key: 'disk',
      label: t('dashboard.systemStatus.disk'),
      icon: 'M22 12H2M5.5 16h.01M9.5 16h.01M5.45 5h13.1a2 2 0 0 1 1.79 1.11L22 12v5a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5l1.66-5.89A2 2 0 0 1 5.45 5z',
      scope: scopeLabel(metrics?.disk, diskTone),
      value: shareValue(diskPercent, metrics?.disk),
      unit: '',
      storage: storageReading(diskUsed, diskTotal),
      percent: diskPercent,
      tone: diskTone,
    },
  ]
})

/**
 * Semicircle geometry for the CPU tile; null while CPU usage is unknown. The
 * needle stops well short of the arc's inner edge (radius 22) so it never
 * touches the progress cap.
 */
const CPU_NEEDLE_LENGTH = 18

const cpuGauge = computed(() => {
  const percent = tiles.value[0]?.percent ?? null
  if (percent === null) return null
  const angle = Math.PI - (percent / 100) * Math.PI
  return {
    dashOffset: Math.PI * 24 * (1 - percent / 100),
    needleX: 32 + CPU_NEEDLE_LENGTH * Math.cos(angle),
    needleY: 32 - CPU_NEEDLE_LENGTH * Math.sin(angle),
  }
})

/**
 * A partly filled segment fills its share in the tier colour instead of fading
 * the whole segment, so it keeps the fill's contrast. Only full segments take
 * the night glow (see the style block). Without a reading the segments are
 * dashed (see the style block), so they do not read as 0%.
 */
function memorySegmentStyle(
  percent: number | null,
  index: number,
  color: string
) {
  if (percent === null) return { '--mark-glow': 'transparent' }
  const fill = Math.min(1, Math.max(0, percent / 10 - index))
  const track = 'color-mix(in srgb, var(--text-primary) 12%, transparent)'
  const stop = `${(fill * 100).toFixed(1)}%`
  let background = `linear-gradient(90deg, ${color} ${stop}, ${track} ${stop})`
  if (fill >= 1) background = color
  if (fill <= 0) background = track
  return {
    background,
    '--mark-glow': fill >= 1 ? color : 'transparent',
  }
}

/** Relay success over the last 24 hours. Null states say why there is no
 * rate: no requests yet, metrics disabled, or a failed query. */
const successRate = computed(() => {
  const rate = props.metrics?.success_rate
  return rate?.state === 'ok' ? finiteNonNegative(rate.rate) : null
})
// Without a snapshot (loading or a failed read) the rate is just unknown, like
// the tiles; only a snapshot can say there were no requests.
const successEmptyLabel = computed(() => {
  const state = props.metrics?.success_rate.state
  return state && state !== 'ok'
    ? t(`dashboard.systemStatus.successRateState.${state}`)
    : '--'
})
const successTone = computed(() => rateTone(successRate.value))
</script>

<template>
  <ConsoleCard data-system-status-card stretch :padded="false">
    <template #title>
      <div class="flex min-w-0 items-center gap-2">
        <h2 class="truncate text-sm font-semibold text-[var(--text-primary)]">
          {{ t('dashboard.systemStatus.title') }}
        </h2>
        <!-- The pill and the rate never wrap: a narrow card truncates the
             title instead, so the header keeps one height in every state. -->
        <span
          class="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-semibold"
          :style="{
            background: `color-mix(in srgb, ${apiState.color} 14%, transparent)`,
            color: apiState.textColor,
          }"
          :data-system-health="error ? 'error' : (metrics?.status ?? 'none')"
          :title="pillDetail ?? undefined"
        >
          <span class="relative flex h-1.5 w-1.5">
            <span
              v-if="apiState.pulse"
              class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60"
              :style="{ background: apiState.color }"
            />
            <span
              class="relative inline-flex h-1.5 w-1.5 rounded-full"
              :style="{ background: apiState.color }"
            />
          </span>
          <span>{{ apiState.label }}</span>
          <span v-if="pillDetail" class="sr-only">{{ pillDetail }}</span>
        </span>
      </div>
    </template>

    <template #action>
      <div class="flex items-center gap-2">
        <div class="text-right">
          <p
            class="success-rate-label text-[0.6875rem] leading-none text-[var(--text-tertiary)]"
            :title="t('dashboard.systemStatus.successRateWindow')"
          >
            {{ t('dashboard.systemStatus.successRate') }}
          </p>
          <!-- A failed query is a fault, not an empty window, so it alone
               takes the warning ink. -->
          <p
            v-if="successRate === null"
            class="success-rate-value whitespace-nowrap text-xs font-medium leading-none"
            :class="
              metrics?.success_rate.state === 'error'
                ? 'text-[var(--status-warning-text)]'
                : 'text-[var(--text-tertiary)]'
            "
            data-success-rate-empty
            :data-success-rate-state="metrics?.success_rate.state ?? 'none'"
          >
            {{ successEmptyLabel }}
          </p>
          <p
            v-else
            class="success-rate-value whitespace-nowrap text-sm font-bold leading-none tabular-nums"
            :style="{ color: successTone.text }"
          >
            {{ formatMetric(successRate) }}%
          </p>
        </div>
        <PrecisionTickDial
          :percent="successRate"
          :color="successTone.fill"
          :size="38"
          :style="{ '--mark-glow': successTone.fill }"
        />
      </div>
    </template>

    <div class="flex grow flex-col px-4 pt-2.5 pb-2.5 sm:px-5 sm:pt-3 sm:pb-3">
      <div
        class="mb-2.5 grid grid-cols-2 content-start gap-2 sm:mb-3 sm:gap-2.5"
        data-system-status-grid
      >
        <div
          v-for="tile in tiles"
          :key="tile.key"
          class="relative flex h-[5.5rem] min-w-0 flex-col rounded-xl bg-[var(--surface-muted)] px-2.5 py-1.5 sm:h-[5.75rem] sm:px-3 sm:py-2"
          :class="{ 'overflow-hidden': tile.key !== 'cpu' }"
          data-system-status-tile
          :data-metric="tile.key"
          :data-load-tier="tile.tone.tier"
          :style="{ '--mark-glow': tile.tone.fill }"
        >
          <div class="flex min-h-5 items-center justify-between gap-1">
            <p
              class="flex min-w-0 items-center gap-1 text-[0.6875rem] text-[var(--text-tertiary)]"
            >
              <ArrowUpDown
                v-if="tile.key === 'bandwidth'"
                :size="12"
                :stroke-width="1.8"
                aria-hidden="true"
                data-bandwidth-icon
              />
              <svg
                v-else-if="tile.icon"
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path :d="tile.icon" />
              </svg>
              <span class="truncate" :title="tile.scope">{{ tile.label }}</span>
              <span class="sr-only" data-metric-scope>{{ tile.scope }}</span>
            </p>
            <!-- Only a byte reading gets the badge. A headline that is already
                 the percentage (CPU, or a share without byte counts) would be
                 repeated and could disagree (4.6% vs 5%). CPU's slot holds the
                 load cells instead. -->
            <span
              v-if="tile.percent !== null && tile.storage"
              class="shrink-0 rounded-md px-1 py-0.5 font-mono text-[0.625rem] font-semibold tabular-nums sm:px-1.5"
              data-percent-badge
              :style="{
                color: tile.tone.text,
                background: `color-mix(in srgb, ${tile.tone.fill} 14%, transparent)`,
              }"
            >
              {{ Math.round(tile.percent) }}%
            </span>
            <CpuLoadCells
              v-else-if="tile.load"
              :cells="tile.load.cells"
              :cores="tile.load.cores"
            />
          </div>

          <!-- The figure keeps its width; the decorative gauge shrinks first. -->
          <div
            v-if="tile.key === 'cpu'"
            class="mt-auto flex items-end justify-between gap-1.5 pt-0.5"
            data-cpu-gauge
          >
            <!-- Only the figure and its unit are monospaced; a placeholder word
                 keeps the body face of the other tiles ("Off", not "0ff"). -->
            <p
              class="flex shrink-0 items-baseline gap-0.5 whitespace-nowrap sm:gap-1"
            >
              <span
                v-if="tile.percent !== null"
                class="display-number status-headline whitespace-nowrap font-mono leading-none"
                :style="{ color: tile.tone.text }"
                data-metric-value
              >
                {{ tile.value }}
              </span>
              <span
                v-else
                class="text-base font-bold leading-tight tabular-nums"
                :style="{ color: tile.tone.text }"
                data-metric-placeholder
              >
                {{ tile.value }}
              </span>
              <span
                v-if="tile.unit"
                class="font-mono text-[0.625rem] font-medium text-[var(--text-tertiary)]"
              >
                {{ tile.unit }}
              </span>
            </p>
            <svg
              class="h-auto w-16 min-w-10 shrink"
              viewBox="0 0 64 38"
              fill="none"
              aria-hidden="true"
            >
              <path
                d="M8 32a24 24 0 0 1 48 0"
                stroke="color-mix(in srgb, var(--text-primary) 12%, transparent)"
                stroke-width="4"
                stroke-linecap="round"
              />
              <!-- The coloured arc carries the reading; the needle and pivot stay
                   secondary so they do not outweigh a low-load arc. -->
              <template v-if="cpuGauge">
                <path
                  d="M8 32a24 24 0 0 1 48 0"
                  :stroke="tile.tone.fill"
                  stroke-width="4"
                  stroke-linecap="round"
                  :stroke-dasharray="Math.PI * 24"
                  :stroke-dashoffset="cpuGauge.dashOffset"
                  data-cpu-gauge-active
                />
                <line
                  x1="32"
                  y1="32"
                  :x2="cpuGauge.needleX"
                  :y2="cpuGauge.needleY"
                  stroke="var(--text-secondary)"
                  stroke-width="1.8"
                  stroke-linecap="round"
                  data-cpu-gauge-needle
                />
                <circle cx="32" cy="32" r="2.25" fill="var(--text-secondary)" />
              </template>
              <!-- Same unknown-state language as the disk ring: dashed, no pivot. -->
              <path
                v-else
                d="M8 32a24 24 0 0 1 48 0"
                :stroke="tile.tone.fill"
                stroke-width="4"
                stroke-dasharray="3 4"
                opacity="0.7"
                data-cpu-gauge-unknown
              />
            </svg>
          </div>

          <template v-else-if="tile.key === 'memory'">
            <div
              v-if="tile.storage"
              class="storage-row mt-auto flex items-baseline font-mono leading-none whitespace-nowrap pt-0.5"
            >
              <span
                class="display-number status-headline leading-none"
                :style="{ color: tile.tone.text }"
                data-metric-value
              >
                {{ tile.storage.used }}
              </span>
              <span
                class="text-[0.625rem] font-normal text-[var(--text-tertiary)] opacity-40 sm:text-xs"
              >
                /
              </span>
              <span
                class="text-[0.6875rem] font-semibold tabular-nums text-[var(--text-secondary)] sm:text-[0.8125rem]"
              >
                {{ tile.storage.total }}
              </span>
              <span
                class="text-[0.625rem] font-medium text-[var(--text-tertiary)]"
              >
                {{ tile.storage.unit }}
              </span>
            </div>
            <p
              v-else
              class="mt-auto flex items-baseline gap-1 whitespace-nowrap pt-0.5"
            >
              <span
                class="text-base font-bold leading-tight tabular-nums"
                :style="{ color: tile.tone.text }"
                :data-metric-placeholder="
                  tile.percent === null ? '' : undefined
                "
              >
                {{ tile.value }}
              </span>
            </p>
            <div
              class="mt-1 grid h-1.5 grid-cols-10 gap-0.5 sm:mt-1.5 sm:h-2 sm:gap-1"
              data-memory-segments
              :data-unknown="tile.percent === null ? '' : undefined"
              aria-hidden="true"
            >
              <span
                v-for="index in 10"
                :key="index"
                class="memory-segment min-w-0 rounded-xs transition-[background,box-shadow] duration-500"
                :style="
                  memorySegmentStyle(tile.percent, index - 1, tile.tone.fill)
                "
              />
            </div>
          </template>

          <!-- Traffic has no ceiling and so no load tier: it stays neutral. Each
               value shares its line's ink, download primary and solid, upload
               tertiary and dashed, so values and lines pair without a legend. -->
          <template v-else-if="tile.key === 'bandwidth'">
            <div class="mt-auto flex min-w-0 flex-col justify-end">
              <p
                class="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 text-[0.625rem] font-bold leading-tight tabular-nums sm:text-[0.6875rem]"
              >
                <template v-if="tile.bandwidth">
                  <span
                    data-bandwidth-direction="up"
                    class="shrink-0 whitespace-nowrap text-[var(--text-tertiary)]"
                  >
                    ↑{{ tile.bandwidth.up }}
                  </span>
                  <span
                    data-bandwidth-direction="down"
                    class="shrink-0 whitespace-nowrap text-[var(--text-primary)]"
                  >
                    ↓{{ tile.bandwidth.down }}
                  </span>
                </template>
                <span
                  v-else
                  class="text-base font-bold leading-tight tabular-nums text-[var(--text-tertiary)]"
                  data-metric-placeholder
                  >--</span
                >
              </p>
              <div
                v-if="tile.series"
                class="mt-0.5 overflow-hidden"
                data-bandwidth-sparkline
              >
                <MiniSparkline
                  :points="tile.series.down"
                  :secondary="tile.series.up"
                  color="var(--text-primary)"
                  secondary-color="var(--text-tertiary)"
                  :height="16"
                />
              </div>
            </div>
          </template>

          <div
            v-else
            class="mt-auto flex items-center justify-between gap-1.5 pb-0.5"
            data-disk-gauge
          >
            <div
              v-if="tile.storage"
              class="flex min-w-0 flex-col justify-center"
            >
              <div class="flex items-baseline gap-0.5 leading-none sm:gap-1">
                <span
                  class="display-number status-headline font-mono leading-none"
                  :style="{ color: tile.tone.text }"
                  data-metric-value
                >
                  {{ tile.storage.used }}
                </span>
                <span
                  class="font-mono text-[0.625rem] font-medium text-[var(--text-tertiary)]"
                >
                  {{ tile.storage.unit }}
                </span>
              </div>
              <div
                class="mt-0.5 flex items-baseline gap-0.5 font-mono text-[0.625rem] font-medium leading-none text-[var(--text-tertiary)] sm:mt-1 sm:gap-1 sm:text-[0.6875rem]"
              >
                <span class="opacity-50">/</span>
                <span class="tabular-nums text-[var(--text-secondary)]">
                  {{ tile.storage.total }}
                </span>
                <span class="opacity-80">{{ tile.storage.unit }}</span>
              </div>
            </div>
            <p
              v-else
              class="flex min-w-0 items-baseline gap-1 whitespace-nowrap"
            >
              <span
                class="text-base font-bold leading-tight tabular-nums"
                :style="{ color: tile.tone.text }"
                :data-metric-placeholder="
                  tile.percent === null ? '' : undefined
                "
              >
                {{ tile.value }}
              </span>
            </p>
            <MiniRing
              :percent="tile.percent ?? 0"
              :color="tile.tone.fill"
              :size="32"
              :indeterminate="tile.percent === null"
            />
          </div>
        </div>
      </div>

      <div
        class="mt-auto flex items-center justify-between gap-3 border-t border-[var(--border-subtle)] pt-2 text-[0.6875rem] sm:pt-2.5"
      >
        <span
          class="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[var(--text-tertiary)]"
        >
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path
              d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82zM7 7h.01"
            />
          </svg>
          {{ t('dashboard.systemStatus.version') }}
        </span>
        <span
          class="min-w-0 truncate font-mono text-[var(--text-secondary)]"
          :title="versionLabel"
          data-system-version
          >{{ versionLabel }}</span
        >
      </div>
    </div>
  </ConsoleCard>
</template>

<style scoped>
/* The card width comes from the dashboard grid, not the viewport, so the
   header adapts to the card. */
[data-system-status-card] {
  container-type: inline-size;
}

/* Tile figures size by card width too: in the three-column desktop grid the
   card is narrower than on a phone. The longest readings ("87.4 / 94.2 GB",
   "45.6 %" beside a 40px gauge) fit 24px only from a 344px card. */
.status-headline {
  font-size: 1.125rem;
}

/* The memory reading's gaps follow the card, not the viewport: the binary unit
   ("87.4 / 94.2 GiB") needs tight gaps up to a 380px card (measured 347px with
   the 24px headline). */
.storage-row {
  gap: 0.125rem;
}

@container (min-width: 344px) {
  .status-headline {
    font-size: 1.5rem;
  }
}

@container (min-width: 380px) {
  .storage-row {
    gap: 0.25rem;
  }
}

/* Glow is a night-theme accent; day tiles stay flat. The filled part of every
   main gauge (CPU arc, disk ring, full memory segments, active success ticks)
   takes the same faint glow in its own colour, so no gauge stands out from the
   others. The small load cells stay flat. */
html.dark [data-cpu-gauge-active],
html.dark [data-disk-gauge] :deep(circle[stroke-linecap]),
html.dark [data-success-rate-ring] :deep(.tick-active),
html.dark .memory-segment {
  filter: drop-shadow(
    0 0 3px color-mix(in srgb, var(--mark-glow) 40%, transparent)
  );
}

/* No reading: dashed like an unknown CPU arc, disk ring or load cell. */
[data-memory-segments][data-unknown] .memory-segment {
  border: 1px dashed color-mix(in srgb, var(--text-tertiary) 60%, transparent);
}

/* An absolutely positioned label drops this margin with it. */
.success-rate-label {
  margin-bottom: 4px;
}

/* A tight header first drops the caption from the layout, keeping it readable
   to assistive technology, then the decorative dial; the rate always shows.
   Breakpoints are the measured card widths each layout needs, with slack:
   Chinese 289px with the caption; English 362px, 320px without the caption
   and 274px without the dial. */
@container (max-width: 299px) {
  .success-rate-label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
}

@container (max-width: 365px) {
  .success-rate-label:lang(en) {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
}

@container (max-width: 323px) {
  [data-success-rate-ring]:lang(en) {
    display: none;
  }
}
</style>
