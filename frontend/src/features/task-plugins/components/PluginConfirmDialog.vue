<script setup lang="ts">
// Upstream @/components/confirm-dialog (title, desc, destructive, extra
// actions before confirm). Ren2Hub's ConfirmDialog has no destructive tone.
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'

withDefaults(
  defineProps<{
    open: boolean
    title: string
    message?: string
    confirmText: string
    destructive?: boolean
    loading?: boolean
    hideConfirm?: boolean
  }>(),
  { message: '', destructive: false, loading: false, hideConfirm: false }
)
const emit = defineEmits<{ confirm: []; cancel: [] }>()
const { t } = useI18n()
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="title"
    size="md"
    :close-disabled="loading"
    @close="emit('cancel')"
  >
    <div
      class="sibling-space-y-3 break-words text-sm text-[var(--text-secondary)]"
    >
      <p v-if="message">{{ message }}</p>
      <slot />
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="loading"
          @click="emit('cancel')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <slot name="actions" />
        <ConsoleButton
          v-if="!hideConfirm"
          :variant="destructive ? 'danger' : 'primary'"
          :loading="loading"
          @click="emit('confirm')"
        >
          {{ confirmText }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
