<template>
  <aside
    class="signal-console pointer-events-none font-mono"
    :class="`signal-console--${snapshot.phase}`"
    :data-phase="snapshot.phase"
    :aria-label="t('signalConsole.a11y')"
  >
    <div class="signal-console__rail" aria-hidden="true" />

    <div class="signal-console__body">
      <!-- The only interactive part of the rail. It deliberately absorbs map input. -->
      <section
        ref="traceInspectorEl"
        class="signal-console__telemetry-head"
        tabindex="0"
        data-canvas-interaction-boundary
        aria-label="Live request telemetry. Hold for three seconds to inspect full request traces."
        @pointerenter="onInspectorPointerEnter"
        @pointerleave="onInspectorPointerLeave"
        @focusin="onInspectorFocusIn"
        @focusout="onInspectorFocusOut"
        @mousemove.stop
        @pointerdown.stop
        @click.stop
        @keydown.esc.stop.prevent="closeTraceDrawer"
      >
        <p class="signal-console__eyebrow">
          <span class="signal-console__live-dot" />
          <span>{{ t('signalConsole.live') }}</span>
          <span class="signal-console__phase"
            >[{{ phaseLabel(snapshot.phase) }}]</span
          >
        </p>

        <div class="signal-console__telemetry" aria-live="polite">
          <p class="signal-console__telemetry-row">
            <span class="signal-console__telemetry-index">[01]</span>
            <span class="signal-console__telemetry-label">INBOUND</span>
            <span class="signal-console__telemetry-value">{{
              inboundLine
            }}</span>
          </p>
          <p class="signal-console__telemetry-row is-target">
            <span class="signal-console__telemetry-index">[02]</span>
            <span class="signal-console__telemetry-label">TARGET</span>
            <span class="signal-console__telemetry-value">{{
              targetLine
            }}</span>
          </p>
          <p class="signal-console__telemetry-row is-more">
            <span class="signal-console__telemetry-index">[..]</span>
            <span class="signal-console__telemetry-label">MORE</span>
            <span class="signal-console__telemetry-value">{{ moreLine }}</span>
            <span class="signal-console__more-dots" aria-hidden="true"
              >. . .</span
            >
          </p>
        </div>

        <Transition name="signal-trace-drawer">
          <div
            v-if="traceDrawerOpen"
            class="signal-console__trace-drawer"
            aria-live="polite"
          >
            <p class="signal-console__trace-drawer-title">
              <span>TRACE INDEX</span>
              <span>{{ formatIndex(drawerTraces.length) }} RECORDS</span>
            </p>

            <ol v-if="drawerTraces.length" class="signal-console__trace-list">
              <li
                v-for="trace in drawerTraces"
                :key="trace.id"
                class="signal-console__trace-record"
                :class="{
                  'is-focus': trace.id === focusedTrace?.id,
                  'is-complete': Boolean(trace.completedAt),
                  'is-pointer': trace.origin === 'pointer',
                }"
              >
                <p class="signal-console__trace-meta">
                  <span
                    >[{{ trace.origin === 'pointer' ? 'PTR' : 'AUTO' }}]</span
                  >
                  <span>{{ phaseLabel(trace.phase) }}</span>
                  <span
                    v-if="trace.completedAt"
                    class="signal-console__trace-latency"
                  >
                    {{ formatLatency(trace.latencyMs) }}
                  </span>
                </p>
                <p class="signal-console__trace-chain">
                  <span class="signal-console__trace-source">{{
                    sourceLine(trace)
                  }}</span>
                  <span class="signal-console__trace-arrow" aria-hidden="true"
                    >→</span
                  >
                  <span class="signal-console__trace-hub">{{
                    trace.hubLabel || 'MODEL GATEWAY'
                  }}</span>
                  <template
                    v-for="hop in trace.hops"
                    :key="`${trace.id}-${hop.modelId}`"
                  >
                    <span class="signal-console__trace-arrow" aria-hidden="true"
                      >→</span
                    >
                    <span
                      class="signal-console__trace-hop"
                      :data-state="hop.state"
                      >{{ hop.modelName }}</span
                    >
                  </template>
                  <template v-if="trace.completedAt">
                    <span class="signal-console__trace-arrow" aria-hidden="true"
                      >→</span
                    >
                    <span class="signal-console__trace-response"
                      >RESP {{ formatLatency(trace.latencyMs) }}</span
                    >
                  </template>
                </p>
              </li>
            </ol>
            <p v-else class="signal-console__trace-empty">
              NO ROUTES BUFFERED // STANDING BY
            </p>
          </div>
        </Transition>
      </section>

      <!-- An intentional clear field: this is reserved for the live Canvas hub and routes. -->
      <div class="signal-console__canvas-window" aria-hidden="true" />

      <!-- Lower deck: the ledger stays anchored so the map has room to breathe. -->
      <div class="signal-console__lower-deck">
        <div class="signal-console__rule" aria-hidden="true">
          . . . . . . . . . . . . . . . .
        </div>

        <ul class="signal-console__capabilities">
          <li
            v-for="(capability, index) in SIGNAL_CAPABILITIES"
            :key="capability.id"
            class="signal-console__capability"
            :class="{ 'is-active': capability.id === phaseCapability }"
          >
            <span class="signal-console__capability-index" aria-hidden="true"
              >[{{ formatIndex(index + 1) }}]</span
            >
            <span class="signal-console__node" aria-hidden="true">
              {{ capability.id === phaseCapability ? '[+]' : '[ ]' }}
            </span>
            <span>
              <span class="signal-console__capability-title">{{
                t(capability.titleKey)
              }}</span>
              <span class="signal-console__capability-detail">{{
                t(capability.detailKey)
              }}</span>
            </span>
          </li>
        </ul>

        <div class="signal-console__baseline">
          <SignalDotMatrix
            compact
            :phase="snapshot.phase"
            :active-capability="phaseCapability"
            :active-requests="snapshot.activeRequests ?? 0"
            :target-count="snapshot.targetCount ?? 0"
          />
          <div class="signal-console__baseline-readout">
            <p class="signal-console__origin" :title="countryTitle">
              <span class="signal-console__origin-label">{{
                countryPrefix
              }}</span>
              <span class="signal-console__origin-value">{{
                countryLine
              }}</span>
            </p>
            <SignalLatencyMeter
              :average-latency-ms="snapshot.averageLatencyMs"
              :tier="snapshot.latencyTier"
              :event-id="snapshot.latencyEventId"
            />
          </div>
        </div>
      </div>
    </div>

    <p class="sr-only">{{ screenReaderStatus }}</p>
  </aside>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import SignalDotMatrix from './SignalDotMatrix.vue'
