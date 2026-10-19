<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OpsStatus } from '@/api/opsMonitor'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { STATUS_COLORS } from '@/utils/opsFormat'

/**
 * One labelled reading inside a `<dl>`. Status shows as a small dot; only a
 * critical value also tints its text, so healthy and unknown stay quiet.
 */
withDefaults(
  defineProps<{
    label: string
    value: string
    unit?: string
    hint?: string
    status?: OpsStatus | null
    size?: 'lg' | 'md' | 'sm'
  }>(),
  { unit: '', hint: '', status: null, size: 'md' }
)
const { t } = useI18n({ useScope: 'local', messages: opsMonitorMessages })
</script>

<template>
  <div class="ops-metric" :class="`ops-metric--${size}`">
    <dt class="ops-metric-label">
      <span
        v-if="status === 'warning' || status === 'critical'"
        class="ops-dot"
        :style="{ background: STATUS_COLORS[status] }"
        aria-hidden="true"
      />{{ label
      }}<span
        v-if="status === 'warning' || status === 'critical'"
        class="sr-only"
        >({{ t(`status.${status}`) }})</span
      >
    </dt>
    <dd
      class="ops-metric-value"
      :class="{ 'ops-metric-value--critical': status === 'critical' }"
    >
      {{ value
      }}<span v-if="unit && value !== '--'" class="ops-unit">{{ unit }}</span>
    </dd>
    <dd v-if="hint" class="ops-metric-hint">{{ hint }}</dd>
  </div>
</template>
