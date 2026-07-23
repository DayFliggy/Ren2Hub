<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    tone?: 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'accent'
  }>(),
  { tone: 'neutral' }
)

const style = computed(() => {
  switch (props.tone) {
    case 'success':
      return 'background:var(--status-success-soft);color:var(--status-success-text)'
    case 'warning':
      return 'background:var(--status-warning-soft);color:var(--status-warning-text)'
    case 'danger':
      return 'background:var(--status-danger-soft);color:var(--status-danger-text)'
    case 'info':
      return 'background:var(--status-info-soft);color:var(--status-info-text)'
    case 'accent':
      return 'background:var(--accent-soft);color:var(--accent-text)'
    default:
      return 'background:var(--surface-muted);color:var(--text-tertiary)'
  }
})
</script>

<template>
  <span
    class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium"
    :style="style"
  >
    <slot />
  </span>
</template>
