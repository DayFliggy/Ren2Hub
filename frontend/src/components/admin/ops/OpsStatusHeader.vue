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

const coverage = computed(() => props.overview?.observation?.coverage ?? null)
const coveragePercent = computed(() =>
  coverage.value && coverage.value.total > 0
    ? (100 * coverage.value.measured) / coverage.value.total
    : null
)

const healthColor = computed(() => {
  if (!props.overview) return 'var(--text-tertiary)'
  return STATUS_COLORS[props.overview.status]
})

const isFullscreen = ref(false)
const fullscreenError = ref('')
async function toggleFullscreen() {
  fullscreenError.value = ''
  try {
    if (document.fullscreenElement) await document.exitFullscreen()
    else {
      if (!document.documentElement.requestFullscreen)
        throw new Error(t('hud.unavailable'))
      await document.documentElement.requestFullscreen()
    }
    onFullscreenChange()
  } catch (cause) {
    fullscreenError.value =
      cause instanceof Error ? cause.message : String(cause)
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
    .filter(
      (check) =>
        ['unknown', 'stale', 'disabled'].includes(check.status) &&
        check.unavailable_reason !== 'not_provided'
    )
    .map((check) => t(`checks.${check.key}`))
  if (!names.length) return ''
  return new Intl.ListFormat(locale.value, {
    style: 'narrow',
    type: 'conjunction',
  }).format(names)
})
const notProvided = computed(() =>
  (props.overview?.checks ?? [])
    .filter((check) => check.unavailable_reason === 'not_provided')
    .map((check) => t(`checks.${check.key}`))
    .join(' / ')
)
/** Check values carry the unit of their key. */
function checkValue(check: OpsCheck, value: number): string {
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
  if (check.reason) return t(`observationReasons.${check.reason}`)
  if (check.value === null) return t(`status.${check.status}`)
  const value = checkValue(check, check.value)
  if (check.threshold === null) return value
  if (check.threshold <= 0) return `${value} · ${t('disabledLimit')}`
  return `${value} / ${checkValue(check, check.threshold)}`
}

const tiles = computed(() => {
  const gateway = props.overview?.gateway
  const total = (rows: { in_flight: number }[] | undefined) =>
    rows ? rows.reduce((sum, row) => sum + row.in_flight, 0) : null
  return [
    {
      label: t('native.userInFlight'),
      value: formatCount(total(gateway?.users), locale.value),
      sub: t('native.logicalRequests'),
    },
    {
      label: t('native.channelInFlight'),
      value: formatCount(total(gateway?.channels), locale.value),
      sub: t('native.upstreamAttempts'),
    },
    {
      label: t('native.sampleCount'),
      value: formatCount(
        gateway
          ? gateway.observations.reduce((sum, row) => sum + row.samples, 0)
          : null,
        locale.value
      ),
      sub: t('native.rollingSamples'),
    },
    {
      label: t('native.knownObservations'),
      value: formatCount(
        gateway ? gateway.observations.filter((row) => row.known).length : null,
        locale.value
      ),
      sub: t('native.minimumSamples'),
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
            :percent="coveragePercent ?? 0"
            :color="healthColor"
            :size="46"
            :stroke-width="4.5"
            :indeterminate="coveragePercent === null"
          >
            <span
              class="ops-health-score font-mono"
              :style="{ color: healthColor }"
            >
              {{ coverage ? `${coverage.measured}/${coverage.total}` : '--' }}
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
              >{{
                overview.status === 'ok' && notProvided
                  ? t('allMeasuredOk')
                  : t(`status.${overview.status}`)
              }}</StatusChip
            >
          </div>
          <p class="ops-header-subtitle">{{ t('subtitle') }}</p>
          <p v-if="coverage" class="ops-note" data-ops-coverage>
            {{ t('coverage', coverage) }}
          </p>
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

    <p v-if="fullscreenError" class="ops-inline-error" role="alert">
      {{ fullscreenError }}
    </p>
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
          {{
            unmeasured || notProvided ? t('allMeasuredOk') : t('allChecksOk')
          }}
        </li>
        <li
          v-if="unmeasured"
          class="ops-reason ops-reason--muted"
          data-ops-unmeasured
        >
          {{ t('unmeasured', { items: unmeasured }) }}
        </li>
        <li
          v-if="notProvided"
          class="ops-reason ops-reason--muted"
          data-ops-not-provided
        >
          {{ t('notProvidedItems', { items: notProvided }) }}
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
