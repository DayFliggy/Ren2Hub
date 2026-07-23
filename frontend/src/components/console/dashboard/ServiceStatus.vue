<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import StatusChip from '@/components/common/StatusChip.vue'

const { t } = useI18n()

const version = ref('--')
const available = ref(false)

onMounted(async () => {
  try {
    const status = await api.get<{ version?: string }>('/api/status')
    version.value = status.version || '--'
    available.value = true
  } catch {
    available.value = false
  }
})
</script>

<template>
  <ConsoleCard :title="t('dashboard.serviceStatus')">
    <ul class="space-y-3.5 text-sm">
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">{{
          t('dashboard.dataSource')
        }}</span>
        <StatusChip tone="info">{{ t('dashboard.localMock') }}</StatusChip>
      </li>
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">{{
          t('dashboard.version')
        }}</span>
        <span class="font-mono text-[var(--text-primary)]">{{ version }}</span>
      </li>
      <li class="flex items-center justify-between">
        <span class="text-[var(--text-tertiary)]">{{
          t('dashboard.simulatedApi')
        }}</span>
        <span
          class="flex items-center gap-1.5 font-semibold"
          :style="{
            color: available ? 'var(--status-success)' : 'var(--status-danger)',
          }"
        >
          <span class="relative flex h-2 w-2">
            <span
              v-if="available"
              class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60"
              style="background: var(--status-success)"
            />
            <span
              class="relative inline-flex h-2 w-2 rounded-full"
              :style="{
                background: available
                  ? 'var(--status-success)'
                  : 'var(--status-danger)',
              }"
            />
          </span>
          {{
            available ? t('dashboard.available') : t('dashboard.unavailable')
          }}
        </span>
      </li>
    </ul>
  </ConsoleCard>
</template>
