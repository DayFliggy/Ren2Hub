<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    vendor: string
    size?: number
  }>(),
  { size: 40 }
)

/** Deterministic token pairing so a vendor always gets the same swatch.
 *  Only semantic tokens are used — swaps cleanly across both themes. */
const palettes = [
  { bg: 'var(--accent-soft)', fg: 'var(--accent-text)' },
  { bg: 'var(--status-info-soft)', fg: 'var(--status-info-text)' },
  { bg: 'var(--status-success-soft)', fg: 'var(--status-success-text)' },
  { bg: 'var(--status-warning-soft)', fg: 'var(--status-warning-text)' },
  { bg: 'var(--status-danger-soft)', fg: 'var(--status-danger-text)' },
]

const swatch = computed(() => {
  let hash = 0
  for (let i = 0; i < props.vendor.length; i++) {
    hash = (hash * 31 + props.vendor.charCodeAt(i)) | 0
  }
  return palettes[Math.abs(hash) % palettes.length]
})

/** First alphanumeric char (handles CJK vendor names gracefully). */
const initial = computed(() => props.vendor.trim().charAt(0).toUpperCase())
</script>

<template>
  <span
    class="inline-flex shrink-0 items-center justify-center rounded-xl font-bold"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
      background: swatch.bg,
      color: swatch.fg,
      fontSize: `${Math.round(size * 0.42)}px`,
    }"
    aria-hidden="true"
  >
    {{ initial }}
  </span>
</template>
