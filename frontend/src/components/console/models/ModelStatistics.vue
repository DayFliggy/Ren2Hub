<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MODEL_PERFORMANCE_HOURS } from '@/api/modelMarket'
import type { PerfBucketPoint } from '@/api/liveContracts'
import { modelMarketMessages } from '@/i18n/modelMarket'
import type { ModelPerformanceState } from '@/types/modelMarket'
import { formatLatencyMs } from '@/utils/opsFormat'

/**
 * With `performance`, shows native perf-metrics for the whole model. Without
 * it, the statistics describe one channel offer, which perf-metrics does not
 * measure, so the unavailable state and its reason are shown instead.
 */
const props = withDefaults(
  defineProps<{
    compact?: boolean
    performance?: ModelPerformanceState
    series?: PerfBucketPoint[]
    seriesLoading?: boolean
  }>(),
  {
    compact: false,
    performance: undefined,
    series: undefined,
    seriesLoading: false,
  }
)
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMarketMessages,
})
const hours = MODEL_PERFORMANCE_HOURS

function formatNumber(value: number) {
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 2,
  }).format(value)
}

function rateTone(rate: number): 'good' | 'warn' | 'bad' {
  if (rate >= 99) return 'good'
  if (rate >= 95) return 'warn'
  return 'bad'
}

function formatSlotTime(ts: number) {
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(ts * 1000))
}

const metrics = computed(() => {
  if (props.performance?.status !== 'ready') return []
  const summary = props.performance.summary
  const latency = formatLatencyMs(summary.avg_latency_ms, locale.value)
  return [
    {
      key: 'successRate',
      value: `${formatNumber(summary.success_rate)}%`,
      tone: rateTone(summary.success_rate),
    },
    { key: 'latency', value: `${latency.value} ${latency.unit}` },
    {
      key: 'tps',
      // The backend reports 0 when no generation timing was sampled.
      value:
        summary.avg_tps > 0
          ? formatNumber(summary.avg_tps)
          : t('modelStats.tpsUnsampled'),
    },
  ]
})

const COMPACT_PLACEHOLDER_BARS = 12

const compactRibbon = computed(() => {
  if (props.performance?.status !== 'ready') return []
  return (props.performance.summary.recent_success_series ?? []).slice(-16)
})

const compactPlaceholderState = computed<'empty' | 'loading' | 'error' | null>(
  () => {
    if (!props.compact || !props.performance) return null
    if (props.performance.status === 'ready') return null
    return props.performance.status
  }
)

const compactPlaceholderAriaLabel = computed(() => {
  const status = compactPlaceholderState.value
  if (!status) return undefined
  return `${t('modelStats.scope', { hours })}：${t(`modelStats.${status}`, { hours })}`
})

const chartPoints = computed(() => {
  if (props.series?.length) {
    return props.series.slice(-24).map((point) => ({
      ts: point.ts,
      time: formatSlotTime(point.ts),
      successRate: point.success_rate,
      tone: rateTone(point.success_rate),
      heightPct: Math.max(12, Math.min(100, point.success_rate)),
      latency:
        point.avg_latency_ms > 0
          ? formatLatencyMs(point.avg_latency_ms, locale.value)
          : null,
      ttft:
        point.avg_ttft_ms > 0
          ? formatLatencyMs(point.avg_ttft_ms, locale.value)
          : null,
      tps: point.avg_tps > 0 ? formatNumber(point.avg_tps) : null,
    }))
  }
  if (props.performance?.status === 'ready') {
    const recent = props.performance.summary.recent_success_series ?? []
    return recent.slice(-24).map((point) => ({
      ts: point.ts,
      time: formatSlotTime(point.ts),
      successRate: point.success_rate,
      tone: rateTone(point.success_rate),
      heightPct: Math.max(12, Math.min(100, point.success_rate)),
      latency: null,
      ttft: null,
      tps: null,
    }))
  }
  return []
})

