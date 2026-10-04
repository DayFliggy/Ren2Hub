<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { AlertTriangle, Maximize2, Minimize2, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { OpsCheck, OpsOverview } from '@/api/opsMonitor'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
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
const emit = defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

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
// Unmeasured checks are named, so a quiet instance never reads as fully checked.
const unmeasured = computed(() => {
  const names = (props.overview?.checks ?? [])
    .filter((check) => check.status === 'unknown')
    .map((check) => t(`checks.${check.key}`))
  if (!names.length) return ''
  return new Intl.ListFormat(locale.value, {
    style: 'narrow',
    type: 'unit',
  }).format(names)
})
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

/** The API reports per-minute rates; the header reads them per second. */
function perSecond(perMinute: number | null): string {
  if (perMinute === null) return '--'
  return `${formatRate(perMinute / 60, locale.value)}${t('gateway.perSecond')}`
}

const tiles = computed(() => {
  const gateway = props.overview?.gateway
  if (!gateway) return []
  const admission = gateway.admission
  const success = formatPercent(gateway.success_rate, locale.value)
  return [
    {
      label: t('kpi.inFlight'),
      value: formatCount(admission.in_flight, locale.value),
      sub: t('kpi.queued', {
        queued: formatCount(admission.queued, locale.value),
        limit: formatCount(admission.total_queue_size, locale.value),
      }),
    },
    {
      label: t('kpi.granted'),
      value: perSecond(gateway.granted_per_minute),
      sub: t('kpi.grantedSub', {
        total: formatCount(admission.granted_total, locale.value),
      }),
    },
    {
      label: t('kpi.rejected'),
      value: perSecond(gateway.rejected_per_minute),
      sub: t('kpi.rejectedSub'),
    },
    {
      label: t('kpi.successRate'),
      value: gateway.success_rate === null ? '--' : `${success.value}%`,
      sub:
        gateway.success_rate === null
          ? t('kpi.noSamples')
          : t('kpi.successRateSub'),
    },
  ]
})
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
        <div v-if="overview" class="ops-health" aria-hidden="true">
          <MiniRing
            :percent="healthScore ?? 0"
            :color="healthColor"
            :size="46"
            :stroke-width="4.5"
            :indeterminate="healthScore === null"
          >
            <span
              class="ops-health-score font-mono"
              :style="{ color: healthColor }"
            >
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

      <div class="ops-header-controls">
        <span class="ops-updated" aria-live="polite">{{
          updatedAt
            ? t('updatedAt', { time: formatClock(updatedAt, locale) })
            : t('neverLoaded')
        }}</span>
        <span
          v-if="!loading && updatedAt"
          class="ops-countdown-badge font-mono"
          :title="t('hud.autoRefresh', { seconds: countdown })"
        >
          {{ countdown }}s
        </span>
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
        <li
          v-for="check in reasons"
          :key="check.key"
          class="ops-reason"
          data-ops-reason
        >
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
          {{ unmeasured ? t('allMeasuredOk') : t('allChecksOk') }}
        </li>
        <li
          v-if="unmeasured"
          class="ops-reason ops-reason--muted"
          data-ops-unmeasured
        >
          {{ t('unmeasured', { items: unmeasured }) }}
        </li>
      </ul>
      <StatTileGrid
        :tiles="tiles"
        :columns="4"
        :class="{ 'ops-card-stale': Boolean(error) }"
        data-ops-kpis
      />
    </template>
    <div
      v-else-if="!error"
      class="ops-skeleton"
      role="status"
      :aria-label="t('loading')"
    >
      <SkeletonBlock rounded="lg" class="h-7" />
      <SkeletonBlock rounded="lg" class="h-20" />
    </div>
  </section>
</template>
