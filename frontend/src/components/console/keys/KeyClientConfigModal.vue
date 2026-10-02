<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getTokenClientConfig,
  type ModelClientConfig,
} from '@/api/modelManifest'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { clientConfigMessages } from '@/i18n/clientConfig'

const props = defineProps<{ tokenId: number }>()
const emit = defineEmits<{ close: [] }>()
const { t, te } = useI18n({ useScope: 'local', messages: clientConfigMessages })
const format = ref<ModelClientConfig['format']>('codex')
const selectedModel = ref('')
const configuration = ref<ModelClientConfig | null>(null)
const loading = ref(false)
const error = ref('')
const copied = ref(false)
const request = useLatestRequest()

async function load() {
  loading.value = true
  error.value = ''
  copied.value = false
  const result = await request.run((signal) =>
    getTokenClientConfig(
      props.tokenId,
      format.value,
      selectedModel.value || undefined,
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    configuration.value = null
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  configuration.value = result.value
  if (!selectedModel.value) selectedModel.value = result.value.models[0] ?? ''
}
watch(
  () => [props.tokenId, format.value],
  () => {
    selectedModel.value = ''
    configuration.value = null
    void load()
  },
  { immediate: true }
)

async function copy() {
  if (!configuration.value || loading.value) return
  try {
    await navigator.clipboard.writeText(configuration.value.content)
    copied.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
function download() {
  if (!configuration.value || loading.value) return
  const url = URL.createObjectURL(
    new Blob([configuration.value.content], {
      type: configuration.value.media_type,
    })
  )
  const link = document.createElement('a')
  link.href = url
  link.download = configuration.value.filename
  link.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="t('title')"
    :subtitle="t('hint')"
    size="lg"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-sm text-[var(--text-secondary)]"
          >{{ t('client') }}
          <select
            v-model="format"
            class="console-input mt-1 w-full"
            :aria-label="t('client')"
          >
            <option value="codex">Codex</option>
            <option value="pi">Pi</option>
          </select>
        </label>
        <label
          v-if="format === 'codex' && configuration"
          class="text-sm text-[var(--text-secondary)]"
          >{{ t('model') }}
          <select
            v-model="selectedModel"
            class="console-input mt-1 w-full"
            :aria-label="t('model')"
            :disabled="loading"
            @change="load"
          >
            <option
              v-for="model in configuration.models"
              :key="model"
              :value="model"
            >
              {{ model }}
            </option>
          </select>
        </label>
      </div>
      <p v-if="loading" role="status">{{ t('loading') }}</p>
      <div
        v-if="error"
        role="alert"
        class="space-y-2 text-sm text-[var(--status-danger-text)]"
      >
        <p>{{ error }}</p>
        <ConsoleButton variant="secondary" @click="load">{{
          t('retry')
        }}</ConsoleButton>
      </div>
      <template v-if="configuration">
        <textarea
          :aria-label="t('preview')"
          :value="configuration.content"
          readonly
          rows="12"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs text-[var(--text-primary)]"
        />
        <div
          v-if="configuration.diagnostics.length"
          class="text-xs text-[var(--text-secondary)]"
        >
          <p class="font-medium">{{ t('diagnostics') }}</p>
          <ul class="mt-2 list-inside list-disc space-y-1">
            <li v-for="(item, index) in configuration.diagnostics" :key="index">
              {{ item.model ? `${item.model}: ` : ''
              }}{{ te(item.code) ? t(item.code) : item.code }}
            </li>
          </ul>
        </div>
      </template>
    </div>
    <template #footer
      ><div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton variant="ghost" @click="emit('close')">{{
          t('close')
        }}</ConsoleButton
        ><ConsoleButton
          variant="secondary"
          :disabled="!configuration || loading"
          @click="copy"
          >{{ copied ? t('copied') : t('copy') }}</ConsoleButton
        ><ConsoleButton
          :disabled="!configuration || loading"
          @click="download"
          >{{ t('download') }}</ConsoleButton
        >
      </div></template
    >
  </ConsoleModal>
</template>
