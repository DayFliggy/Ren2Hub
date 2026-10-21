<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  opsMonitorApi,
  performanceNumber,
  type OpsOverview,
  type PerformanceAction,
} from '@/api/opsMonitor'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatBytes, formatCount } from '@/utils/opsFormat'
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
const probes = [
  { key: 'database', metrics: ['latency', 'inUse', 'idle', 'waitCount'] },
  {
    key: 'redis',
    metrics: ['latency', 'totalConns', 'idleConns', 'staleConns'],
  },
]
const cacheMetrics = computed(() => {
  const stats = props.overview?.cache
  if (!stats) return []
  const count = (key: string) => ({
    key,
    value: formatCount(performanceNumber(stats.cache_stats, key), locale.value),
    unit: '',
  })
  const bytes = (key: string) => ({
    key,
    ...formatBytes(performanceNumber(stats.cache_stats, key), locale.value),
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
      v-for="probe in probes"
      :key="probe.key"
      :title="t(`storage.${probe.key}`)"
      :scope="t('scope.instant')"
      :loading="false"
      :loaded="true"
      error=""
      :updated-at="0"
      :retryable="false"
      :data-ops-storage="probe.key"
    >
      <template #action
        ><StatusChip tone="neutral">{{
          t('status.unknown')
        }}</StatusChip></template
      >
      <p class="ops-storage-kind">{{ t('native.storageUnavailable') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--fit">
        <OpsMetric
          v-for="metric in probe.metrics"
          :key="metric"
          :label="t(`storage.${metric}`)"
          value="--"
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
