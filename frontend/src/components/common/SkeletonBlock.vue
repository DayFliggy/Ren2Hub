<script setup lang="ts">
withDefaults(
  defineProps<{
    bordered?: boolean
    rounded?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | 'full'
  }>(),
  { bordered: false, rounded: '2xl' }
)

const roundedClass = {
  sm: 'rounded-sm',
  md: 'rounded-md',
  lg: 'rounded-lg',
  xl: 'rounded-xl',
  '2xl': 'rounded-2xl',
  full: 'rounded-full',
} as const
</script>

<template>
  <div
    aria-hidden="true"
    class="animate-pulse"
    :class="[
      bordered
        ? 'border border-[var(--border-subtle)] bg-[var(--surface-solid)]'
        : 'bg-[var(--surface-muted)]',
      roundedClass[rounded],
    ]"
  />
</template>
