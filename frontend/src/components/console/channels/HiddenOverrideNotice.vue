<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'

/** State line under an override field whose stored value is hidden. */
defineProps<{
  /** Display name of the field, already translated. */
  name: string
  cleared: boolean
  canWrite: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ clear: []; keep: [] }>()
const { t } = useI18n()
</script>

<template>
  <div
    class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-[var(--text-secondary)]"
  >
    <span>{{
      cleared
        ? t('channels.hiddenOverrideWillClear', { field: name })
        : t('channels.redactedHidden')
    }}</span>
    <ConsoleButton
      v-if="canWrite"
      size="sm"
      variant="secondary"
      :disabled="disabled"
      @click="cleared ? emit('keep') : emit('clear')"
    >
      {{
        cleared
          ? t('channels.hiddenOverrideKeep')
          : t('channels.clearHiddenOverride', { field: name })
      }}
    </ConsoleButton>
  </div>
</template>
