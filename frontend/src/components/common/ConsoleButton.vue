<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'danger' | 'ghost'
    size?: 'sm' | 'md' | 'lg'
    block?: boolean
    loading?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
  }>(),
  {
    variant: 'primary',
    size: 'md',
    block: false,
    loading: false,
    disabled: false,
    type: 'button',
  }
)

const classes = computed(() => {
  const base =
    'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-xl font-semibold transition-all focus-ring disabled:cursor-not-allowed disabled:opacity-50'
  const sizes = {
    sm: 'h-8 px-3 text-xs',
    md: 'h-10 px-4 text-sm',
    lg: 'h-12 px-5 text-base',
  }
  const variants = {
    // The design's solid CTA maps to the accent role (caramel by day,
    // starlight gold by night) — never hardcoded black.
    primary:
      'bg-[var(--accent)] text-[var(--accent-contrast)] hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] shadow-[0_6px_18px_var(--shadow-color)]',
    secondary:
      'border border-[var(--border-default)] bg-[var(--surface-solid)] text-[var(--text-primary)] hover:bg-[var(--surface-muted)]',
    // On the rust-red fill, --text-inverse (light by day, dark by night) holds
    // contrast; --accent-contrast is a dark ink that fails against day rust.
    danger:
      'bg-[var(--status-danger)] text-[var(--text-inverse)] hover:opacity-90',
    ghost:
      'text-[var(--text-secondary)] hover:bg-[var(--surface-muted)] hover:text-[var(--text-primary)]',
  }
  return [
    base,
    sizes[props.size],
    variants[props.variant],
    props.block ? 'w-full' : '',
  ].join(' ')
})
</script>

<template>
  <button :type="type" :class="classes" :disabled="disabled || loading">
    <svg
      v-if="loading"
      class="h-4 w-4 animate-spin"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle
        cx="12"
        cy="12"
        r="9"
        stroke="currentColor"
        stroke-opacity=".25"
        stroke-width="2.5"
      />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        stroke-width="2.5"
        stroke-linecap="round"
      />
    </svg>
    <slot />
  </button>
</template>
