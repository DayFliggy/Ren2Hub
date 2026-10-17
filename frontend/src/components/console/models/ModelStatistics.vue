<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelMarketMessages } from '@/i18n/modelMarket'

withDefaults(defineProps<{ compact?: boolean }>(), { compact: false })
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
</script>

<template>
  <div
    class="model-statistics-empty"
    :class="{ compact }"
    data-model-statistics-unavailable
    data-market-telemetry
  >
    <span class="statistics-mark" aria-hidden="true" />
    <span>{{ t('noStatistics') }}</span>
    <span v-if="!compact" class="statistics-explanation">{{
      t('noStatisticsDetail')
    }}</span>
  </div>
</template>

<style scoped>
.model-statistics-empty {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.4rem 0.5rem;
  padding: 0.65rem 0;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  border-top: 1px dashed var(--border-subtle);
}
.statistics-mark {
  width: 0.4rem;
  height: 0.4rem;
  border: 1px solid var(--text-tertiary);
  border-radius: 50%;
  flex: none;
}
.statistics-explanation {
  flex-basis: 100%;
  line-height: 1.7;
}
.compact {
  padding: 0.5rem 0 0;
  font-size: 0.6875rem;
}
</style>
