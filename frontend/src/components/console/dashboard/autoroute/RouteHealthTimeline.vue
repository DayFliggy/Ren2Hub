<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <div>
    <div
      class="grid grid-cols-6 gap-1.5"
      role="img"
      :aria-label="t('dashboard.autoRoute.historyUnavailable')"
      data-route-health-timeline
    >
      <span
        v-for="i in 6"
        :key="i"
        class="h-6 min-w-0 rounded-md border border-[var(--border-subtle)] bg-[var(--surface-muted)] sm:h-7"
        aria-hidden="true"
        data-route-health-cell
      />
    </div>
    <p class="mt-1.5 text-[0.625rem] text-[var(--text-tertiary)]">
      {{ t('dashboard.autoRoute.historyUnavailable') }}
    </p>
  </div>
</template>
