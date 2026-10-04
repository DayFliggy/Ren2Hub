<script setup lang="ts">
// Upstream components/javascript-viewer.tsx uses CodeMirror. Ren2Hub has no
// editor dependency, so this is a read-only, line-numbered, wrapping <pre>
// without syntax highlighting.
import { computed } from 'vue'

const props = defineProps<{ value: string; label: string }>()
const lines = computed(() => props.value.split('\n'))
</script>

<template>
  <div
    class="overflow-auto rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] py-3 font-mono text-xs leading-[1.65]"
    role="region"
    :aria-label="label"
    tabindex="0"
  >
    <div
      v-for="(line, index) in lines"
      :key="index"
      class="grid grid-cols-[3rem_minmax(0,1fr)]"
    >
      <span
        class="select-none border-r border-[var(--border-subtle)] pr-2 text-right text-[var(--text-tertiary)]"
        aria-hidden="true"
        >{{ index + 1 }}</span
      >
      <span class="whitespace-pre-wrap break-all px-3">{{ line || ' ' }}</span>
    </div>
  </div>
</template>
