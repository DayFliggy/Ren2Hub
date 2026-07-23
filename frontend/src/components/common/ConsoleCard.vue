<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    padded?: boolean
  }>(),
  { title: '', padded: true }
)
</script>

<template>
  <section
    class="rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] shadow-[var(--card-shadow)]"
  >
    <header
      v-if="title || $slots.action"
      class="flex items-center justify-between gap-3 px-5 pt-4"
      :class="{ 'pb-1': !padded }"
    >
      <h2 class="text-sm font-semibold text-[var(--text-primary)]">
        {{ title }}
      </h2>
      <slot name="action" />
    </header>
    <div :class="padded ? 'p-5' : ''">
      <slot />
    </div>
  </section>
</template>
