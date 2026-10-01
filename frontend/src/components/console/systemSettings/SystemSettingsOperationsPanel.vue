<script setup lang="ts">
import { ArrowRight } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <section class="operations-panel" data-operations-monitor-link>
    <p>{{ t('systemSettings.operations.monitorNote') }}</p>
    <RouterLink :to="{ name: 'ops-monitor' }" class="operations-link focus-ring"
      >{{ t('systemSettings.operations.monitorLink')
      }}<ArrowRight :size="15" aria-hidden="true"
    /></RouterLink>
  </section>
</template>

<style scoped>
.operations-panel {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-top: 1.5rem;
  border-top: 1px dashed var(--border-default);
  padding-top: 1rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
}
.operations-panel p {
  flex: 1 1 18rem;
  min-width: 0;
}
.operations-link {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-height: 2.75rem;
  font-weight: 600;
  color: var(--accent-text);
}
.operations-link:hover {
  text-decoration: underline;
}
</style>
