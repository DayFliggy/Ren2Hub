<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { onClickOutside, onKeyStroke } from '@vueuse/core'

export interface CpuLoadCell {
  /** Averaging window: 1, 5 or 15 minutes. */
  minutes: number
  /** Formatted load figure; null when this window was not measured. */
  value: string | null
  /** Share of the cores in use, 0-1; null when the core count is unknown. */
  fill: number | null
  /** Load-tier colour chosen by the caller. */
  color: string
  /** Load beyond the core count: work is queueing for CPU. */
  overloaded: boolean
}

/**
 * Three load cells (1/5/15 min) as a single 44×44 tap target. Click or hover
 * reveals a popover showing all load values with core count.
 */
const props = defineProps<{
  cells: CpuLoadCell[]
  cores: number | null
}>()

const { t } = useI18n()

const open = ref(false)
const root = ref<HTMLElement>()

onClickOutside(root, () => {
  open.value = false
})

onKeyStroke('Escape', () => {
  open.value = false
})

// Hover is tracked on the wrapper, which also contains the readout, so the
// pointer can move onto the readout without closing it (WCAG 1.4.13). Touch
// and pen taps are left to the click toggle.
function handlePointerEnter(event: PointerEvent) {
  if (event.pointerType === 'mouse') open.value = true
}

function handlePointerLeave(event: PointerEvent) {
  if (event.pointerType === 'mouse') open.value = false
}

// A mouse click while hovering keeps the readout open instead of toggling it
// shut under the pointer; keyboard and touch activation toggle.
function handleClick(event: MouseEvent) {
  // Browsers dispatch click as a PointerEvent; keyboard clicks report no type.
  const isMouse = (event as PointerEvent).pointerType === 'mouse'
  open.value = isMouse || !open.value
}

/** A measured non-zero load keeps a sliver so it does not read as empty. */
function fillHeight(fill: number): string {
  if (fill <= 0) return '0%'
  return `${Math.max(8, Math.min(1, fill) * 100)}%`
}

function buttonLabel(): string {
  const windows = props.cells
    .map((c) =>
      c.value === null
        ? t('dashboard.systemStatus.loadWindowUnknown', { minutes: c.minutes })
        : t('dashboard.systemStatus.loadWindow', {
            minutes: c.minutes,
            value: c.value,
          })
    )
    .join(t('dashboard.systemStatus.loadSeparator'))
  if (props.cores === null) {
    return t('dashboard.systemStatus.loadSummary', { windows })
  }
  const coreLabel = t(
    'dashboard.systemStatus.loadCores',
    { n: props.cores.toString() },
    props.cores
  )
  return t('dashboard.systemStatus.loadSummaryCores', {
    windows,
    cores: coreLabel,
  })
}
</script>

<template>
  <span
    ref="root"
    class="cpu-load"
    data-cpu-load
    @pointerenter="handlePointerEnter"
    @pointerleave="handlePointerLeave"
  >
    <button
      class="cpu-load__trigger focus-ring"
      type="button"
      :aria-label="buttonLabel()"
      :aria-expanded="open"
      @click="handleClick"
    >
      <!-- Every window keeps its full tier colour; position and the readout
           tell the windows apart, so fading older ones would blur the tiers. -->
      <span
        v-for="cell in cells"
        :key="cell.minutes"
        class="cpu-load__cell"
        :data-minutes="cell.minutes"
        :data-unknown="cell.value === null ? '' : undefined"
        :style="{
          '--tier': cell.color,
          '--fill-height': cell.fill !== null ? fillHeight(cell.fill) : '0%',
        }"
      >
        <span class="cpu-load__fill" data-cpu-load-fill />
      </span>
    </button>

    <div
      class="cpu-load__readout"
      :class="{ 'cpu-load__readout--open': open }"
      data-load-readout
      aria-hidden="true"
    >
      <p v-if="cores !== null" class="cpu-load__header">
        {{ t('dashboard.systemStatus.loadAverage') }} ·
        {{
          t('dashboard.systemStatus.loadCores', { n: cores.toString() }, cores)
        }}
      </p>
      <dl class="cpu-load__values">
        <div
          v-for="cell in cells"
          :key="cell.minutes"
          class="cpu-load__value-col"
          :data-load-window="cell.minutes"
        >
          <dt class="cpu-load__caption">
            {{
              t('dashboard.systemStatus.loadCaption', { minutes: cell.minutes })
            }}
          </dt>
          <dd
            class="cpu-load__figure"
            :class="{ 'cpu-load__figure--over': cell.overloaded }"
          >
            <template v-if="cell.value !== null">{{ cell.value }}</template>
            <template v-else>
              --<span class="sr-only">{{
                t('dashboard.systemStatus.loadUnavailable')
              }}</span>
            </template>
          </dd>
        </div>
      </dl>
    </div>
  </span>
</template>

<style scoped>
.cpu-load {
  position: relative;
  display: inline-flex;
}

.cpu-load__trigger {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
}

/* Extends the tap target to 44×44px while keeping cells visually 8×16. */
.cpu-load__trigger::before {
  content: '';
  position: absolute;
  inset: -8px -7px -20px -7px;
  border-radius: var(--shape-control);
}

.cpu-load__cell {
  position: relative;
  display: inline-flex;
  width: 8px;
  height: 16px;
  align-items: center;
  justify-content: center;
  border-radius: 3px;
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
  transition: background-color 200ms ease;
}

.cpu-load__trigger[aria-expanded='true'] .cpu-load__cell {
  background: color-mix(in srgb, var(--tier) 12%, transparent);
}

.cpu-load__cell[data-unknown] {
  background: transparent;
  border: 1px dashed color-mix(in srgb, var(--text-tertiary) 60%, transparent);
}

.cpu-load__fill {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 8px;
  height: 16px;
  border-radius: 3px;
  background: var(--tier);
  clip-path: inset(calc(100% - var(--fill-height, 0%)) 0 0 0);
  transition: clip-path 500ms var(--ease-spring-smooth);
}

.cpu-load__readout {
  position: absolute;
  top: calc(100% + 4px);
  left: 50%;
  transform: translateX(-50%) scale(0.94) translateY(-4px);
  min-width: 160px;
  padding: 10px 12px 11px;
  background: var(--surface-overlay);
  border: 1px solid var(--overlay-border);
  border-radius: var(--shape-overlay);
  box-shadow: var(--elevation-2);
  opacity: 0;
  transform-origin: top center;
  transition:
    opacity 180ms var(--ease-spring-snappy),
    transform 180ms var(--ease-spring-snappy);
  visibility: hidden;
  z-index: 10;
}

.cpu-load__readout--open {
  opacity: 1;
  transform: translateX(-50%) scale(1) translateY(0);
  visibility: visible;
}

.cpu-load__header {
  margin: 0 0 8px;
  font-size: 11px;
  font-weight: 500;
  color: var(--text-secondary);
  white-space: nowrap;
}

.cpu-load__values {
  display: flex;
  gap: 14px;
  margin: 0;
}

.cpu-load__value-col {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cpu-load__caption {
  font-size: 9px;
  font-weight: 500;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  color: var(--text-tertiary);
}

.cpu-load__figure {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--text-primary);
}

.cpu-load__figure--over {
  color: var(--load-danger-text);
}
</style>
