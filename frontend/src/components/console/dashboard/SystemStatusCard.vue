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
import type { SystemMetrics } from '@/composables/useDashboard'
import { useAppStore } from '@/stores'

const props = defineProps<{
  metrics?: SystemMetrics | null
}>()

const { t } = useI18n()
const { phase, statusReachable, versionLabel } = storeToRefs(useAppStore())
const metricsAvailable = computed(
  () => props.metrics !== null && props.metrics !== undefined
)

const apiState = computed(() => {
  if (phase.value === 'loading') {
    return {
      color: 'var(--status-info)',
      label: t('dashboard.loading'),
      pulse: false,
    }
  }
  if (phase.value === 'error') {
    return {
      color: 'var(--status-danger)',
      label: t('dashboard.offline'),
      pulse: false,
    }
  }
  if (
    phase.value === 'ready' &&
    statusReachable.value &&
    metricsAvailable.value
  ) {
    return {
      color: 'var(--status-success)',
      label: t('dashboard.online'),
      pulse: true,
    }
  }
  if (
    phase.value === 'degraded' &&
    statusReachable.value &&
    metricsAvailable.value
  ) {
    return {
      color: 'var(--status-warning)',
      label: t('dashboard.degraded'),
      pulse: false,
    }
  }
  return {
    color: 'var(--text-tertiary)',
    label: t('dashboard.unknown'),
    pulse: false,
  }
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

function clampPercent(value: number | null | undefined): number | null {
  const finite = finiteNonNegative(value)
  return finite === null ? null : Math.min(100, finite)
}

function ratioPercent(
  used: number | null | undefined,
  total: number | null | undefined
): number | null {
  const safeUsed = finiteNonNegative(used)
  const safeTotal = finiteNonNegative(total)
  if (safeUsed === null || safeTotal === null || safeTotal <= 0) return null
  return Math.min(100, (safeUsed / safeTotal) * 100)
}

/**
 * 统一 4 档资源负载语义色彩梯度 (CPU / 内存 / 磁盘):
 * - <= 60%: 正常 (var(--glow))
 * - 60% ~ 75%: 关注 (var(--accent))
 * - 75% ~ 90%: 警告 (var(--status-warning))
 * - > 90%: 危险 (var(--status-danger))
 */
function resourceLoadColor(percent: number | null): string {
  if (percent === null) return 'var(--text-tertiary)'
  if (percent > 90) return 'var(--status-danger)'
  if (percent > 75) return 'var(--status-warning)'
  if (percent > 60) return 'var(--accent)'
  return 'var(--glow)'
}

/** Success rate reads the other way round — high is good. */
function rateColor(percent: number | null): string {
  if (percent === null) return 'var(--text-tertiary)'
  if (percent >= 99) return 'var(--glow)'
  if (percent >= 95) return 'var(--status-warning)'
  return 'var(--status-danger)'
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

function formatStorage(value: number | null | undefined): string {
  const safeValue = finiteNonNegative(value)
  return safeValue === null ? '--' : formatMetric(safeValue)
}

function normalizeSeries(
  series: SystemMetrics['bandwidth_series'] | undefined
): { down: number[]; up: number[] } | undefined {
  if (!series || !Array.isArray(series.up) || !Array.isArray(series.down)) {
    return undefined
  }

  if (series.up.length !== series.down.length || series.up.length < 2) {
    return undefined
  }

  const up: number[] = []
  const down: number[] = []
  for (let index = 0; index < series.up.length; index += 1) {
    const safeUp = finiteNonNegative(series.up[index])
    const safeDown = finiteNonNegative(series.down[index])
    if (safeUp === null || safeDown === null) return undefined
    up.push(safeUp)
    down.push(safeDown)
  }

  return up.length >= 2 ? { up, down } : undefined
}

function cpuCoreCount(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isInteger(value) && value > 0
    ? value
    : null
}

/** Two decimals as uptime prints them; fewer once the figure grows, so a
 * reading still fits the stretched cell. */
function formatLoad(value: number): string {
  if (value >= 100) return value.toFixed(0)
  if (value >= 10) return value.toFixed(1)
  return value.toFixed(2)
}

/** 1 / 5 / 15 min cells, each measured against the core count with the same
 * four-tier colours as CPU usage. */
function buildLoadCells(
  load: SystemMetrics['load_average'] | undefined,
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
      color: resourceLoadColor(fill === null ? null : fill * 100),
      overloaded: fill !== null && fill > 1,
    }
  })
}

