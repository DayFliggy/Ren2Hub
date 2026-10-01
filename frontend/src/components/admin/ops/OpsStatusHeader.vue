<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import {
  OPS_WINDOWS,
  type OpsCheck,
  type OpsOverview,
  type OpsWindow,
} from '@/api/opsMonitor'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  STATUS_COLORS,
  STATUS_TONES,
  durationParts,
  formatClock,
  formatLatencyMs,
  formatPercent,
  formatRate,
  formatTimestamp,
} from '@/utils/opsFormat'

const props = defineProps<{
  overview: OpsOverview | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const selectedWindow = defineModel<OpsWindow>('window', { required: true })
const emit = defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const windowOptions = computed(() =>
  OPS_WINDOWS.map((value) => ({ value, label: t(`window.${value}`) }))
)
// The roll-up ignores unmeasured checks, so only graded problems are reasons.
const reasons = computed(() =>
  (props.overview?.checks ?? [])
    .filter(
      (check) => check.status === 'critical' || check.status === 'warning'
    )
    .sort(
      (a, b) =>
        Number(b.status === 'critical') - Number(a.status === 'critical')
    )
)
const uptime = computed(() =>
  props.overview
    ? durationParts(props.overview.runtime.uptime_seconds)
        .map((part) => t(`duration.${part.unit}`, { n: part.amount }))
        .join(' ')
    : '--'
)

/** Check values carry the unit of their key. */
function checkValue(check: OpsCheck, value: number): string {
  if (check.key === 'admission')
    return `${formatRate(value / 60, locale.value)}${t('gateway.perSecond')}`
  if (
    check.key === 'database' ||
    check.key === 'log_database' ||
    check.key === 'redis'
  ) {
    const latency = formatLatencyMs(value, locale.value)
    return `${latency.value} ${latency.unit}`
  }
  return `${formatPercent(value, locale.value).value}%`
}

function checkDetail(check: OpsCheck): string {
  if (check.key === 'draining' || check.key === 'master')
    return t(`checkDetail.${check.key}`)
  if (check.error) return t(`errors.${check.error}`)
  if (check.value === null) return t(`status.${check.status}`)
  const value = checkValue(check, check.value)
  if (check.threshold === null) return value
  if (check.threshold === 0) return `${value} · ${t('disabledLimit')}`
  return `${value} / ${checkValue(check, check.threshold)}`
}
</script>

<template>
  <section
    class="ops-header"
    :aria-label="t('title')"
    :aria-busy="loading"
    data-ops-section="status"
  >
    <div class="ops-header-top">
      <div class="ops-header-heading">
        <h1 class="display-title">{{ t('title') }}</h1>
        <StatusChip
          v-if="overview"
          :tone="STATUS_TONES[overview.status]"
          data-ops-overall-status
          :data-status="overview.status"
          ><span class="sr-only">{{ t('statusLabel') }}: </span
          >{{ t(`status.${overview.status}`) }}</StatusChip
        >
      </div>
      <div class="ops-header-controls">
        <div class="ops-window">
          <SegmentedToggle
            v-model="selectedWindow"
            :options="windowOptions"
            :label="t('window.label')"
            size="sm"
          />
          <span class="ops-window-note">{{ t('window.note') }}</span>
        </div>
        <span class="ops-updated" aria-live="polite">{{
          updatedAt
            ? t('updatedAt', { time: formatClock(updatedAt, locale) })
            : t('neverLoaded')
        }}</span>
        <IconButton
          :label="loading ? t('refreshing') : t('refresh')"
          :disabled="loading"
          @click="emit('refresh')"
          ><RefreshCw
            :size="16"
            :class="{ 'ops-spin': loading }"
            aria-hidden="true"
        /></IconButton>
      </div>
    </div>

    <div v-if="error" class="ops-inline-error" role="alert" data-ops-error>
      <AlertTriangle :size="15" aria-hidden="true" />
      <div class="min-w-0 flex-1">
        <p class="ops-inline-error-title">
          {{ overview ? t('stale') : t('loadFailed') }}
        </p>
        <p class="ops-inline-error-detail">{{ error }}</p>
        <p class="ops-inline-error-detail">
          {{
            updatedAt
              ? t('lastSuccess', { time: formatClock(updatedAt, locale) })
              : t('neverLoaded')
          }}
        </p>
      </div>
      <ConsoleButton
        variant="secondary"
        size="sm"
        :loading="loading"
        @click="emit('refresh')"
        >{{ t('retry') }}</ConsoleButton
      >
    </div>

    <template v-if="overview">
      <ul
        class="ops-reasons"
        :class="{ 'ops-card-stale': Boolean(error) }"
        data-ops-reasons
      >
        <li v-for="check in reasons" :key="check.key" class="ops-reason">
          <span
            class="ops-dot"
            :style="{ background: STATUS_COLORS[check.status] }"
            aria-hidden="true"
          /><strong>{{ t(`checks.${check.key}`) }}</strong
          ><span class="sr-only">({{ t(`status.${check.status}`) }})</span
          ><span>{{ checkDetail(check) }}</span>
        </li>
        <li
          v-if="!reasons.length && overview.status === 'ok'"
          class="ops-reason ops-reason--ok"
        >
          {{ t('allChecksOk') }}
        </li>
      </ul>
      <dl class="ops-runtime" :class="{ 'ops-card-stale': Boolean(error) }">
        <div>
          <dt>{{ t('runtime.version') }}</dt>
          <dd>
            <code>{{ overview.runtime.version || '--' }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t('runtime.node') }}</dt>
          <dd>{{ overview.runtime.node_name || '--' }}</dd>
        </div>
        <div>
          <dt>{{ t('runtime.role') }}</dt>
          <dd>
            {{ t(overview.runtime.master ? 'runtime.master' : 'runtime.slave')
            }}<template v-if="overview.runtime.draining">
              · {{ t('runtime.draining') }}</template
            >
          </dd>
        </div>
        <div>
          <dt>{{ t('runtime.platform') }}</dt>
          <dd>
            <code
              >{{ overview.runtime.goos }}/{{ overview.runtime.goarch }} ·
              {{ overview.runtime.go_version }}</code
            >
          </dd>
        </div>
        <div>
          <dt>{{ t('runtime.uptime') }}</dt>
          <dd
            :title="
              t('runtime.started', {
                time: formatTimestamp(overview.runtime.started_at, locale),
              })
            "
          >
            {{ uptime }}
          </dd>
        </div>
      </dl>
    </template>
    <div
      v-else-if="!error"
      class="ops-skeleton"
      role="status"
      :aria-label="t('loading')"
    >
      <SkeletonBlock rounded="lg" class="h-7" />
      <SkeletonBlock rounded="lg" class="h-10" />
    </div>
  </section>
</template>
