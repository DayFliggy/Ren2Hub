<script setup lang="ts">
import { computed } from 'vue'

import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import type { TokenMeter } from '@/utils/tokenMeter'

/**
 * A key's state glyph: the arc is the consumed share, the colour is urgency,
 * and a centre mark says why a key is blocked (a bar for switched off, a slash
 * for expired) so those two never depend on colour alone.
 */
const props = withDefaults(
  defineProps<{
    meter: TokenMeter
    unlimited: boolean
    size?: number
    strokeWidth?: number
  }>(),
  { size: 28, strokeWidth: 3 }
)

const mark = computed(() => {
  if (props.meter.state === 'disabled') return 'M2.5 6h7'
  // Backslash, as in a no-entry sign; a forward slash reads as a gauge needle.
  if (props.meter.state === 'expired') return 'M3.5 3.5l5 5'
  return null
})
const markSize = computed(() => Math.round(props.size * 0.42))
</script>

<template>
  <MiniRing
    :percent="meter.arcPercent"
    :color="meter.ringColor"
    :size="size"
    :stroke-width="strokeWidth"
    :indeterminate="unlimited"
    :data-ring-state="meter.state"
  >
    <svg
      v-if="mark"
      :width="markSize"
      :height="markSize"
      viewBox="0 0 12 12"
      class="text-[var(--text-secondary)]"
      aria-hidden="true"
    >
      <path
        :d="mark"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
      />
    </svg>
    <slot v-else />
  </MiniRing>
</template>
