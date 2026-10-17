<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
defineProps<{ title: string; hint: string }>()
const { t } = useI18n({ messages: restoredConsoleMessages })
</script>
<template>
  <ConsoleCard :title="title" stretch data-unavailable-panel>
    <div class="flex min-h-28 flex-col justify-center">
      <p class="text-xl font-semibold text-[var(--text-secondary)]">
        {{ t('restored.unavailable') }}
      </p>
      <p class="mt-3 text-sm leading-6 text-[var(--text-tertiary)]">
        {{ hint }}
      </p>
    </div>
  </ConsoleCard>
</template>
