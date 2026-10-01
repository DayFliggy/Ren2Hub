<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpsDatabaseProbe, OpsOverview, OpsStatus } from '@/api/opsMonitor'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { STATUS_TONES, formatCount, formatLatencyMs } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

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
const storage = computed(() => props.overview?.storage ?? null)
const databases = computed(() => [
  {
    key: 'database',
    label: t('storage.database'),
    probe: storage.value?.database ?? null,
  },
  {
    key: 'log_database',
    label: t('storage.logDatabase'),
    probe: storage.value?.log_database ?? null,
  },
])
const metricStatus = (status: OpsStatus) =>
  status === 'warning' || status === 'critical' ? status : null
const poolLimit = (probe: OpsDatabaseProbe) =>
  probe.max_open === 0
    ? t('storage.unlimited')
    : formatCount(probe.max_open, locale.value)
</script>

<template>
  <section
    class="ops-storage"
    :aria-label="t('storage.title')"
    data-ops-section="storage"
  >
    <h2 class="sr-only">{{ t('storage.title') }}</h2>
    <OpsCard
      v-for="item in databases"
      :key="item.key"
      :title="item.label"
      :scope="t('scope.instant')"
      :loading="loading"
      :loaded="overview !== null"
      :error="error"
      :updated-at="updatedAt"
      :retryable="false"
      :data-ops-storage="item.key"
    >
      <template v-if="item.probe" #action>
        <StatusChip :tone="STATUS_TONES[item.probe.status]">{{
          t(`status.${item.probe.status}`)
        }}</StatusChip>
      </template>
      <p v-if="!item.probe" class="ops-empty">
        {{ t('storage.sharedLogDatabase') }}
      </p>
      <template v-else>
        <p class="ops-storage-kind">
          <code>{{ item.probe.kind }}</code
          ><span v-if="item.probe.error" class="ops-storage-error">{{
            t(`errors.${item.probe.error}`)
          }}</span>
        </p>
        <dl class="ops-metric-grid ops-metric-grid--3">
          <OpsMetric
            :label="t('storage.latency')"
            v-bind="formatLatencyMs(item.probe.latency_ms, locale)"
            :status="metricStatus(item.probe.status)"
            size="md"
          />
          <OpsMetric
            :label="t('storage.inUse')"
            :value="formatCount(item.probe.in_use, locale)"
            :hint="`${t('storage.maxOpen')} ${poolLimit(item.probe)}`"
            size="md"
          />
          <OpsMetric
            :label="t('storage.idle')"
            :value="formatCount(item.probe.idle, locale)"
            :hint="`${t('storage.open')} ${formatCount(item.probe.open, locale)}`"
            size="md"
          />
          <OpsMetric
            :label="t('storage.waitCount')"
            :value="formatCount(item.probe.wait_count, locale)"
            :hint="t('scope.process')"
            size="sm"
          />
        </dl>
      </template>
    </OpsCard>
    <OpsCard
      :title="t('storage.redis')"
      :scope="t('scope.instant')"
      :loading="loading"
      :loaded="overview !== null"
      :error="error"
      :updated-at="updatedAt"
      :retryable="false"
      data-ops-storage="redis"
    >
      <template v-if="storage" #action>
        <StatusChip :tone="STATUS_TONES[storage.redis.status]">{{
          t(`status.${storage.redis.status}`)
        }}</StatusChip>
      </template>
      <template v-if="storage">
        <p v-if="!storage.redis.enabled" class="ops-empty">
          {{ t('storage.redisDisabled') }}
        </p>
        <template v-else>
          <p v-if="storage.redis.error" class="ops-storage-kind">
            <span class="ops-storage-error">{{
              t(`errors.${storage.redis.error}`)
            }}</span>
          </p>
          <dl class="ops-metric-grid ops-metric-grid--3">
            <OpsMetric
              :label="t('storage.latency')"
              v-bind="formatLatencyMs(storage.redis.latency_ms, locale)"
              :status="metricStatus(storage.redis.status)"
              size="md"
            />
            <OpsMetric
              :label="t('storage.totalConns')"
              :value="formatCount(storage.redis.total_conns, locale)"
              size="md"
            />
            <OpsMetric
              :label="t('storage.idleConns')"
              :value="formatCount(storage.redis.idle_conns, locale)"
              size="md"
            />
            <OpsMetric
              :label="t('storage.staleConns')"
              :value="formatCount(storage.redis.stale_conns, locale)"
              :hint="t('scope.process')"
              size="sm"
            />
          </dl>
        </template>
      </template>
    </OpsCard>
  </section>
</template>
