<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { api } from '@/api/console'
import {
  opsMonitorApi,
  performanceNumber,
  type PerformanceStats,
} from '@/api/opsMonitor'
import { useToast } from '@/composables/useToast'

interface ChannelAffinityStats {
  enabled: boolean
  total: number
  unknown: number
  cache_capacity: number
  cache_algo: string
}

interface LogFiles {
  enabled: boolean
  file_count: number
  total_size: number
}

const props = defineProps<{
  kind: 'channel-affinity' | 'performance'
}>()

type Action =
  'clear-affinity' | 'clear-cache' | 'reset-stats' | 'gc' | 'cleanup-logs'

const { t } = useI18n()
const toast = useToast()
const pendingAction = ref<Action | null>(null)
const loading = ref(false)
const loadingStats = ref(false)
const affinityStats = ref<ChannelAffinityStats | null>(null)
const affinityStatsFailed = ref(false)
const performanceStats = ref<PerformanceStats | null>(null)
const logFiles = ref<LogFiles | null>(null)
const logCleanupMode = ref<'by_count' | 'by_days'>('by_count')
const logCleanupValue = ref(30)
// GET /api/performance/logs answers enabled:false when no log directory is
// configured; null means the status is unknown because the read failed.
const logEnabled = computed(() => logFiles.value?.enabled === true)

const actionText = computed(() => {
  switch (pendingAction.value) {
    case 'clear-affinity':
      return {
        title: t('systemSettings.maintenance.clearAffinity'),
        message: t('systemSettings.maintenance.clearAffinityMessage'),
      }
    case 'clear-cache':
      return {
        title: t('systemSettings.maintenance.clearCache'),
        message: t('systemSettings.maintenance.clearCacheMessage'),
      }
    case 'reset-stats':
      return {
        title: t('systemSettings.maintenance.resetStats'),
        message: t('systemSettings.maintenance.resetStatsMessage'),
      }
    case 'gc':
      return {
        title: t('systemSettings.maintenance.gc'),
        message: t('systemSettings.maintenance.gcMessage'),
      }
    case 'cleanup-logs':
      return {
        title: t('systemSettings.maintenance.cleanupLogs'),
        message: t(
          logCleanupMode.value === 'by_count'
            ? 'systemSettings.maintenance.cleanupLogsByCount'
            : 'systemSettings.maintenance.cleanupLogsByDays',
          { count: logCleanupValue.value }
        ),
      }
    default:
      return { title: '', message: '' }
  }
})

// common.DiskCacheStats exposes usage counters, not hit/miss totals, so the
// summary reports active cache usage instead of a hit rate.
const cacheSummary = computed(() => {
  const stats = performanceStats.value?.cache_stats
  return {
    memoryCount: formatCount(stats?.active_memory_buffers),
    memorySize: formatBytes(stats?.current_memory_usage_bytes),
    diskCount: formatCount(stats?.active_disk_files),
    diskSize: formatBytes(stats?.current_disk_usage_bytes),
    logCount: formatCount(logFiles.value?.file_count),
  }
})

function formatCount(value: number | null | undefined) {
  return value == null ? '—' : String(value)
}

