<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'

import ConsoleCard from '@/components/common/ConsoleCard.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const { phase, statusReachable, uptimeLabel, versionLabel } =
  storeToRefs(useAppStore())

const apiState = computed(() => {
  if (phase.value === 'ready' && statusReachable.value) {
    return {
      tone: 'success' as const,
      color: 'var(--status-success)',
      label: t('dashboard.online'),
      pulse: true,
    }
  }
  if (phase.value === 'degraded' && statusReachable.value) {
    return {
      tone: 'warning' as const,
      color: 'var(--status-warning)',
      label: t('dashboard.degraded'),
      pulse: false,
    }
  }
  if (phase.value === 'error') {
    return {
      tone: 'danger' as const,
      color: 'var(--status-danger)',
      label: t('dashboard.offline'),
      pulse: false,
    }
  }
  if (phase.value === 'loading') {
    return {
      tone: 'info' as const,
      color: 'var(--status-info)',
      label: t('dashboard.loading'),
      pulse: false,
    }
  }
  return {
    tone: 'neutral' as const,
    color: 'var(--text-tertiary)',
    label: t('dashboard.unknown'),
    pulse: false,
  }
})
</script>

<template>
  <ConsoleCard
    :title="t('dashboard.serviceStatus')"
    :aria-description="t('nav.demoModeHint')"
    data-source="mock"
  >
    <ul class="space-y-3.5 text-sm" :data-status-reachable="statusReachable">
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">{{
          t('dashboard.uptime')
        }}</span>
        <StatusChip :tone="apiState.tone">{{ uptimeLabel }}</StatusChip>
      </li>
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">{{
          t('dashboard.version')
        }}</span>
        <span class="font-mono text-[var(--text-primary)]">{{
          versionLabel
        }}</span>
      </li>
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">API</span>
        <span
          class="flex items-center gap-1.5 font-semibold"
          :style="{
            color: apiState.color,
          }"
        >
          <span class="relative flex h-2 w-2">
            <span
              v-if="apiState.pulse"
              class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60"
              :style="{ background: apiState.color }"
            />
            <span
              class="relative inline-flex h-2 w-2 rounded-full"
              :style="{ background: apiState.color }"
            />
          </span>
          {{ apiState.label }}
        </span>
      </li>
    </ul>
  </ConsoleCard>
</template>
