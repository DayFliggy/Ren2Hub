<script setup lang="ts">
// Upstream renders shadcn <Alert>; Ren2Hub has no shared alert, so this maps
// its default / destructive / warning variants onto status tokens.
withDefaults(
  defineProps<{
    tone?: 'neutral' | 'danger' | 'warning' | 'success'
    title?: string
    role?: 'alert' | 'status'
  }>(),
  { tone: 'neutral', title: '', role: undefined }
)
const tones = {
  neutral:
    'border-[var(--border-subtle)] bg-[var(--surface-muted)] text-[var(--text-primary)]',
  danger:
    'border-[var(--status-danger)]/25 bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]',
  warning:
    'border-[var(--status-warning)]/25 bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]',
  success:
    'border-[var(--status-success)]/25 bg-[var(--status-success-soft)] text-[var(--status-success-text)]',
}
</script>

<template>
  <div
    :role="role"
    class="flex min-w-0 gap-2.5 rounded-xl border px-3.5 py-2.5 text-sm"
    :class="tones[tone]"
  >
    <span v-if="$slots.icon" class="mt-0.5 shrink-0" aria-hidden="true">
      <slot name="icon" />
    </span>
    <div class="min-w-0 flex-1 space-y-1">
      <p v-if="title" class="font-medium break-words">{{ title }}</p>
      <div
        v-if="$slots.default"
        class="text-xs leading-relaxed break-words opacity-90"
      >
        <slot />
      </div>
    </div>
  </div>
</template>