const activeSlot = ref(-1)
watch(chartPoints, (points) => {
  activeSlot.value = points.length ? points.length - 1 : -1
})
const inspectedPoint = computed(() =>
  activeSlot.value >= 0 ? chartPoints.value[activeSlot.value] : undefined
)

function stepSlot(delta: number, event: KeyboardEvent) {
  const total = chartPoints.value.length
  if (!total) return
  event.preventDefault()
  const next = Math.max(0, Math.min(total - 1, activeSlot.value + delta))
  activeSlot.value = next
  const group = (event.currentTarget as HTMLElement).parentElement
  const target = group?.children[next] as HTMLElement | undefined
  target?.focus()
}
</script>

<template>
  <div
    v-if="performance?.status === 'ready'"
    class="model-statistics"
    :class="{ compact }"
    data-model-statistics="ready"
    data-market-telemetry
    :role="compact ? 'group' : undefined"
    :aria-label="compact ? t('modelStats.scope', { hours }) : undefined"
  >
    <span v-if="!compact" class="statistics-scope">{{
      t('modelStats.scope', { hours })
    }}</span>
    <div class="statistics-summary-row">
      <dl class="statistics-metrics">
        <div v-for="metric in metrics" :key="metric.key">
          <dt>
            <span
              v-if="metric.tone"
              class="statistics-dot"
              :class="`is-${metric.tone}`"
              aria-hidden="true"
            />{{ t(`modelStats.${metric.key}`) }}
          </dt>
          <dd>{{ metric.value }}</dd>
        </div>
      </dl>
      <div
        v-if="compact && compactRibbon.length"
        class="statistics-mini-ribbon"
        aria-hidden="true"
      >
        <span
          v-for="point in compactRibbon"
          :key="point.ts"
          class="statistics-mini-bar"
          :class="`is-${rateTone(point.success_rate)}`"
          :style="{
            height: `${Math.max(20, Math.min(100, point.success_rate))}%`,
          }"
        />
      </div>
      <div
        v-else-if="compact"
        class="statistics-mini-ribbon is-placeholder"
        aria-hidden="true"
      >
        <span
          v-for="index in COMPACT_PLACEHOLDER_BARS"
          :key="index"
          class="statistics-mini-bar is-empty"
        />
      </div>
    </div>
    <div
      v-if="!compact && chartPoints.length"
      class="statistics-series"
      data-model-statistics-series
    >
      <div class="statistics-series-head">
        <span>{{ t('modelStats.seriesTitle') }}</span>
        <span class="statistics-series-range">
          {{ chartPoints[0]?.time }} – {{ chartPoints.at(-1)?.time }}
        </span>
      </div>
      <div
        class="statistics-bars"
        role="group"
        :aria-label="t('modelStats.seriesTitle')"
      >
        <button
          v-for="(point, index) in chartPoints"
          :key="point.ts"
          type="button"
          class="statistics-bar-slot focus-ring"
          :class="{ active: index === activeSlot }"
          :aria-label="
            t('modelStats.slotLabel', {
              time: point.time,
              rate: formatNumber(point.successRate),
            })
          "
          :aria-pressed="index === activeSlot"
          @mouseenter="activeSlot = index"
          @focus="activeSlot = index"
          @click="activeSlot = index"
          @keydown.arrow-left="stepSlot(-1, $event)"
          @keydown.arrow-right="stepSlot(1, $event)"
        >
          <span
            class="statistics-bar-fill"
            :class="`is-${point.tone}`"
            :style="{ height: `${point.heightPct}%` }"
          />
        </button>
      </div>
      <div
        v-if="inspectedPoint"
        class="statistics-slot-readout"
        aria-live="polite"
      >
        <span class="statistics-slot-time">{{ inspectedPoint.time }}</span>
        <span>
          <span
            class="statistics-dot"
            :class="`is-${inspectedPoint.tone}`"
            aria-hidden="true"
          />
          {{ t('modelStats.successRate') }}
          <strong>{{ formatNumber(inspectedPoint.successRate) }}%</strong>
        </span>
        <span v-if="inspectedPoint.latency">
          {{ t('modelStats.latency') }}
          <strong
            >{{ inspectedPoint.latency.value }}
            {{ inspectedPoint.latency.unit }}</strong
          >
        </span>
        <span v-if="inspectedPoint.ttft">
          {{ t('modelStats.ttft') }}
          <strong
            >{{ inspectedPoint.ttft.value }}
            {{ inspectedPoint.ttft.unit }}</strong
          >
        </span>
        <span v-if="inspectedPoint.tps">
          {{ t('modelStats.tps') }}
          <strong>{{ inspectedPoint.tps }}</strong>
        </span>
      </div>
    </div>
    <p v-else-if="!compact && seriesLoading" class="statistics-series-note">
      {{ t('modelStats.loading') }}
    </p>
  </div>
  <div
    v-else-if="compactPlaceholderState"
    class="model-statistics compact is-placeholder"
    :class="`is-state-${compactPlaceholderState}`"
    :data-model-statistics="compactPlaceholderState"
    data-market-telemetry
    role="group"
    :aria-label="compactPlaceholderAriaLabel"
  >
    <div class="statistics-summary-row">
      <dl
        v-if="compactPlaceholderState === 'empty'"
        class="statistics-metrics is-placeholder"
      >
        <div>
          <dt>
            <span class="statistics-dot is-empty" aria-hidden="true" />{{
              t('modelStats.successRate')
            }}
          </dt>
          <dd>--</dd>
        </div>
        <div>
          <dt>{{ t('modelStats.latency') }}</dt>
          <dd>--</dd>
        </div>
        <div>
          <dt>{{ t('modelStats.tps') }}</dt>
          <dd>--</dd>
        </div>
      </dl>
      <span
        v-else
        class="statistics-status"
        :class="{ 'is-error': compactPlaceholderState === 'error' }"
      >
        <span
          class="statistics-dot"
          :class="compactPlaceholderState === 'error' ? 'is-bad' : 'is-loading'"
          aria-hidden="true"
        />{{ t(`modelStats.${compactPlaceholderState}`, { hours }) }}
      </span>
      <div
        class="statistics-mini-ribbon is-placeholder"
        :class="`is-${compactPlaceholderState}`"
        aria-hidden="true"
      >
        <span
          v-for="index in COMPACT_PLACEHOLDER_BARS"
          :key="index"
          class="statistics-mini-bar"
          :class="`is-${compactPlaceholderState}`"
        />
      </div>
    </div>
  </div>
  <div
    v-else-if="!compact && performance"
    class="model-statistics"
    :class="{ compact }"
    :data-model-statistics="performance.status"
    data-market-telemetry
  >
    <span class="statistics-scope">{{ t('modelStats.scope', { hours }) }}</span>
    <span
      class="statistics-status"
      :class="{ 'is-error': performance.status === 'error' }"
      >{{ t(`modelStats.${performance.status}`, { hours }) }}</span
    >
  </div>
  <div
    v-else-if="!compact"
    class="model-statistics-empty"
    :class="{ compact }"
    data-model-statistics-unavailable
    data-market-telemetry
  >
    <span class="statistics-mark" aria-hidden="true" />
    <span>{{ t('noStatistics') }}</span>
    <span v-if="!compact" class="statistics-explanation">{{
      t('noStatisticsDetail')
    }}</span>
  </div>
