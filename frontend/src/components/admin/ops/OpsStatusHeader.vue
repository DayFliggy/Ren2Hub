<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { AlertTriangle, Maximize2, Minimize2, RefreshCw } from 'lucide-vue-next'
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
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  STATUS_COLORS,
  STATUS_TONES,
  formatClock,
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRate,
} from '@/utils/opsFormat'

const props = withDefaults(
  defineProps<{
    overview: OpsOverview | null
    loading: boolean
    error: string
    updatedAt: number
    refreshIntervalSeconds?: number
  }>(),
  { refreshIntervalSeconds: 30 }
)
const selectedWindow = defineModel<OpsWindow>('window', { required: true })
const emit = defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const windowOptions = computed(() =>
  OPS_WINDOWS.map((value) => ({ value, label: t(`window.${value}`) }))
)

const healthScore = computed(() => props.overview?.health_score ?? null)

const healthColor = computed(() => {
  if (!props.overview) return 'var(--text-tertiary)'
  return STATUS_COLORS[props.overview.status]
})

const isFullscreen = ref(false)
function toggleFullscreen() {
  if (typeof document === 'undefined') return
  if (!document.fullscreenElement) {
    if (document.documentElement.requestFullscreen) {
      document.documentElement.requestFullscreen().catch(() => {})
    }
    isFullscreen.value = true
  } else {
    if (document.exitFullscreen) {
      document.exitFullscreen().catch(() => {})
    }
    isFullscreen.value = false
  }
}
function onFullscreenChange() {
  isFullscreen.value = Boolean(document.fullscreenElement)
}

const countdown = ref(props.refreshIntervalSeconds)
let timer: number | null = null
function resetCountdown() {
  countdown.value = props.refreshIntervalSeconds
}

watch(
  () => props.updatedAt,
  () => resetCountdown()
)

onMounted(() => {
  if (typeof document !== 'undefined') {
    document.addEventListener('fullscreenchange', onFullscreenChange)
  }
  if (typeof window !== 'undefined') {
    timer = window.setInterval(() => {
      if (props.loading) {
        countdown.value = props.refreshIntervalSeconds
      } else if (countdown.value > 1) {
        countdown.value -= 1
      } else {
        countdown.value = props.refreshIntervalSeconds
      }
    }, 1000)
  }
})

onUnmounted(() => {
  if (typeof document !== 'undefined') {
    document.removeEventListener('fullscreenchange', onFullscreenChange)
  }
  if (timer !== null) {
    clearInterval(timer)
  }
})
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
        <div v-if="overview" class="ops-hud-health" aria-hidden="true">
          <MiniRing
            :percent="healthScore ?? 0"
            :color="healthColor"
            :size="46"
            :stroke-width="4.5"
            :indeterminate="healthScore === null"
          >
            <span class="ops-hud-score" :style="{ color: healthColor }">
              {{ healthScore !== null ? healthScore : '--' }}
            </span>
          </MiniRing>
        </div>
        <div class="ops-header-title-block">
          <div class="ops-header-title-row">
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
          <p class="ops-header-subtitle">{{ t('subtitle') }}</p>
        </div>
      </div>

      <!-- Realtime Telemetry HUD (Sub2API-inspired live pulse stats) -->
      <div
        v-if="overview"
        class="ops-hud-telemetry"
        role="region"
        :aria-label="t('hud.livePulse')"
      >
        <div class="ops-hud-pulse-tag">
          <span class="ops-live-pulse-dot" aria-hidden="true" />
          <span>{{ t('hud.livePulse') }}</span>
        </div>
        <div class="ops-hud-metrics">
          <div class="ops-hud-metric">
            <span class="ops-hud-kpi-label">{{
              t('hud.activeConcurrency')
            }}</span>
            <span class="ops-hud-kpi-value">{{
              formatCount(overview.gateway.admission.in_flight, locale)
            }}</span>
          </div>
          <div class="ops-hud-divider" aria-hidden="true" />
          <div class="ops-hud-metric">
            <span class="ops-hud-kpi-label">{{ t('hud.grantedRate') }}</span>
            <span class="ops-hud-kpi-value">
              {{
                overview.gateway.granted_per_minute !== null
                  ? `${formatRate(overview.gateway.granted_per_minute / 60, locale)}/s`
                  : '--'
              }}
            </span>
          </div>
          <div class="ops-hud-divider" aria-hidden="true" />
          <div class="ops-hud-metric" data-ops-success-rate>
            <span class="ops-hud-kpi-label">{{
              t('gateway.successRate')
            }}</span>
            <span class="ops-hud-kpi-value">
              {{
                overview.gateway.success_rate !== null
                  ? `${formatPercent(overview.gateway.success_rate, locale).value}%`
                  : '--'
              }}
            </span>
          </div>
        </div>
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
        <div class="ops-refresh-group">
          <span class="ops-updated" aria-live="polite">{{
            updatedAt
              ? t('updatedAt', { time: formatClock(updatedAt, locale) })
              : t('neverLoaded')
          }}</span>
          <span
            v-if="!loading && updatedAt"
            class="ops-countdown-badge"
            :title="t('hud.autoRefresh', { seconds: countdown })"
          >
            {{ countdown }}s
          </span>
        </div>
        <IconButton
          :label="loading ? t('refreshing') : t('refresh')"
          :disabled="loading"
          @click="emit('refresh')"
          ><RefreshCw
            :size="16"
            :class="{ 'ops-spin': loading }"
            aria-hidden="true"
        /></IconButton>
        <IconButton
          :label="isFullscreen ? t('hud.exitFullscreen') : t('hud.fullscreen')"
          @click="toggleFullscreen"
          ><component
            :is="isFullscreen ? Minimize2 : Maximize2"
            :size="16"
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
      <details
        class="ops-diagnostics"
        :class="{ 'ops-card-stale': Boolean(error) }"
      >
        <summary class="focus-ring">
          {{ t('diagnostics.title') }}
          <span class="ops-scope">{{ t('scope.process') }}</span>
        </summary>
        <dl class="ops-metric-grid ops-metric-grid--3">
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.queued') }}</dt>
            <dd class="ops-metric-value">
              {{ formatCount(overview.gateway.admission.queued, locale) }}
            </dd>
          </div>
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.queueFull') }}</dt>
            <dd class="ops-metric-value">
              {{
                formatCount(overview.gateway.admission.queue_full_total, locale)
              }}
            </dd>
          </div>
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.waitTimeout') }}</dt>
            <dd class="ops-metric-value">
              {{
                formatCount(
                  overview.gateway.admission.wait_timeout_total,
                  locale
                )
              }}
            </dd>
          </div>
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.retryRejected') }}</dt>
            <dd class="ops-metric-value">
              {{
                formatCount(
                  overview.gateway.retry_budget.rejected_total,
                  locale
                )
              }}
            </dd>
          </div>
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.canceled') }}</dt>
            <dd class="ops-metric-value">
              {{
                formatCount(overview.gateway.admission.canceled_total, locale)
              }}
            </dd>
          </div>
          <div class="ops-metric">
            <dt class="ops-metric-label">{{ t('gateway.pools') }}</dt>
            <dd class="ops-metric-value">
              {{ formatCount(overview.gateway.admission.pools, locale) }}
            </dd>
          </div>
        </dl>
      </details>
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
