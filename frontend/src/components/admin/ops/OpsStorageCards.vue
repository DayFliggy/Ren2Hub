<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  opsMonitorApi,
  type DBPoolStat,
  type DiskCacheStats,
  type OpsOverview,
  type PerformanceAction,
} from '@/api/opsMonitor'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  STATUS_TONES,
  formatBytes,
  formatCount,
  formatLatencyMs,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

defineOptions({ inheritAttrs: false })

const props = defineProps<{
  overview: OpsOverview | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const emit = defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const storage = computed(() => props.overview?.observation?.storage ?? null)
const redis = computed(() => storage.value?.redis ?? null)
const dbPool = computed(() => props.overview?.cache?.db_pool ?? null)
// A separate log pool only exists when the log database is not shared.
const pools = computed(() => {
  const pool = dbPool.value
  if (!pool) return []
  const rows = [
    {
      key: 'database',
      stats: pool.main,
      probe: storage.value?.database ?? null,
    },
  ]
  if (!pool.log_shared)
    rows.push({
      key: 'logDatabase',
      stats: pool.log,
      probe: storage.value?.log_database ?? null,
    })
  return rows
})
function poolMetrics(pool: DBPoolStat) {
  const count = (key: string, value: number) => ({
    key,
    value: formatCount(value, locale.value),
    unit: '',
  })
  return [
    count('inUse', pool.in_use),
    count('idle', pool.idle),
    count('open', pool.open_connections),
    pool.max_open_connections === 0
      ? { key: 'maxOpen', value: t('storage.unlimited'), unit: '' }
      : count('maxOpen', pool.max_open_connections),
    count('waitCount', pool.wait_count),
    {
      key: 'waitDuration',
      ...formatLatencyMs(pool.wait_duration_ms, locale.value),
    },
  ]
}
const cacheMetrics = computed(() => {
  const stats = props.overview?.cache
  if (!stats) return []
  const count = (key: keyof DiskCacheStats) => ({
    key,
    value: formatCount(stats.cache_stats[key], locale.value),
    unit: '',
  })
  const bytes = (key: keyof DiskCacheStats) => ({
    key,
    ...formatBytes(stats.cache_stats[key], locale.value),
  })
  return [
    bytes('current_memory_usage_bytes'),
    bytes('current_disk_usage_bytes'),
    count('active_memory_buffers'),
    count('active_disk_files'),
    count('memory_cache_hits'),
    count('disk_cache_hits'),
  ]
})
const target = ref<PerformanceAction | null>(null)
const pending = ref(false)
const actionError = ref('')
const result = ref('')
let disposed = false
onBeforeUnmount(() => {
  disposed = true
})
const actions: PerformanceAction[] = ['gc', 'disk_cache', 'reset_stats']
function confirmAction(action: PerformanceAction) {
  actionError.value = ''
  target.value = action
}
async function executeAction() {
  if (!target.value || pending.value) return
  const action = target.value
  pending.value = true
  actionError.value = ''
  result.value = ''
  try {
    await opsMonitorApi.execute(action)
    if (disposed) return
    target.value = null
    result.value = t('native.actionCompleted', {
      action: t(`native.actions.${action}`),
    })
    emit('refresh')
  } catch (cause) {
    if (!disposed)
      actionError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) pending.value = false
  }
}
</script>

<template>
  <section
    v-bind="$attrs"
    class="ops-storage"
    :aria-label="t('storage.title')"
    data-ops-section="storage"
  >
    <h2 class="sr-only">{{ t('storage.title') }}</h2>
    <OpsCard
      v-for="pool in pools"
      :key="pool.key"
      :title="t(`storage.${pool.key}`)"
      :scope="t('scope.instant')"
      :loading="loading"
      :loaded="dbPool !== null"
      :error="error"
      :updated-at="updatedAt"
      :retryable="false"
      :data-ops-storage="pool.key"
    >
      <template #action
        ><StatusChip :tone="STATUS_TONES[pool.probe?.status ?? 'unknown']">{{
          t(`status.${pool.probe?.status ?? 'unknown'}`)
        }}</StatusChip></template
      >
      <p v-if="pool.probe?.reason" class="ops-note">
        {{ t(`observationReasons.${pool.probe.reason}`) }}
      </p>
      <OpsMetric
        :label="t('storage.latency')"
        v-bind="formatLatencyMs(pool.probe?.latency_ms ?? null, locale)"
        size="sm"
      />
      <p class="ops-storage-kind">{{ t('native.poolScope') }}</p>
      <p v-if="pool.stats === null" class="ops-empty" data-ops-pool-unavailable>
        {{ t('native.poolUnavailable') }}
      </p>
      <dl v-else class="ops-metric-grid ops-metric-grid--fit">
        <OpsMetric
          v-for="metric in poolMetrics(pool.stats)"
          :key="metric.key"
          :label="t(`storage.${metric.key}`)"
          :value="metric.value"
          :unit="metric.unit"
          size="md"
        />
      </dl>
      <p
        v-if="pool.key === 'database' && dbPool?.log_shared"
        class="ops-footnote"
      >
        {{ t('storage.sharedLogDatabase') }}
      </p>
    </OpsCard>
    <OpsCard
      :title="t('storage.redis')"
      :scope="t('scope.instant')"
      :loading="loading"
      :loaded="redis !== null"
      :error="error"
      :updated-at="updatedAt"
      :retryable="false"
      data-ops-storage="redis"
    >
      <template #action
        ><StatusChip :tone="STATUS_TONES[redis?.status ?? 'unknown']">{{
          t(`status.${redis?.status ?? 'unknown'}`)
        }}</StatusChip></template
      >
      <p v-if="redis?.reason" class="ops-note">
        {{ t(`observationReasons.${redis.reason}`) }}
      </p>
      <dl class="ops-metric-grid ops-metric-grid--fit">
        <OpsMetric
          :label="t('storage.latency')"
          v-bind="formatLatencyMs(redis?.latency_ms ?? null, locale)"
          size="md"
        />
      </dl>
    </OpsCard>
    <OpsCard
      :title="t('native.cache')"
      :scope="t('scope.instant')"
      :loading="loading"
      :loaded="overview?.cache != null"
      :error="error"
      :updated-at="updatedAt"
      :retryable="false"
      data-ops-storage="cache"
    >
      <template v-if="overview?.cache">
        <dl class="ops-metric-grid ops-metric-grid--fit">
          <OpsMetric
            v-for="metric in cacheMetrics"
            :key="metric.key"
            :label="t(`native.cacheMetrics.${metric.key}`)"
            :value="metric.value"
            :unit="metric.unit"
            size="sm"
          />
        </dl>
        <p class="ops-footnote">{{ t('native.cacheCounterScope') }}</p>
        <div class="ops-maintenance-actions">
          <ConsoleButton
            v-for="action in actions"
            :key="action"
            variant="secondary"
            size="sm"
            :disabled="pending"
            @click="confirmAction(action)"
            >{{ t(`native.actions.${action}`) }}</ConsoleButton
          >
        </div>
        <p v-if="result" class="ops-note" role="status">{{ result }}</p>
      </template>
    </OpsCard>
  </section>
  <ConfirmDialog
    :open="target !== null"
    :title="target ? t(`native.actions.${target}`) : ''"
    :message="t('native.confirmAction')"
    :loading="pending"
    @confirm="executeAction"
    @cancel="target = null"
  >
    <p v-if="actionError" class="ops-storage-error" role="alert">
      {{ actionError }}
    </p>
  </ConfirmDialog>
</template>
