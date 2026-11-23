<script setup lang="ts">
import { onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { systemUpdateMessages } from '@/i18n/systemUpdate'

/**
 * The second step of an operation, shown in the panel where its button was,
 * as Sub2API confirms a rollback in place instead of opening a dialog. Focus
 * starts on Cancel so a repeated Enter does not run the operation.
 */
defineProps<{
  title: string
  message: string
  action: string
  variant: 'primary' | 'danger'
  /** The confirmed operation is running: the step stays until it ends. */
  running: boolean
}>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()

const { t } = useI18n({ useScope: 'local', messages: systemUpdateMessages })

const titleId = useId()
const cancel = ref<InstanceType<typeof ConsoleButton> | null>(null)

onMounted(() => (cancel.value?.$el as HTMLElement | undefined)?.focus())
</script>

<template>
  <div
    class="version-confirm"
    role="group"
    :aria-labelledby="titleId"
    data-version-confirm
  >
    <p :id="titleId" class="version-confirm-title">{{ title }}</p>
    <p class="version-note">{{ message }}</p>
    <div class="version-confirm-actions">
      <ConsoleButton
        ref="cancel"
        size="sm"
        variant="secondary"
        :disabled="running"
        data-version-confirm-cancel
        @click="emit('cancel')"
        >{{ t('cancel') }}</ConsoleButton
      >
      <ConsoleButton
        size="sm"
        :variant="variant"
        :loading="running"
        data-version-confirm-accept
        @click="emit('confirm')"
        >{{ action }}</ConsoleButton
      >
    </div>
  </div>
</template>
