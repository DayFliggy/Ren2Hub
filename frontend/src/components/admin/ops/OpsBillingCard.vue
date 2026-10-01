<script setup lang="ts">
import { computed } from 'vue'
import { ArrowRight } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { billingApi } from '@/api/billing'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { useAppStore } from '@/stores'
import { formatQuota } from '@/utils/format'
import { formatCount, formatTimestamp } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

const INTERVAL_MS = 60_000
const OPERATIONAL_COUNTERS = [
  'calculation_total',
  'calculation_failures_total',
  'missing_price_total',
  'unknown_usage_total',
  'replay_mismatches_total',
] as const

// Metric labels are shared with the reconciliations page (console domain).
const { t: billingLabel } = useI18n()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const app = useAppStore()
const feed = usePolledFeed(
  (signal) => billingApi.metrics(signal),
  () => INTERVAL_MS
)
const metrics = computed(() => feed.data.value)
const reconciliationsEnabled = computed(() =>
  app.isFeatureEnabled('billing_reconciliation')
)
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
    <template v-if="reconciliationsEnabled" #action>
      <RouterLink
        :to="{ name: 'billing-reconciliations' }"
        class="ops-link focus-ring"
        data-ops-reconciliations-link
        >{{ t('billing.openReconciliations')
        }}<ArrowRight :size="14" aria-hidden="true"
      /></RouterLink>
    </template>
    <template v-if="metrics">
      <p
        v-if="metrics.pending === 0 && metrics.reconciliations === 0"
        class="ops-all-clear"
      >
        <span
          class="ops-dot"
          style="background: var(--status-success)"
          aria-hidden="true"
        />{{ t('billing.allClear') }}
      </p>
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="billingLabel('billing.metrics.pending')"
          :value="formatCount(metrics.pending, locale)"
          size="md"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.reconciliations')"
          :value="formatCount(metrics.reconciliations, locale)"
          :status="attention(metrics.reconciliations)"
          size="md"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.oldest_wait_seconds')"
          :value="formatCount(metrics.oldest_wait_seconds, locale)"
          size="md"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.recovery_failures')"
          :value="formatCount(metrics.recovery_failures, locale)"
          :status="attention(metrics.recovery_failures)"
          size="sm"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.idempotency_conflicts')"
          :value="formatCount(metrics.idempotency_conflicts, locale)"
          :status="attention(metrics.idempotency_conflicts)"
          size="sm"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.debt_events')"
          :value="formatCount(metrics.debt_events, locale)"
          size="sm"
        />
        <OpsMetric
          :label="billingLabel('billing.metrics.reserved_quota')"
          :value="formatQuota(metrics.reserved_quota, 3)"
          size="sm"
        />
      </dl>
      <div v-if="metrics.operational" class="ops-trend">
        <p class="ops-trend-title">
          {{ t('billing.operational')
          }}<span
            class="ops-scope"
            :title="
              t('runtime.started', {
                time: formatTimestamp(metrics.operational.started_at, locale),
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
            :value="formatCount(metrics.operational[key], locale)"
            :status="
              key === 'calculation_total'
                ? null
                : attention(metrics.operational[key])
            "
            size="sm"
          />
        </dl>
      </div>
    </template>
  </OpsCard>
</template>