import SignalLatencyMeter from './SignalLatencyMeter.vue'
import {
  SIGNAL_CAPABILITIES,
  type SignalCapabilityId,
  type SignalConsolePhase,
  type SignalConsoleSnapshot,
  type SignalRequestTrace,
} from '@/data/signalConsole'

const props = defineProps<{
  snapshot: SignalConsoleSnapshot
}>()

const { t } = useI18n()
const traceInspectorEl = ref<HTMLElement | null>(null)
const traceDrawerOpen = ref(false)
const pointerWithinInspector = ref(false)
const focusWithinInspector = ref(false)
let traceOpenTimer: number | undefined
let focusCheckTimer: number | undefined

const formatIndex = (index: number) =>
  String(Math.max(0, index)).padStart(2, '0')

const phaseLabel = (phase: SignalConsolePhase) =>
  phase.replace('_', ' ').toUpperCase()

const phaseCapability = computed<SignalCapabilityId>(() => {
  switch (props.snapshot.phase) {
    case 'gather':
    case 'hub_hold':
      return 'access'
    case 'route':
      return 'observe'
    case 'respond':
    case 'settle':
      return 'billing'
    case 'idle':
    default:
      return 'governance'
  }
})

const allTraces = computed(() => props.snapshot.traces ?? [])
const activeTraces = computed(() =>
  allTraces.value.filter((trace) => !trace.completedAt)
)
const recentTraces = computed(() =>
  allTraces.value
    .filter((trace) => Boolean(trace.completedAt))
    .sort((left, right) => (right.completedAt ?? 0) - (left.completedAt ?? 0))
    .slice(0, 4)
)
const drawerTraces = computed(() => [
  ...activeTraces.value,
  ...recentTraces.value,
])

const focusedTrace = computed(() => {
  const traces = allTraces.value
  const focused = props.snapshot.focusRequestId

  return (
    (focused ? traces.find((trace) => trace.id === focused) : undefined) ??
    activeTraces.value.find((trace) => trace.origin === 'pointer') ??
    activeTraces.value[0] ??
    recentTraces.value[0]
  )
})

const countryCodes = computed(() => {
  const trace = focusedTrace.value
  if (!trace) return []

  return [
    ...new Set(
      trace.sources
        .map((source) => source.country)
        .filter((country): country is string => Boolean(country))
    ),
  ]
})

