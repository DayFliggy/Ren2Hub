<script setup lang="ts">
import StatusChip from '@/components/common/StatusChip.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

defineProps<{
  title: string
  tagline: string
  statusLabel: string
  icon: string
  claimLabel: string
  footerHint: string
}>()

const emit = defineEmits<{ claim: [] }>()
</script>

<template>
  <article
    class="pencil-surface flex flex-col rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-5"
    data-handdrawn="surface"
  >
    <header class="flex items-start justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <span
          class="flex size-10 shrink-0 items-center justify-center rounded-full"
          style="background: var(--accent-soft)"
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--accent-text)"
            stroke-width="1.8"
            aria-hidden="true"
          >
            <path :d="icon" />
          </svg>
        </span>
        <div class="min-w-0">
          <h3 class="text-base font-semibold text-[var(--text-primary)]">
            {{ title }}
          </h3>
          <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
            {{ tagline }}
          </p>
        </div>
      </div>
      <div class="flex shrink-0 flex-col items-end gap-1.5">
        <StatusChip tone="neutral">{{ statusLabel }}</StatusChip>
      </div>
    </header>

    <div class="mt-4 flex-1">
      <slot />
    </div>

    <footer
      class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-[var(--border-subtle)] pt-4"
    >
      <p class="text-xs text-[var(--text-tertiary)]">{{ footerHint }}</p>
      <ConsoleButton size="sm" @click="emit('claim')">
        {{ claimLabel }}
      </ConsoleButton>
    </footer>
  </article>
</template>
