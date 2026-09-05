<script setup lang="ts">
import AppNavbar from '@/components/layout/AppNavbar.vue'
import AppFooter from '@/components/layout/AppFooter.vue'
defineProps<{ title: string }>()
</script>

<template>
  <div
    class="public-page min-h-screen bg-[var(--page-background)] text-[var(--text-primary)]"
  >
    <AppNavbar />
    <main
      class="mx-auto min-h-[calc(100vh-6rem)] w-full max-w-7xl px-4 pb-12 pt-28 sm:px-6 lg:px-8"
    >
      <header
        class="mb-8 flex min-w-0 flex-wrap items-center justify-between gap-4 border-b border-[var(--border-subtle)] pb-5"
      >
        <h1 class="display-title min-w-0 break-words text-3xl font-semibold">
          {{ title }}
        </h1>
        <slot name="actions" />
      </header>
      <slot />
    </main>
    <AppFooter />
  </div>
</template>

<style scoped>
.public-page :deep(a:focus-visible) {
  outline: 2px solid var(--focus-ring);
  outline-offset: 3px;
}
.public-page :deep(table) {
  border-collapse: collapse;
  width: 100%;
  text-align: left;
}
.public-page :deep(th),
.public-page :deep(td) {
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-subtle);
}
.public-page :deep(th) {
  background: var(--surface-table-header);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}
.public-page :deep(td) {
  font-size: 14px;
}
</style>
