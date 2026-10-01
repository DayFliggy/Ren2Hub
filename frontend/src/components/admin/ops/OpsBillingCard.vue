<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { OPS_SETTLEMENT_BASES, opsMonitorApi } from '@/api/opsMonitor'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatCount, formatTimestamp } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

const INTERVAL_MS = 60_000
const OPERATIONAL_COUNTERS = [
  'calculation_total',
  'calculation_failures_total',
  'missing_price_total',
] as const

const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const feed = usePolledFeed(opsMonitorApi.billing, () => INTERVAL_MS)
const metrics = computed(() => feed.data.value)
const attention = (value: number) => (value > 0 ? ('warning' as const) : null)
</script>

<template>
  <OpsCard
    :title="t('billing.title')"
    :scope="t('rootOnly')"
    :loading="feed.loading.value"
    :loaded="metrics !== null"
    :error="feed.error.value"
    :updated-at="feed.updatedAt.value"
    data-ops-section="billing"
    @retry="feed.refresh"
  >
    <template v-if="metrics">
      <p v-if="metrics.billing_recovery.pending === 0" class="ops-all-clear">
        <span
          class="ops-dot"
          style="background: var(--status-success)"
          aria-hidden="true"
        />{{ t('billing.allClear') }}
      </p>
      <p class="ops-trend-title">
        {{ t('billing.recovery') }}
        <span class="ops-scope">{{ t('billing.databaseScope') }}</span>
      </p>
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="t('billing.pending')"
          :value="formatCount(metrics.billing_recovery.pending, locale)"
          :status="attention(metrics.billing_recovery.pending)"
          size="md"
        />
        <OpsMetric
          :label="t('billing.oldestUpdated')"
          :value="
            formatTimestamp(metrics.billing_recovery.oldest_updated_at, locale)
          "
          size="sm"
        />
      </dl>
      <p class="ops-footnote">{{ t('billing.recoveryHint') }}</p>
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('billing.operational') }}
          <span
            class="ops-scope"
            :title="
              t('runtime.started', {
                time: formatTimestamp(metrics.billing.started_at, locale),
              })
            "
            >{{ t('scope.process') }}</span
          >
        </p>
        <dl class="ops-metric-grid ops-metric-grid--3">
          <OpsMetric
            v-for="key in OPERATIONAL_COUNTERS"
            :key="key"
            :label="t(`billing.counters.${key}`)"
            :value="formatCount(metrics.billing[key], locale)"
            :status="
              key === 'calculation_total'
                ? null
                : attention(metrics.billing[key])
            "
            size="sm"
          />
        </dl>
        <p class="ops-trend-title">{{ t('billing.settlements') }}</p>
        <dl class="ops-metric-grid ops-metric-grid--3" data-ops-settlements>
          <OpsMetric
            v-for="basis in OPS_SETTLEMENT_BASES"
            :key="basis"
            :label="t(`billing.bases.${basis}`)"
            :value="
              formatCount(metrics.billing.settlements_total[basis], locale)
            "
            size="sm"
          />
        </dl>
      </div>
    </template>
  </OpsCard>
</template>
