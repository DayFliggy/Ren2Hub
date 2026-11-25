<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import type { AdminChannel } from '@/types/console'

const props = defineProps<{ channel: AdminChannel | null; canWrite: boolean }>()
const emit = defineEmits<{ close: []; applied: [] }>()
const { t } = useI18n()
const preview = ref<{ add_models: string[]; remove_models: string[] } | null>(
  null
)
const additions = ref<string[]>([])
const removals = ref<string[]>([])
const busy = ref(false)
const error = ref('')
let controller: AbortController | null = null
let sequence = 0

async function detect() {
  if (!props.channel) return
  const current = ++sequence
  controller?.abort()
  controller = new AbortController()
  busy.value = true
  error.value = ''
  preview.value = null
  try {
    const result = await api.post<{
      add_models: string[]
      remove_models: string[]
    }>(
      '/api/channel/upstream_updates/detect',
      { id: props.channel.id },
      { signal: controller.signal }
    )
    if (current !== sequence) return
    preview.value = result
    additions.value = [...result.add_models]
    removals.value = []
  } catch (cause) {
    if (current === sequence && !controller.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === sequence) busy.value = false
  }
}
async function apply() {
  if (!props.channel || !preview.value || !props.canWrite || busy.value) return
  const current = ++sequence
  controller?.abort()
  const request = new AbortController()
  controller = request
  busy.value = true
  error.value = ''
  try {
    await api.post(
      '/api/channel/upstream_updates/apply',
      {
        id: props.channel.id,
        add_models: additions.value,
        remove_models: removals.value,
        ignore_models: [],
      },
      { signal: request.signal }
    )
    if (current !== sequence) return
    emit('applied')
    emit('close')
  } catch (cause) {
    if (current === sequence && !request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === sequence) busy.value = false
  }
}
watch(
  () => props.channel,
  (channel) => {
    sequence++
    controller?.abort()
    preview.value = null
    error.value = ''
    if (channel) void detect()
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  sequence++
  controller?.abort()
})
</script>

<template>
  <ConsoleModal
    :open="channel !== null"
    :title="t('channels.syncModelsTitle')"
    size="lg"
    footer-border
    :close-disabled="busy && preview !== null"
    @close="emit('close')"
  >
    <p class="mb-4 text-sm text-[var(--text-secondary)]">
      {{ t('channels.syncModelsHint') }}
    </p>
    <p
      v-if="error"
      role="alert"
      class="mb-3 break-words text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <p v-if="busy && !preview" role="status">{{ t('common.loading') }}</p>
    <div v-if="preview" class="grid gap-5 sm:grid-cols-2">
      <section>
        <h3 class="mb-2.5 text-sm font-semibold text-[var(--text-primary)]">
          {{ t('channels.syncModelsAdd') }}
        </h3>
        <p
          v-if="!preview.add_models.length"
          class="text-sm text-[var(--text-tertiary)]"
        >
          {{ t('channels.syncModelsNone') }}
        </p>
        <label
          v-for="name in preview.add_models"
          :key="name"
          class="mb-2 flex cursor-pointer items-start gap-2.5 break-all text-sm text-[var(--text-primary)]"
          ><input
            v-model="additions"
            type="checkbox"
            :value="name"
            :disabled="busy || !canWrite"
            class="checkbox-round mt-0.5 shrink-0"
          /><span>{{ name }}</span></label
        >
      </section>
      <section>
        <h3 class="mb-2.5 text-sm font-semibold text-[var(--text-primary)]">
          {{ t('channels.syncModelsRemove') }}
        </h3>
        <p
          v-if="!preview.remove_models.length"
          class="text-sm text-[var(--text-tertiary)]"
        >
          {{ t('channels.syncModelsNone') }}
        </p>
        <label
          v-for="name in preview.remove_models"
          :key="name"
          class="mb-2 flex cursor-pointer items-start gap-2.5 break-all text-sm text-[var(--text-primary)]"
          ><input
            v-model="removals"
            type="checkbox"
            :value="name"
            :disabled="busy || !canWrite"
            class="checkbox-round mt-0.5 shrink-0"
          /><span>{{ name }}</span></label
        >
      </section>
    </div>
    <template #footer>
      <div class="flex flex-wrap items-center justify-end gap-3">
        <ConsoleButton
          variant="ghost"
          :disabled="busy && preview !== null"
          @click="emit('close')"
          >{{ t('common.cancel') }}</ConsoleButton
        >
        <ConsoleButton variant="secondary" :disabled="busy" @click="detect">{{
          t('channels.syncModelsRefresh')
        }}</ConsoleButton>
        <ConsoleButton
          :loading="busy && preview !== null"
          :disabled="
            !canWrite ||
            !preview ||
            busy ||
            (!additions.length && !removals.length)
          "
          @click="apply"
          >{{ t('channels.syncModelsApply') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
</template>
