<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ value: number | null; label: string }>()
const { locale } = useI18n()
const percent = computed(() =>
  props.value === null ? null : Math.min(100, Math.max(0, props.value))
)
const tone = computed(() =>
  percent.value === null
    ? 'neutral'
    : percent.value >= 90
      ? 'danger'
      : percent.value >= 70
        ? 'warning'
        : 'success'
)
const labelValue = computed(() =>
  props.value === null
    ? '-'
    : `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(props.value)}%`
)
</script>

<template>
  <span class="resource-metric" :aria-label="`${label}: ${labelValue}`">
    <svg
      class="resource-ring"
      :class="`resource-ring--${tone}`"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <circle class="resource-track" cx="12" cy="12" r="9" />
      <circle
        cx="12"
        cy="12"
        r="9"
        pathLength="100"
        stroke-dasharray="100"
        :stroke-dashoffset="100 - (percent ?? 0)"
      />
    </svg>
    <span>{{ labelValue }}</span>
  </span>
</template>

<style scoped>
.resource-metric {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-width: 5rem;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.resource-ring {
  flex: none;
  transform: rotate(-90deg);
  fill: none;
  stroke: currentColor;
  stroke-width: 2.5;
  color: var(--text-tertiary);
}
.resource-track {
  stroke: var(--border-subtle);
}
.resource-ring--success {
  color: var(--status-success);
}
.resource-ring--warning {
  color: var(--status-warning);
}
.resource-ring--danger {
  color: var(--status-danger);
}
</style>