interface MetricTile {
  key: 'cpu' | 'memory' | 'bandwidth' | 'disk'
  label: string
  /** lucide 24x24 path data */
  icon?: string
  value: string
  unit: string
  bandwidth?: { up: string; down: string }
  memory?: { used: string; total: string }
  disk?: { used: string; total: string }
  /** CPU only: host load per averaging window and the core count it scales to. */
  load?: { cells: CpuLoadCell[]; cores: number | null }
  /** 0-100 usage against a ceiling; null when the metric is unavailable. */
  percent: number | null
  /** Shared-scale throughput history, for metrics with no ceiling. */
  series?: { down: number[]; up: number[] }
  color: string
}

const tiles = computed<MetricTile[]>(() => {
  const metrics = props.metrics
  const memoryUsed = finiteNonNegative(metrics?.memory_used_gb)
  const memoryTotal = finiteNonNegative(metrics?.memory_total_gb)
  const diskUsed = finiteNonNegative(metrics?.disk_used_gb)
  const diskTotal = finiteNonNegative(metrics?.disk_total_gb)
  const cpuPercent = clampPercent(metrics?.cpu_percent)
  const cpuCores = cpuCoreCount(metrics?.cpu_cores)
  const memoryPercent = ratioPercent(memoryUsed, memoryTotal)
  const diskPercent = ratioPercent(diskUsed, diskTotal)
  const bandwidthUp = finiteNonNegative(metrics?.bandwidth_up_mbps)
  const bandwidthDown = finiteNonNegative(metrics?.bandwidth_down_mbps)
  const bandwidth =
    bandwidthUp !== null && bandwidthDown !== null
      ? {
          up: formatBandwidth(bandwidthUp),
          down: formatBandwidth(bandwidthDown),
        }
      : undefined

  return [
    {
      key: 'cpu',
      label: t('dashboard.systemStatus.cpu'),
      icon: 'M4 4h16v16H4zM9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3M9 9h6v6H9z',
      value: cpuPercent === null ? '--' : formatMetric(cpuPercent),
      unit: cpuPercent === null ? '' : '%',
      load: {
        cells: buildLoadCells(metrics?.load_average, cpuCores),
        cores: cpuCores,
      },
      percent: cpuPercent,
      color: resourceLoadColor(cpuPercent),
    },
    {
      key: 'memory',
      label: t('dashboard.systemStatus.memory'),
      icon: 'M3 7h18v10H3zM7 7v10M11 7v10M15 7v10M5 17v3M19 17v3',
      value:
        memoryUsed === null || memoryTotal === null
          ? '--'
          : `${formatStorage(memoryUsed)} / ${formatStorage(memoryTotal)}`,
      unit: memoryUsed === null || memoryTotal === null ? '' : 'GB',
      memory:
        memoryUsed !== null && memoryTotal !== null
          ? {
              used: formatStorage(memoryUsed),
              total: formatStorage(memoryTotal),
            }
          : undefined,
      percent: memoryPercent,
      color: resourceLoadColor(memoryPercent),
    },
    {
      key: 'bandwidth',
      label: t('dashboard.systemStatus.bandwidth'),
      value: bandwidth ? '' : '--',
      unit: '',
      bandwidth,
      percent: null,
      series: normalizeSeries(metrics?.bandwidth_series),
      color: 'var(--signal)',
    },
    {
      key: 'disk',
      label: t('dashboard.systemStatus.disk'),
      icon: 'M22 12H2M5.5 16h.01M9.5 16h.01M5.45 5h13.1a2 2 0 0 1 1.79 1.11L22 12v5a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5l1.66-5.89A2 2 0 0 1 5.45 5z',
      value:
        diskUsed === null || diskTotal === null
          ? '--'
          : `${formatStorage(diskUsed)} / ${formatStorage(diskTotal)}`,
      unit: diskUsed === null || diskTotal === null ? '' : 'GB',
      disk:
        diskUsed !== null && diskTotal !== null
          ? {
              used: formatStorage(diskUsed),
              total: formatStorage(diskTotal),
            }
          : undefined,
      percent: diskPercent,
      color: resourceLoadColor(diskPercent),
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

function memorySegmentStyle(
  percent: number | null,
  index: number,
  color = 'var(--glow)'
) {
  const fill =
    percent === null ? 0 : Math.min(1, Math.max(0, percent / 10 - index))
  const isFilled = fill > 0
  return {
    background: isFilled
      ? color
      : 'color-mix(in srgb, var(--text-primary) 12%, transparent)',
    opacity: isFilled ? (fill >= 1 ? 1 : 0.6) : 1,
    boxShadow:
      fill >= 1
        ? `0 0 8px color-mix(in srgb, ${color} 45%, transparent)`
        : 'none',
  }
}

/** Success rate reads out in the header, so it is not one of the tiles. */
const successRate = computed(() =>
  clampPercent(props.metrics?.api_success_rate)
)
const successColor = computed(() => rateColor(successRate.value))
</script>

<template>
  <ConsoleCard data-system-status-card stretch :padded="false">
    <template #title>
      <div class="flex min-w-0 items-center gap-2">
        <h2 class="truncate text-sm font-semibold text-[var(--text-primary)]">
          {{ t('dashboard.systemStatus.title') }}
        </h2>
        <span
          class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-semibold"
          :style="{
            background: `color-mix(in srgb, ${apiState.color} 14%, transparent)`,
            color: apiState.color,
          }"
          :data-status-reachable="statusReachable"
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
        </span>
      </div>
    </template>

    <template #action>
      <div class="flex items-center gap-2">
        <div class="text-right">
          <p
            class="success-rate-label text-[11px] leading-none text-[var(--text-tertiary)]"
          >
            {{ t('dashboard.systemStatus.successRate') }}
          </p>
          <p
            class="success-rate-value text-sm font-bold leading-none tabular-nums"
            :style="{ color: successColor }"
          >
            {{ successRate === null ? '--' : `${formatMetric(successRate)}%` }}
          </p>
        </div>
        <PrecisionTickDial
          :percent="successRate"
          :color="successColor"
          :size="38"
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
          class="group relative flex h-[88px] min-w-0 flex-col rounded-xl bg-[var(--surface-muted)] px-2.5 py-1.5 transition-colors duration-300 hover:bg-[var(--surface-hover)] sm:h-[92px] sm:px-3 sm:py-2"
          :class="{ 'overflow-hidden': tile.key !== 'cpu' }"
          data-system-status-tile
          :data-metric="tile.key"
        >
          <div class="flex min-h-5 items-center justify-between gap-1">
            <p
              class="flex min-w-0 items-center gap-1 text-[11px] text-[var(--text-tertiary)]"
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
              <span class="truncate">{{ tile.label }}</span>
            </p>
            <!-- CPU's headline is already the percentage; a rounded badge would
                 repeat it and could disagree with it (4.6% vs 5%). Its slot holds
                 the load cells instead. -->
            <span
              v-if="tile.percent !== null && tile.key !== 'cpu'"
              class="shrink-0 rounded-md px-1 py-0.5 font-mono text-[10px] font-semibold tabular-nums sm:px-1.5"
              data-percent-badge
              :style="{
                color: tile.color,
                background: `color-mix(in srgb, ${tile.color} 14%, transparent)`,
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

          <div
            v-if="tile.key === 'cpu'"
            class="mt-auto flex items-end justify-between gap-1.5 pt-0.5"
            data-cpu-gauge
          >
            <p
              class="flex min-w-0 items-baseline gap-0.5 whitespace-nowrap font-mono sm:gap-1"
            >
              <span
                class="display-number whitespace-nowrap text-lg leading-none sm:text-2xl"
                :style="{ color: tile.color }"
                data-metric-value
              >
                {{ tile.value }}
              </span>
              <span
                v-if="tile.unit"
                class="text-[10px] font-medium text-[var(--text-tertiary)]"
              >
                {{ tile.unit }}
              </span>
            </p>
            <svg
              class="h-8 w-14 shrink-0 sm:h-9 sm:w-16"
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
                  :stroke="tile.color"
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
                :stroke="tile.color"
                stroke-width="4"
                stroke-dasharray="3 4"
                opacity="0.7"
                data-cpu-gauge-unknown
              />
            </svg>
          </div>

          <template v-else-if="tile.key === 'memory'">
            <div
              v-if="tile.memory"
              class="mt-auto flex items-baseline gap-0.5 font-mono leading-none whitespace-nowrap pt-0.5 sm:gap-1"
            >
              <span
                class="display-number text-lg leading-none sm:text-2xl"
                :style="{ color: tile.color }"
                data-metric-value
              >
                {{ tile.memory.used }}
              </span>
              <span
                class="text-[10px] font-normal text-[var(--text-tertiary)] opacity-40 sm:text-xs"
              >
                /
              </span>
              <span
                class="text-[11px] font-semibold tabular-nums text-[var(--text-secondary)] sm:text-[13px]"
              >
                {{ tile.memory.total }}
              </span>
              <span
                v-if="tile.unit"
                class="text-[10px] font-medium text-[var(--text-tertiary)]"
              >
                {{ tile.unit }}
              </span>
            </div>
            <p
              v-else
              class="mt-auto flex items-baseline gap-1 whitespace-nowrap pt-0.5"
            >
              <span
                class="text-base font-bold leading-tight tabular-nums"
                :style="{ color: tile.color }"
              >
                {{ tile.value }}
              </span>
              <span
                v-if="tile.unit"
                class="text-[10px] text-[var(--text-tertiary)]"
              >
                {{ tile.unit }}
              </span>
            </p>
            <div
              class="mt-1 grid h-1.5 grid-cols-10 gap-0.5 sm:mt-1.5 sm:h-2 sm:gap-1"
              data-memory-segments
              aria-hidden="true"
            >
              <span
                v-for="index in 10"
                :key="index"
                class="min-w-0 rounded-sm transition-[background,opacity,box-shadow] duration-500"
                :style="memorySegmentStyle(tile.percent, index - 1, tile.color)"
              />
            </div>
          </template>

          <template v-else-if="tile.key === 'bandwidth'">
            <div class="mt-auto flex min-w-0 flex-col justify-end">
              <p
                class="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 text-[10px] font-bold leading-tight tabular-nums sm:text-[11px]"
              >
                <template v-if="tile.bandwidth">
                  <span
                    data-bandwidth-direction="up"
                    class="shrink-0 whitespace-nowrap"
                    style="color: var(--accent)"
                  >
                    ↑{{ tile.bandwidth.up }}
                  </span>
                  <span
                    data-bandwidth-direction="down"
                    class="shrink-0 whitespace-nowrap"
                    style="color: var(--glow)"
                  >
                    ↓{{ tile.bandwidth.down }}
                  </span>
                </template>
                <span v-else class="text-[var(--text-tertiary)]">--</span>
              </p>
              <div
                v-if="tile.series"
                class="mt-0.5 overflow-hidden"
                data-bandwidth-sparkline
              >
                <MiniSparkline
                  :points="tile.series.down"
                  :secondary="tile.series.up"
                  color="var(--glow)"
                  secondary-color="var(--accent)"
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
            <div v-if="tile.disk" class="flex min-w-0 flex-col justify-center">
              <div class="flex items-baseline gap-0.5 leading-none sm:gap-1">
                <span
                  class="display-number font-mono text-lg leading-none sm:text-2xl"
                  :style="{ color: tile.color }"
                  data-metric-value
                >
                  {{ tile.disk.used }}
                </span>
                <span
                  v-if="tile.unit"
                  class="font-mono text-[10px] font-semibold sm:text-[11px]"
                  :style="{ color: tile.color }"
                >
                  {{ tile.unit }}
                </span>
              </div>
              <div
                class="mt-0.5 flex items-baseline gap-0.5 font-mono text-[10px] font-medium leading-none text-[var(--text-tertiary)] sm:mt-1 sm:gap-1 sm:text-[11px]"
              >
                <span class="opacity-50">/</span>
                <span class="tabular-nums text-[var(--text-secondary)]">
                  {{ tile.disk.total }}
                </span>
                <span class="opacity-80">{{ tile.unit }}</span>
              </div>
            </div>
            <p
              v-else
              class="flex min-w-0 items-baseline gap-1 whitespace-nowrap"
            >
              <span
                class="text-base font-bold leading-tight tabular-nums"
                :style="{ color: tile.color }"
              >
                {{ tile.value }}
              </span>
              <span
                v-if="tile.unit"
                class="text-[10px] text-[var(--text-tertiary)]"
              >
                {{ tile.unit }}
              </span>
            </p>
            <MiniRing
              :percent="tile.percent ?? 0"
              :color="tile.color"
              :size="32"
              :indeterminate="tile.percent === null"
            />
          </div>
        </div>
      </div>

      <div
        class="mt-auto flex items-center justify-between gap-3 border-t border-[var(--border-subtle)] pt-2 text-[11px] sm:pt-2.5"
      >
        <span class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
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
        <span class="font-mono text-[var(--text-secondary)]">{{
          versionLabel
        }}</span>
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
