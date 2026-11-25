<script setup lang="ts">
import { computed, inject, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  Activity,
  AlertTriangle,
  Maximize2,
  Minimize2,
  Pause,
  Play,
  RefreshCw,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { routeLocationKey, routerKey } from 'vue-router'
import type {
  OpsCheck,
  OpsOverview,
  OpsQualityHours,
  PerformanceQuality,
} from '@/api/opsMonitor'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatTileGrid, {
  type StatTile,
} from '@/components/common/StatTileGrid.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { verdictTone } from '@/utils/loadTone'
import {
  STATUS_COLORS,
  STATUS_TEXT_COLORS,
  STATUS_TONES,
  durationParts,
  formatClock,
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRate,
} from '@/utils/opsFormat'
import OpsDiagnosisDrawer from './OpsDiagnosisDrawer.vue'

export type PollingIntervalChoice = 10 | 30 | 60

const props = withDefaults(
  defineProps<{
    overview: OpsOverview | null
    quality?: PerformanceQuality | null
    qualityHours?: OpsQualityHours
    channelNames?: Record<number, string>
    loading: boolean
    error: string
    updatedAt: number
    refreshIntervalSeconds?: number
    paused?: boolean
  }>(),
  {
    quality: null,
    qualityHours: 24,
    channelNames: () => ({}),
    refreshIntervalSeconds: 30,
    paused: false,
  }
)
const emit = defineEmits<{
  refresh: []
  'update:refreshIntervalSeconds': [seconds: PollingIntervalChoice]
  'update:paused': [paused: boolean]
}>()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const nowMs = ref(Date.now())
const diagnosisOpen = ref(false)

const coverage = computed(() => props.overview?.observation?.coverage ?? null)
const coveragePercent = computed(() =>
  coverage.value && coverage.value.total > 0
    ? (100 * coverage.value.measured) / coverage.value.total
    : null
)

const healthRingColor = computed(() => {
  if (!props.overview) return 'var(--text-tertiary)'
  return STATUS_COLORS[props.overview.status]
})

const healthTextColor = computed(() => {
  if (!props.overview) return 'var(--text-tertiary)'
  return STATUS_TEXT_COLORS[props.overview.status]
})

const uptimeLabel = computed(() => {
  const startedAt = props.overview?.instance_started_at
  if (!startedAt || startedAt <= 0) return ''
  const referenceSec =
    props.overview?.sampled_at && props.overview.sampled_at > startedAt
      ? props.overview.sampled_at
      : Math.floor(nowMs.value / 1000)
  const elapsed = Math.max(0, referenceSec - startedAt)
  if (elapsed <= 0) return ''
  const text = durationParts(elapsed)
    .map((part) => t(`duration.${part.unit}`, { n: part.amount }))
    .join(' ')
  return t('uptime', { duration: text })
})

const nativeFullscreen = ref(false)
const routeFullscreen = computed(
  () => route?.query?.fullscreen === '1' || route?.query?.fullscreen === 'true'
)
const isFullscreen = computed(
  () => nativeFullscreen.value || routeFullscreen.value
)
const fullscreenError = ref('')

function setRouteFullscreen(active: boolean) {
  if (!router || !route) return
  const nextQuery = { ...route.query }
  if (active) nextQuery.fullscreen = '1'
  else delete nextQuery.fullscreen
  void router.replace({ query: nextQuery })
}

async function toggleFullscreen() {
  fullscreenError.value = ''
  try {
    if (isFullscreen.value) {
      if (document.fullscreenElement) await document.exitFullscreen()
      setRouteFullscreen(false)
    } else {
      if (!document.documentElement.requestFullscreen)
        throw new Error(t('hud.unavailable'))
      await document.documentElement.requestFullscreen()
      setRouteFullscreen(true)
    }
    onFullscreenChange()
  } catch (cause) {
    fullscreenError.value =
      cause instanceof Error ? cause.message : String(cause)
  }
}

function onFullscreenChange() {
  nativeFullscreen.value = Boolean(document.fullscreenElement)
  if (!nativeFullscreen.value && routeFullscreen.value) {
    setRouteFullscreen(false)
  }
}

function onKeydown(event: KeyboardEvent) {
  if (
    event.key === 'Escape' &&
    routeFullscreen.value &&
    !document.fullscreenElement
  ) {
    setRouteFullscreen(false)
  }
}

const countdown = ref(props.refreshIntervalSeconds)
let timer: number | null = null
function resetCountdown() {
  countdown.value = props.refreshIntervalSeconds
}

watch([() => props.updatedAt, () => props.refreshIntervalSeconds], () =>
  resetCountdown()
)

onMounted(() => {
  if (typeof document !== 'undefined') {
    document.addEventListener('fullscreenchange', onFullscreenChange)
    document.addEventListener('keydown', onKeydown)
  }
  if (typeof window !== 'undefined') {
    timer = window.setInterval(() => {
      nowMs.value = Date.now()
      if (props.paused || props.loading) {
        if (props.loading) countdown.value = props.refreshIntervalSeconds
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
    document.removeEventListener('keydown', onKeydown)
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

const activeBreakers = computed(() => props.overview?.gateway?.breakers ?? [])
const activeCooldowns = computed(() => props.overview?.gateway?.cooldowns ?? [])
const issueCount = computed(
  () =>
    reasons.value.length +
    activeBreakers.value.length +
    activeCooldowns.value.length
)
const hasCriticalIssue = computed(
  () =>
    reasons.value.some((check) => check.status === 'critical') ||
    activeBreakers.value.some((item) => item.state === 'open')
)

function formatList(names: string[]): string {
  if (!names.length) return ''
  return new Intl.ListFormat(locale.value, {
    style: 'narrow',
    type: 'conjunction',
  }).format(names)
}

// Separate truly missing readings from resources whose threshold check is off,
// so a measured resource with a disabled threshold never reads as "No data".
const unmeasured = computed(() =>
  formatList(
    (props.overview?.checks ?? [])
      .filter(
        (check) =>
          check.value === null &&
          ['unknown', 'stale', 'disabled'].includes(check.status) &&
          check.unavailable_reason !== 'not_provided'
      )
      .map((check) => t(`checks.${check.key}`))
  )
)

const uncheckedChecks = computed(() =>
  formatList(
    (props.overview?.checks ?? [])
      .filter(
        (check) =>
          check.value !== null &&
          check.status === 'disabled' &&
          check.unavailable_reason !== 'not_provided'
      )
      .map((check) => t(`checks.${check.key}`))
  )
)

const unmeasuredSummary = computed(() =>
  [
    unmeasured.value ? t('unmeasured', { items: unmeasured.value }) : '',
    uncheckedChecks.value
      ? t('uncheckedChecks', { items: uncheckedChecks.value })
      : '',
  ]
    .filter(Boolean)
    .join(' · ')
)

const notProvided = computed(() =>
  (props.overview?.checks ?? [])
    .filter((check) => check.unavailable_reason === 'not_provided')
    .map((check) => t(`checks.${check.key}`))
    .join(' / ')
)

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

interface InfraChip {
  key: string
  label: string
  reading: string
  dotColor: string
  targetSection: 'resources' | 'storage'
}

const infraChips = computed<InfraChip[]>(() => {
  const obs = props.overview?.observation
  if (!obs) return []
  const chips: InfraChip[] = []
  for (const key of ['cpu', 'memory', 'disk'] as const) {
    const res = obs.resources[key]
    const check = props.overview?.checks.find((item) => item.key === key)
    const status = check?.status ?? res.state
    const tone = verdictTone(
      status === 'disabled' ? 'unchecked' : status,
      res.percent
    )
    const reading =
      res.percent !== null
        ? `${formatPercent(res.percent, locale.value).value}%`
        : t(`status.${status}`)
    chips.push({
      key,
      label: t(`checks.${key}`),
      reading,
      dotColor: tone.fill,
      targetSection: 'resources',
    })
  }
  const addProbe = (
    key: 'database' | 'log_database' | 'redis',
    probe: { status: OpsCheck['status']; latency_ms: number | null }
  ) => {
    const latency = formatLatencyMs(probe.latency_ms, locale.value)
    const reading =
      probe.latency_ms !== null
        ? `${latency.value} ${latency.unit}`
        : t(`status.${probe.status}`)
    chips.push({
      key,
      label: t(`checks.${key}`),
      reading,
      dotColor: STATUS_COLORS[probe.status],
      targetSection: 'storage',
    })
  }
  addProbe('database', obs.storage.database)
  if (obs.storage.log_database) {
    addProbe('log_database', obs.storage.log_database)
  }
  addProbe('redis', obs.storage.redis)
  return chips
})

function scrollToSection(sectionName: 'resources' | 'storage') {
  if (typeof document === 'undefined') return
  const target = document.querySelector(`[data-ops-section="${sectionName}"]`)
  target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

const tiles = computed<StatTile[]>(() => {
  const gateway = props.overview?.gateway
  const sumInFlight = (rows: { in_flight: number }[] | undefined) =>
    rows ? rows.reduce((sum, row) => sum + row.in_flight, 0) : null
  const userInFlight = sumInFlight(gateway?.users)
  const channelInFlight = sumInFlight(gateway?.channels)

  const qualitySummary = props.quality?.enabled ? props.quality.summary : null
  const rateDisplay = formatPercent(
    qualitySummary?.success_rate ?? null,
    locale.value
  )
  const rateTone: StatTile['tone'] =
    qualitySummary == null
      ? 'default'
      : qualitySummary.success_rate >= 99
        ? 'success'
        : qualitySummary.success_rate >= 95
          ? 'warning'
          : 'danger'

  const latencyDisplay = formatLatencyMs(
    qualitySummary?.avg_latency_ms ?? null,
    locale.value
  )
  const tpsText =
    qualitySummary?.avg_tps == null
      ? '--'
      : `${formatRate(qualitySummary.avg_tps, locale.value)} ${t('quality.tps')}`

  const breakerCount = activeBreakers.value.length
  const cooldownCount = activeCooldowns.value.length
  const disruptionTone: StatTile['tone'] =
    breakerCount > 0 ? 'danger' : cooldownCount > 0 ? 'warning' : 'success'

  return [
    {
      label: t('noc.inFlight'),
      value: formatCount(userInFlight, locale.value),
      sub: t('noc.inFlightSub', {
        users: formatCount(userInFlight, locale.value),
        channels: formatCount(channelInFlight, locale.value),
      }),
    },
    {
      label: t('noc.qualityRate', { hours: props.qualityHours }),
      value:
        rateDisplay.value === '--'
          ? '--'
          : `${rateDisplay.value}${rateDisplay.unit}`,
      sub: t('noc.qualityRequests', {
        count: formatCount(qualitySummary?.requests ?? null, locale.value),
      }),
      tone: rateTone,
    },
    {
      label: t('noc.latencyThroughput'),
      value:
        latencyDisplay.value === '--'
          ? '--'
          : `${latencyDisplay.value} ${latencyDisplay.unit}`,
      sub: t('noc.latencyThroughputSub', { tps: tpsText }),
    },
    {
      label: t('noc.breakersCooldowns'),
      value:
        gateway == null
          ? '--'
          : formatCount(breakerCount + cooldownCount, locale.value),
      sub:
        breakerCount + cooldownCount > 0
          ? t('noc.breakersCooldownsSub', {
              breakers: formatCount(breakerCount, locale.value),
              cooldowns: formatCount(cooldownCount, locale.value),
            })
          : t('noc.allClearSub'),
      tone: gateway == null ? 'default' : disruptionTone,
    },
  ]
})

function onIntervalChange(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value)
  if (value === 10 || value === 30 || value === 60) {
    emit('update:refreshIntervalSeconds', value)
  }
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
        <div v-if="overview" class="ops-health" aria-hidden="true">
          <MiniRing
            :percent="coveragePercent ?? 0"
            :color="healthRingColor"
            :size="46"
            :stroke-width="4.5"
            :indeterminate="coveragePercent === null"
          >
            <span
              class="ops-health-score display-number font-mono"
              :style="{ color: healthTextColor }"
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
            <span v-if="uptimeLabel" class="ops-uptime-badge font-mono">{{
              uptimeLabel
            }}</span>
            <button
              v-if="issueCount > 0"
              type="button"
              class="ops-diagnosis-trigger focus-ring touch-snappy"
              :class="{ 'ops-diagnosis-trigger--critical': hasCriticalIssue }"
              data-ops-diagnosis-trigger
              @click="diagnosisOpen = true"
            >
              <Activity :size="14" aria-hidden="true" />
              <span>{{ t('diagnosis.button', { count: issueCount }) }}</span>
            </button>
          </div>
          <div class="ops-header-meta">
            <p class="ops-header-subtitle">{{ t('subtitle') }}</p>
            <p v-if="coverage" class="ops-note" data-ops-coverage>
              {{ t('coverage', coverage) }}
            </p>
          </div>
        </div>
      </div>

      <div class="ops-header-controls">
        <div class="ops-header-status-meta">
          <span class="ops-updated" aria-live="polite">{{
            updatedAt
              ? t('updatedAt', { time: formatClock(updatedAt, locale) })
              : t('neverLoaded')
          }}</span>
          <span
            v-if="!loading && updatedAt"
            class="ops-countdown-badge font-mono"
            :title="
              paused
                ? t('polling.paused')
                : t('hud.autoRefresh', { seconds: countdown })
            "
          >
            {{ paused ? t('polling.paused') : `${countdown}s` }}
          </span>
        </div>
        <div class="ops-header-actions">
          <select
            class="ops-interval-select focus-ring"
            :aria-label="t('polling.label')"
            :value="refreshIntervalSeconds"
            @change="onIntervalChange"
          >
            <option value="10">{{ t('polling.10') }}</option>
            <option value="30">{{ t('polling.30') }}</option>
            <option value="60">{{ t('polling.60') }}</option>
          </select>
          <IconButton
            :label="paused ? t('polling.resume') : t('polling.pause')"
            @click="emit('update:paused', !paused)"
            ><component
              :is="paused ? Play : Pause"
              :size="15"
              aria-hidden="true"
          /></IconButton>
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
            :label="
              isFullscreen ? t('hud.exitFullscreen') : t('hud.fullscreen')
            "
            @click="toggleFullscreen"
            ><component
              :is="isFullscreen ? Minimize2 : Maximize2"
              :size="16"
              aria-hidden="true"
          /></IconButton>
        </div>
      </div>
    </div>

    <div class="ink-divider" aria-hidden="true" />

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
      <div
        v-if="infraChips.length"
        class="ops-infra-strip"
        :class="{ 'ops-card-stale': Boolean(error) }"
        :aria-label="t('infraStrip')"
        data-ops-infra-strip
      >
        <button
          v-for="chip in infraChips"
          :key="chip.key"
          type="button"
          class="ops-infra-chip focus-ring"
          @click="scrollToSection(chip.targetSection)"
        >
          <span
            class="ops-dot"
            :style="{ background: chip.dotColor }"
            aria-hidden="true"
          />
          <span class="ops-infra-chip-label">{{ chip.label }}</span>
          <span class="ops-infra-chip-value font-mono">{{ chip.reading }}</span>
        </button>
      </div>

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
            unmeasuredSummary || notProvided
              ? t('allMeasuredOk')
              : t('allChecksOk')
          }}
        </li>
        <li
          v-if="unmeasuredSummary"
          class="ops-reason ops-reason--muted"
          data-ops-unmeasured
        >
          {{ unmeasuredSummary }}
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

    <OpsDiagnosisDrawer
      :open="diagnosisOpen"
      :checks="reasons"
      :breakers="activeBreakers"
      :cooldowns="activeCooldowns"
      :channel-names="channelNames"
      :now-ms="nowMs"
      @close="diagnosisOpen = false"
    />
  </section>
</template>