const countryLine = computed(() =>
  countryCodes.value.length ? countryCodes.value.join(' + ') : 'GLOBAL'
)
const countryPrefix = computed(() =>
  activeTraces.value.length ? 'FROM' : 'LAST'
)
const countryTitle = computed(() => {
  if (!countryCodes.value.length) return 'No active request country'
  return `${countryPrefix.value.toLowerCase()} request country: ${countryCodes.value.join(', ')}`
})

function sourceLine(trace: SignalRequestTrace) {
  const labels = trace.sources.map((source) => source.label).filter(Boolean)
  if (labels.length) return labels.join(' + ')
  if (trace.sourceCount > 0) return `${formatIndex(trace.sourceCount)} SOURCES`
  return 'EDGE SOURCE'
}

function traceTargetLine(trace: SignalRequestTrace) {
  const modelNames = trace.hops.map((hop) => hop.modelName).filter(Boolean)
  return modelNames.length ? modelNames.join(' > ') : 'MODEL GATEWAY'
}

function formatLatency(latency?: number) {
  return typeof latency === 'number' ? `${Math.round(latency)}MS` : 'PENDING'
}

const inboundLine = computed(() => {
  const trace = focusedTrace.value
  if (trace) return `${sourceLine(trace)} // ${phaseLabel(trace.phase)}`

  const sourceCount = props.snapshot.sourceCount ?? 0
  return sourceCount > 0
    ? `${formatIndex(sourceCount)} SOURCES // STANDBY`
    : 'EDGE READY // STANDBY'
})

const targetLine = computed(() => {
  const trace = focusedTrace.value
  if (trace)
    return `${traceTargetLine(trace)} // ${formatLatency(trace.latencyMs)}`

  const { modelName, latencyMs, targetCount = 0 } = props.snapshot
  if (modelName) return `${modelName} // ${formatLatency(latencyMs)}`
  if (targetCount > 0) return `SELECTING // ${formatIndex(targetCount)} NODES`
  return 'GATEWAY WARM // READY'
})

const moreLine = computed(() => {
  if (traceDrawerOpen.value)
    return `TRACE INDEX // ${formatIndex(drawerTraces.value.length)} RECORDS`
  const traceCount = drawerTraces.value.length
  return traceCount
    ? `HOLD 3S // ${formatIndex(traceCount)} TRACES`
    : 'HOLD 3S // TRACE INDEX'
})

const screenReaderStatus = computed(() => {
  const trace = focusedTrace.value
  if (!trace) return t('signalConsole.a11yIdle')
  return `${sourceLine(trace)} through ${traceTargetLine(trace)}: ${phaseLabel(trace.phase)}, ${formatLatency(trace.latencyMs)}`
})

function clearTraceOpenTimer() {
  if (!traceOpenTimer) return
  window.clearTimeout(traceOpenTimer)
  traceOpenTimer = undefined
}

function syncTraceDrawer() {
  const shouldRemainOpen =
    pointerWithinInspector.value || focusWithinInspector.value
  if (!shouldRemainOpen) {
    clearTraceOpenTimer()
    traceDrawerOpen.value = false
    return
  }

  if (traceDrawerOpen.value || traceOpenTimer) return
  traceOpenTimer = window.setTimeout(() => {
    traceOpenTimer = undefined
    if (pointerWithinInspector.value || focusWithinInspector.value)
      traceDrawerOpen.value = true
  }, 3000)
}

function onInspectorPointerEnter() {
  pointerWithinInspector.value = true
  syncTraceDrawer()
}

function onInspectorPointerLeave() {
  pointerWithinInspector.value = false
  syncTraceDrawer()
}

function onInspectorFocusIn() {
  focusWithinInspector.value = true
  syncTraceDrawer()
}

function onInspectorFocusOut() {
  if (focusCheckTimer) window.clearTimeout(focusCheckTimer)
  focusCheckTimer = window.setTimeout(() => {
    focusCheckTimer = undefined
    focusWithinInspector.value = Boolean(
      traceInspectorEl.value?.contains(document.activeElement)
    )
    syncTraceDrawer()
  }, 0)
}

function closeTraceDrawer() {
  clearTraceOpenTimer()
  traceDrawerOpen.value = false
}

onBeforeUnmount(() => {
  clearTraceOpenTimer()
  if (focusCheckTimer) window.clearTimeout(focusCheckTimer)
})
</script>