function formatBytes(bytes: number | null | undefined) {
  if (bytes == null) return '—'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

async function loadStats() {
  loadingStats.value = true
  try {
    if (props.kind === 'channel-affinity') {
      affinityStats.value = await api.get<ChannelAffinityStats>(
        '/api/option/channel_affinity_cache'
      )
      affinityStatsFailed.value = false
    } else {
      const [stats, logs] = await Promise.all([
        opsMonitorApi.stats(),
        api.get<LogFiles>('/api/performance/logs'),
      ])
      performanceStats.value = stats
      logFiles.value = logs
    }
  } catch (error) {
    if (props.kind === 'channel-affinity') {
      affinityStatsFailed.value = true
    } else {
      // A failed refresh must not leave an earlier cleanable state in place.
      logFiles.value = null
    }
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loadingStats.value = false
  }
}

async function runAction() {
  if (!pendingAction.value) return
  if (pendingAction.value === 'cleanup-logs' && !logEnabled.value) {
    pendingAction.value = null
    return
  }
  loading.value = true
  try {
    switch (pendingAction.value) {
      case 'clear-affinity':
        await api.delete('/api/option/channel_affinity_cache', { all: true })
        break
      case 'clear-cache':
        await api.delete('/api/performance/disk_cache')
        break
      case 'reset-stats':
        await api.post('/api/performance/reset_stats')
        break
      case 'gc':
        await api.post('/api/performance/gc')
        break
      case 'cleanup-logs':
        await api.delete('/api/performance/logs', {
          mode: logCleanupMode.value,
          value: logCleanupValue.value,
        })
        break
    }
    toast.success(t('systemSettings.maintenance.done'))
    pendingAction.value = null
    await loadStats()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}

onMounted(loadStats)
</script>

<template>
  <section
    class="settings-maintenance-actions mt-6 border-t border-dashed border-[var(--border-default)] pt-5 text-sm text-[var(--text-secondary)]"
  >
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="display-title text-sm font-bold text-[var(--text-primary)]">
          {{ t('systemSettings.maintenance.title') }}
        </h3>
        <p
          v-if="loadingStats"
          class="mt-0.5 text-xs text-[var(--text-tertiary)]"
        >
          {{ t('systemSettings.maintenance.loadingStats') }}
        </p>
        <template v-else-if="kind === 'channel-affinity'">
          <p
            v-if="affinityStats"
            class="mt-0.5 text-xs text-[var(--text-tertiary)]"
          >
            {{
              t('systemSettings.maintenance.affinitySummary', {
                total: affinityStats.total,
                capacity: affinityStats.cache_capacity,
              })
            }}
          </p>
          <p
            v-if="affinityStatsFailed"
            role="status"
            class="mt-0.5 text-xs text-[var(--text-tertiary)]"
          >
            {{
              t(
                affinityStats
                  ? 'systemSettings.maintenance.affinityStatsStale'
                  : 'systemSettings.maintenance.affinityStatsUnavailable'
              )
            }}
          </p>
        </template>
        <p v-else class="mt-0.5 text-xs text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.performanceSummary', cacheSummary) }}
        </p>
      </div>
      <ConsoleButton
        variant="ghost"
        size="sm"
        :loading="loadingStats"
        @click="loadStats"
      >
        {{ t('common.refresh') }}
      </ConsoleButton>
    </div>

    <!-- KPI Metric Cards Grid -->
    <div
      v-if="kind === 'channel-affinity' && affinityStats"
      class="my-4 grid grid-cols-2 gap-3 sm:grid-cols-3"
    >
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.cacheEntries') }}
        </div>
        <div
          class="mt-1 font-mono text-xl font-bold text-[var(--text-primary)]"
        >
          {{ affinityStats.total }}
        </div>
      </div>
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.unknownEntries') }}
        </div>
        <div
          class="mt-1 font-mono text-xl font-bold text-[var(--text-primary)]"
        >
          {{ affinityStats.unknown }}
        </div>
      </div>
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.cacheAlgorithm') }}
        </div>
        <div
          class="mt-1 font-mono text-base font-bold text-[var(--text-primary)]"
        >
          {{ affinityStats.cache_algo || '—' }}
        </div>
      </div>
    </div>

    <div
      v-else-if="performanceStats"
      class="my-4 grid grid-cols-2 gap-3 sm:grid-cols-4"
    >
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.diskCache') }}
        </div>
        <div
          class="mt-1 font-mono text-lg font-bold text-[var(--text-primary)]"
        >
          {{
            formatBytes(
              performanceNumber(performanceStats.disk_cache_info, 'total_size')
            )
          }}
        </div>
      </div>
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.memory') }}
        </div>
        <div
          class="mt-1 font-mono text-lg font-bold text-[var(--text-primary)]"
        >
          {{ formatBytes(performanceStats.memory_stats.alloc) }}
        </div>
      </div>
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          Goroutine
        </div>
        <div
          class="mt-1 font-mono text-lg font-bold text-[var(--text-primary)]"
        >
          {{ performanceStats.memory_stats.num_goroutine }}
        </div>
      </div>
      <div class="rounded-xl border border-[var(--border-subtle)] p-3.5">
        <div class="text-xs font-medium text-[var(--text-tertiary)]">
          {{ t('systemSettings.maintenance.logSize') }}
        </div>
        <p
          v-if="logFiles?.enabled === false"
          data-log-disabled
          class="mt-1 text-xs text-[var(--text-secondary)]"
        >
          {{ t('systemSettings.maintenance.logDisabled') }}
        </p>
        <div
          v-else
          class="mt-1 font-mono text-lg font-bold text-[var(--text-primary)]"
        >
          {{ formatBytes(logFiles?.total_size) }}
        </div>
      </div>
    </div>

    <template v-if="kind === 'channel-affinity'">
      <div class="mt-4 flex flex-wrap gap-2.5">
        <ConsoleButton
          variant="secondary"
          size="sm"
          @click="pendingAction = 'clear-affinity'"
        >
          {{ t('systemSettings.maintenance.clearAffinityButton') }}
        </ConsoleButton>
      </div>
    </template>
    <template v-else>
      <div class="mt-4 flex flex-wrap gap-2.5">
        <ConsoleButton
          variant="secondary"
          size="sm"
          @click="pendingAction = 'clear-cache'"
        >
          {{ t('systemSettings.maintenance.clearCache') }}
        </ConsoleButton>
        <ConsoleButton
          variant="secondary"
          size="sm"
          @click="pendingAction = 'reset-stats'"
        >
          {{ t('systemSettings.maintenance.resetStatsButton') }}
        </ConsoleButton>
        <ConsoleButton variant="ghost" size="sm" @click="pendingAction = 'gc'">
          {{ t('systemSettings.maintenance.gc') }}
        </ConsoleButton>
      </div>
      <div
        v-if="logEnabled"
        class="mt-4 flex flex-wrap items-center gap-2.5 rounded-xl border border-[var(--border-subtle)] p-3"
      >
        <span class="text-xs font-medium text-[var(--text-secondary)]">{{
          t('systemSettings.maintenance.logCleanup')
        }}</span>
        <select
          v-model="logCleanupMode"
          class="focus-ring h-8 rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-2.5 text-xs text-[var(--text-primary)]"
          :aria-label="t('systemSettings.maintenance.logCleanupMode')"
        >
          <option value="by_count">
            {{ t('systemSettings.maintenance.byCount') }}
          </option>
          <option value="by_days">
            {{ t('systemSettings.maintenance.byDays') }}
          </option>
        </select>
        <input
          v-model.number="logCleanupValue"
          type="number"
          min="1"
          class="focus-ring h-8 w-20 rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-2.5 text-xs text-[var(--text-primary)]"
          :aria-label="t('systemSettings.maintenance.logCleanupValue')"
        />
        <ConsoleButton
          variant="danger"
          size="sm"
          :disabled="logCleanupValue < 1"
          @click="pendingAction = 'cleanup-logs'"
        >
          {{ t('systemSettings.maintenance.cleanupLogsButton') }}
        </ConsoleButton>
      </div>
    </template>
  </section>

  <ConfirmDialog
    :open="pendingAction !== null"
    :title="actionText.title"
    :message="actionText.message"
    :loading="loading"
    :confirm-text="t('systemSettings.maintenance.confirm')"
    @cancel="pendingAction = null"
    @confirm="runAction"
  />
</template>
