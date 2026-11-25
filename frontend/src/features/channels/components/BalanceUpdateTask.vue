<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { useChannelBalanceTask } from '@/composables/useChannelBalanceTask'

const emit = defineEmits<{ complete: [] }>()
const { t } = useI18n()
const { task, busy, polling, active, error, start, poll } =
  useChannelBalanceTask({
    onComplete: () => emit('complete'),
  })
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 text-xs">
    <ConsoleButton
      size="sm"
      variant="secondary"
      :disabled="busy || active"
      @click="start"
      >{{ t('pluginExtensions.balanceAll') }}</ConsoleButton
    >
    <span v-if="active" role="status">{{
      task?.state
        ? t('pluginExtensions.balanceProgress', {
            completed: task.state.processed ?? 0,
            total: task.state.total ?? 0,
          })
        : t('pluginExtensions.balancePending')
    }}</span>
    <span v-if="task?.status === 'succeeded'" role="status">{{
      t('pluginExtensions.balanceDone')
    }}</span>
    <span v-if="error" role="alert" class="text-[var(--status-danger-text)]">{{
      error
    }}</span>
    <ConsoleButton
      v-if="error && active"
      :disabled="polling"
      size="sm"
      variant="secondary"
      @click="poll"
      >{{ t('common.retry') }}</ConsoleButton
    >
  </div>
</template>
