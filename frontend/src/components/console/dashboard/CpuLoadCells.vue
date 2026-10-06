<script setup lang="ts">
import { useI18n } from 'vue-i18n'

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
 * Three load cells (1 / 5 / 15 min) without text. Hovering or focusing a
 * cell stretches it to reveal its figure; leaving springs it back.
 */
const props = defineProps<{
  cells: CpuLoadCell[]
  cores: number | null
}>()

const { t } = useI18n()

/** Newer windows read stronger, matching the left-to-right order. */
const STRENGTH = [1, 0.75, 0.55]

function cellLabel(cell: CpuLoadCell): string {
  const minutes = cell.minutes
  if (cell.value === null) {
    return t('dashboard.systemStatus.loadCellUnknown', { minutes })
  }
  if (props.cores === null) {
    return t('dashboard.systemStatus.loadCellNoCores', {
      minutes,
      value: cell.value,
    })
  }
  return t(
    'dashboard.systemStatus.loadCell',
    { minutes, value: cell.value, cores: props.cores },
    props.cores
  )
}

/** A measured non-zero load keeps a sliver so it does not read as empty. */
function fillHeight(fill: number): string {
  if (fill <= 0) return '0%'
  return `${Math.max(8, Math.min(1, fill) * 100)}%`
}
</script>

<template>
  <span
    class="cpu-load"
    role="group"
    :aria-label="t('dashboard.systemStatus.loadAverage')"
    data-cpu-load
  >
    <span
      v-for="(cell, index) in cells"
      :key="cell.minutes"
      class="cpu-load__cell"
      role="img"
      tabindex="0"
      :aria-label="cellLabel(cell)"
      :data-minutes="cell.minutes"
      :data-unknown="cell.value === null ? '' : undefined"
      :style="{ '--tier': cell.color, '--strength': STRENGTH[index] ?? 1 }"
    >
      <span
        v-if="cell.fill !== null"
        class="cpu-load__fill"
        :style="{ height: fillHeight(cell.fill) }"
        data-cpu-load-fill
      />
      <span
        class="cpu-load__value"
        :class="{ 'cpu-load__value--over': cell.overloaded }"
        aria-hidden="true"
      >
        {{ cell.value ?? '--' }}
      </span>
    </span>
  </span>
</template>
<style scoped>
.cpu-load {
  display: inline-flex;
  height: 16px;
  flex-shrink: 0;
  align-items: center;
  gap: 3px;
}

/* Leaving uses the bouncy spring, whose overshoot is the rebound; the global
   reduced-motion rule turns every transition here off. */
.cpu-load__cell {
  position: relative;
  display: inline-flex;
  width: 8px;
  height: 16px;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border-radius: 3px;
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
  outline: none;
  -webkit-tap-highlight-color: transparent;
  transition:
    width 460ms var(--ease-spring-bouncy),
    background-color 200ms ease;
}

.cpu-load__cell:hover,
.cpu-load__cell:focus-visible {
  width: 32px;
  background: color-mix(in srgb, var(--tier) 16%, transparent);
  transition:
    width 260ms var(--ease-spring-snappy),
    background-color 200ms ease;
}

.cpu-load__cell:focus-visible {
  outline: 1.5px solid var(--tier);
  outline-offset: 1px;
}

.cpu-load__cell[data-unknown] {
  background: transparent;
  border: 1px dashed color-mix(in srgb, var(--text-tertiary) 60%, transparent);
}

.cpu-load__fill {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  border-radius: inherit;
  background: var(--tier);
  opacity: var(--strength);
  transition:
    height 500ms var(--ease-spring-smooth),
    opacity 200ms ease;
}

.cpu-load__cell:hover .cpu-load__fill,
.cpu-load__cell:focus-visible .cpu-load__fill {
  opacity: 0.28;
}

.cpu-load__value {
  position: relative;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  line-height: 1;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  color: var(--text-primary);
  opacity: 0;
  transition: opacity 120ms ease;
}

.cpu-load__value--over {
  color: var(--status-danger);
}

.cpu-load__cell:hover .cpu-load__value,
.cpu-load__cell:focus-visible .cpu-load__value {
  opacity: 1;
  transition-delay: 90ms;
}
</style>
