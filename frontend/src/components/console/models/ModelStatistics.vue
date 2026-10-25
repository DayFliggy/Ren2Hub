<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { MODEL_PERFORMANCE_HOURS } from '@/api/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import type { ModelPerformanceState } from '@/types/modelMarket'
import { formatLatencyMs } from '@/utils/opsFormat'

/**
 * With `performance`, shows native perf-metrics for the whole model. Without
 * it, the statistics describe one channel offer, which perf-metrics does not
 * measure, so the unavailable state and its reason are shown instead.
 */
const props = withDefaults(
  defineProps<{ compact?: boolean; performance?: ModelPerformanceState }>(),
  { compact: false, performance: undefined }
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

const metrics = computed(() => {
  if (props.performance?.status !== 'ready') return []
  const summary = props.performance.summary
  const latency = formatLatencyMs(summary.avg_latency_ms, locale.value)
  return [
    {
      key: 'successRate',
      value: `${formatNumber(summary.success_rate)}%`,
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
    <dl class="statistics-metrics">
      <div v-for="metric in metrics" :key="metric.key">
        <dt>{{ t(`modelStats.${metric.key}`) }}</dt>
        <dd>{{ metric.value }}</dd>
      </div>
    </dl>
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
  gap: 0.4rem 0.5rem;
  padding: 0.65rem 0;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  border-top: 1px dashed var(--border-subtle);
}
.statistics-mark {
  width: 0.4rem;
  height: 0.4rem;
  border: 1px solid var(--text-tertiary);
  border-radius: 50%;
  flex: none;
}
.statistics-explanation,
.statistics-scope {
  flex-basis: 100%;
  line-height: 1.7;
}
.statistics-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 1rem;
}
.statistics-metrics > div {
  display: flex;
  align-items: baseline;
  gap: 0.35rem;
}
.statistics-metrics dd {
  color: var(--text-primary);
  font-family: var(--font-mono);
}
.statistics-status.is-error {
  color: var(--status-danger-text);
}
.compact {
  padding: 0.5rem 0 0;
  font-size: 0.6875rem;
}
</style>