</template>

<style scoped>
.model-statistics,
.model-statistics-empty {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.45rem 0.55rem;
  padding: 0.7rem 0;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  border-top: 1px dashed var(--border-subtle);
}
.statistics-summary-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  width: 100%;
  min-width: 0;
}
.statistics-mark {
  width: 0.4rem;
  height: 0.4rem;
  border: 1px solid var(--text-tertiary);
  border-radius: 50%;
  flex: none;
}
.statistics-dot {
  display: inline-block;
  width: 0.4rem;
  height: 0.4rem;
  border-radius: 50%;
  margin-right: 0.28rem;
  vertical-align: middle;
}
.statistics-dot.is-good,
.statistics-bar-fill.is-good,
.statistics-mini-bar.is-good {
  background: var(--status-success);
}
.statistics-dot.is-warn,
.statistics-bar-fill.is-warn,
.statistics-mini-bar.is-warn {
  background: var(--status-warning);
}
.statistics-dot.is-bad,
.statistics-bar-fill.is-bad,
.statistics-mini-bar.is-bad {
  background: var(--status-danger);
}
.statistics-dot.is-empty {
  border: 1.5px solid var(--load-unchecked);
  background: transparent;
}
.statistics-dot.is-loading {
  background: var(--load-unchecked);
  opacity: 0.7;
  animation: statistics-pulse 1.4s ease-in-out infinite;
}
.statistics-metrics.is-placeholder dt {
  color: var(--text-tertiary);
}
.statistics-metrics.is-placeholder dd {
  color: var(--load-unchecked);
  opacity: 0.78;
}
.statistics-mini-bar.is-empty {
  height: 44%;
  background: var(--load-unchecked);
  opacity: 0.26;
}
.statistics-mini-bar.is-loading {
  height: 48%;
  background: var(--load-unchecked);
  opacity: 0.34;
  animation: statistics-pulse 1.4s ease-in-out infinite;
}
.statistics-mini-bar.is-error {
  height: 44%;
  background: var(--status-danger);
  opacity: 0.28;
}
@keyframes statistics-pulse {
  0%,
  100% {
    opacity: 0.22;
  }
  50% {
    opacity: 0.52;
  }
}
.statistics-explanation,
.statistics-scope {
  flex-basis: 100%;
  line-height: 1.6;
}
.statistics-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.9rem;
}
.statistics-metrics > div {
  display: flex;
  align-items: baseline;
  gap: 0.32rem;
}
.statistics-metrics dd {
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.statistics-mini-ribbon {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  height: 1rem;
  flex: none;
}
.statistics-mini-bar {
  width: 3px;
  border-radius: 2px 2px 0 0;
  opacity: 0.85;
}
.statistics-series {
  flex-basis: 100%;
  display: grid;
  gap: 0.45rem;
  margin-top: 0.25rem;
  padding: 0.7rem 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
}
.statistics-series-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.25rem 0.75rem;
  font-size: 0.6875rem;
  color: var(--text-secondary);
}
.statistics-series-range {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--text-tertiary);
}
.statistics-bars {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  height: 2.75rem;
  padding-top: 0.25rem;
  border-bottom: 1px solid var(--border-subtle);
}
.statistics-bar-slot {
  flex: 1 1 0;
  min-width: 0;
  height: 100%;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 0 1px;
  border-radius: 3px 3px 0 0;
  cursor: pointer;
}
.statistics-bar-slot:hover,
.statistics-bar-slot.active {
  background: var(--state-hover-layer);
}
.statistics-bar-fill {
  width: 100%;
  max-width: 0.7rem;
  border-radius: 3px 3px 0 0;
  opacity: 0.78;
  transition: opacity 0.12s ease;
}
.statistics-bar-slot:hover .statistics-bar-fill,
.statistics-bar-slot.active .statistics-bar-fill {
  opacity: 1;
}
.statistics-slot-readout {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.85rem;
  font-size: 0.6875rem;
  color: var(--text-secondary);
}
.statistics-slot-readout strong {
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.statistics-slot-time {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--text-tertiary);
}
.statistics-series-note {
  flex-basis: 100%;
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.statistics-status.is-error {
  color: var(--status-danger-text);
}
.compact {
  padding: 0.5rem 0 0;
  font-size: 0.6875rem;
}
html.dark .model-statistics,
html.dark .model-statistics-empty {
  border-top-style: solid;
}
@media (prefers-reduced-motion: reduce) {
  .statistics-bar-fill {
    transition: none;
  }
  .statistics-dot.is-loading,
  .statistics-mini-bar.is-loading {
    animation: none;
  }
}
</style>
