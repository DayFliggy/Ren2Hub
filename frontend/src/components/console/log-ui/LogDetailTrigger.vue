<script setup lang="ts">
import { Eye } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    align?: 'left' | 'right'
    content: string
    label: string
  }>(),
  {
    align: 'left',
  }
)

const emit = defineEmits<{
  activate: []
}>()
</script>

<template>
  <button
    type="button"
    data-log-detail-trigger
    class="group -my-1 inline-flex min-h-11 w-full min-w-0 items-center gap-1.5 text-xs text-[var(--text-secondary)] transition-colors focus-ring hover:text-[var(--accent-text)] focus-visible:text-[var(--accent-text)] lg:min-h-7"
    :class="align === 'right' ? 'justify-end text-right' : 'text-left'"
    :title="content || label"
    :aria-label="label"
    @click="emit('activate')"
  >
    <span class="relative min-w-0 max-w-full pb-0.5 align-bottom">
      <span class="block truncate">{{ content || '—' }}</span>
      <span
        data-log-detail-underline
        class="absolute inset-x-0 bottom-0 h-0.5 origin-left scale-x-0 bg-[var(--accent)] transition-transform duration-150 group-hover:scale-x-100 group-focus-visible:scale-x-100"
        aria-hidden="true"
      />
    </span>
    <Eye
      :size="14"
      aria-hidden="true"
      class="shrink-0 text-[var(--text-tertiary)] transition-colors group-hover:text-[var(--accent-text)]"
    />
  </button>
</template>
