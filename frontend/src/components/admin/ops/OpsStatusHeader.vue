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
  durationParts,
  formatClock,
  formatCount,
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

const healthScore = computed<number | null>(() => {
  if (!props.overview) return null
  if (props.overview.status === 'ok') return 100
  const criticals = (props.overview.checks ?? []).filter(
    (c) => c.status === 'critical'
  ).length
  const warnings = (props.overview.checks ?? []).filter(
    (c) => c.status === 'warning'
  ).length
  if (props.overview.status === 'critical') {
    return Math.max(10, 60 - criticals * 20 - warnings * 5)
  }
  if (props.overview.status === 'warning') {
    return Math.max(55, 95 - warnings * 10)
  }
  return null
})

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

const countdown = ref(10)
let timer: number | null = null
function resetCountdown() {
  countdown.value = 10
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
        countdown.value = 10
      } else if (countdown.value > 1) {
        countdown.value -= 1
      } else {
        countdown.value = 10
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
            <span class="ops-hud-kpi-value"
              >{{
                formatRate(
                  (overview.gateway.granted_per_minute ?? 0) / 60,
                  locale
                )
              }}/s</span
            >
          </div>
          <div class="ops-hud-divider" aria-hidden="true" />
          <div class="ops-hud-metric">
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
