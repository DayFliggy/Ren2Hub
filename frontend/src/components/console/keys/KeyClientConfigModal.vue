<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  getTokenClientConfig,
  loadClientConfigCatalog,
} from '@/api/clientConfig'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { clientConfigMessages } from '@/i18n/clientConfig'
import {
  ClientConfigError,
  type ClientConfigCatalog,
  type ClientConfigFormat,
  type ClientConfiguration,
  type PiModelSettings,
} from '@/types/clientConfig'
import { validPiSettings } from '@/utils/clientConfig'

const props = defineProps<{ tokenId: number }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n({ useScope: 'local', messages: clientConfigMessages })
const format = ref<ClientConfigFormat>('codex')
const selectedModel = ref('')
const serverAddress = ref('')
const catalog = ref<ClientConfigCatalog | null>(null)
const configuration = ref<ClientConfiguration | null>(null)
const catalogLoading = ref(false)
const generating = ref(false)
const error = ref('')
const copied = ref(false)
const contextWindow = ref('')
const maxTokens = ref('')
const reasoning = ref('')
const input = ref('')
const catalogRequest = useLatestRequest()
const configRequest = useLatestRequest()
const loading = computed(() => catalogLoading.value || generating.value)
const availableModels = computed(
  () => catalog.value?.protocolModels[format.value] ?? []
)
const piSettings = computed<PiModelSettings | undefined>(() => {
  if (
    !contextWindow.value ||
    !maxTokens.value ||
    !reasoning.value ||
    !input.value
  )
    return undefined
  const value = {
    contextWindow: Number(contextWindow.value),
    maxTokens: Number(maxTokens.value),
    reasoning: reasoning.value === 'yes',
    imageInput: input.value === 'image',
  }
  return validPiSettings(value) ? value : undefined
})

function message(value: unknown): string {
  if (value instanceof ClientConfigError) return t(`error.${value.code}`)
  return value instanceof ApiError ? value.message : t('error.failed')
}

function clearConfiguration() {
  configRequest.cancel()
  configuration.value = null
  copied.value = false
  generating.value = false
  error.value = ''
}

async function load() {
  clearConfiguration()
  catalog.value = null
  selectedModel.value = ''
  catalogLoading.value = true
  const result = await catalogRequest.run((signal) =>
    loadClientConfigCatalog(props.tokenId, signal)
  )
  if (result.stale) return
  catalogLoading.value = false
  if (!result.ok) {
    error.value = message(result.error)
    return
  }
  catalog.value = result.value
  serverAddress.value = result.value.baseURL
  selectedModel.value = result.value.protocolModels[format.value][0] ?? ''
}

async function generate() {
  clearConfiguration()
  if (!catalog.value || !selectedModel.value) return
  if (format.value === 'pi' && !piSettings.value) {
    error.value = t('error.pi_settings_required')
    return
  }
  generating.value = true
  const result = await configRequest.run((signal) =>
    getTokenClientConfig(
      props.tokenId,
      format.value,
      selectedModel.value,
      serverAddress.value,
      piSettings.value,
      signal
    )
  )
  if (result.stale) return
  generating.value = false
  if (!result.ok) error.value = message(result.error)
  else configuration.value = result.value
}

watch(() => props.tokenId, load, { immediate: true })
watch(format, () => {
  selectedModel.value = availableModels.value[0] ?? ''
})
watch([format, selectedModel], () => {
  clearConfiguration()
  contextWindow.value = ''
  maxTokens.value = ''
  reasoning.value = ''
  input.value = ''
  if (format.value === 'codex' && selectedModel.value) void generate()
})
watch([contextWindow, maxTokens, reasoning, input], () => {
  if (format.value === 'pi') clearConfiguration()
})
watch(serverAddress, clearConfiguration, { flush: 'sync' })

function useCurrentSite() {
  serverAddress.value = new URL(
    import.meta.env.BASE_URL,
    window.location.origin
  ).href
}

async function copy() {
  const current = configuration.value
  if (!current) return
  try {
    await navigator.clipboard.writeText(current.content)
    if (configuration.value === current) copied.value = true
  } catch {
    error.value = t('error.copy_failed')
  }
}

function download() {
  if (!configuration.value) return
  const { content, mediaType, filename } = configuration.value
  const url = URL.createObjectURL(new Blob([content], { type: mediaType }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  // Let the browser consume the click before releasing its download URL.
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
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
    <div class="sibling-space-y-4">
      <div class="sibling-space-y-2">
        <label class="block text-sm text-[var(--text-secondary)]">
          <span>{{ t('server_address') }}</span>
          <input
            v-model="serverAddress"
            type="url"
            class="console-input mt-1 w-full"
            :aria-label="t('server_address')"
            autocomplete="off"
            spellcheck="false"
          />
        </label>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('server_address_hint') }}
        </p>
        <ConsoleButton variant="secondary" size="sm" @click="useCurrentSite">{{
          t('current_site')
        }}</ConsoleButton>
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-sm text-[var(--text-secondary)]">
          <span>{{ t('client') }}</span>
          <select
            v-model="format"
            class="console-input mt-1 w-full"
            :aria-label="t('client')"
          >
            <option value="codex">Codex</option>
            <option value="pi">Pi</option>
          </select>
        </label>
        <label class="min-w-0 text-sm text-[var(--text-secondary)]">
          <span>{{ t('model') }}</span>
          <select
            v-model="selectedModel"
            class="console-input mt-1 w-full min-w-0"
            :disabled="!catalog"
            :aria-label="t('model')"
          >
            <option
              v-for="model in availableModels"
              :key="model"
              :value="model"
            >
              {{ model }}
            </option>
          </select>
        </label>
      </div>
      <template v-if="format === 'pi'">
        <p class="text-sm text-[var(--text-secondary)]">{{ t('pi_limits') }}</p>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="text-sm text-[var(--text-secondary)]">
            <span>{{ t('context_window') }}</span>
            <input
              v-model="contextWindow"
              type="number"
              min="1"
              step="1"
              inputmode="numeric"
              class="console-input mt-1 w-full"
              :aria-label="t('context_window')"
            />
          </label>
          <label class="text-sm text-[var(--text-secondary)]">
            <span>{{ t('max_tokens') }}</span>
            <input
              v-model="maxTokens"
              type="number"
              min="1"
              step="1"
              inputmode="numeric"
              class="console-input mt-1 w-full"
              :aria-label="t('max_tokens')"
            />
          </label>
          <label class="text-sm text-[var(--text-secondary)]">
            <span>{{ t('reasoning') }}</span>
            <select
              v-model="reasoning"
              class="console-input mt-1 w-full"
              :aria-label="t('reasoning')"
            >
              <option value="" disabled>{{ t('choose') }}</option>
              <option value="yes">{{ t('yes') }}</option>
              <option value="no">{{ t('no') }}</option>
            </select>
          </label>
          <label class="text-sm text-[var(--text-secondary)]">
            <span>{{ t('input') }}</span>
            <select
              v-model="input"
              class="console-input mt-1 w-full"
              :aria-label="t('input')"
            >
              <option value="" disabled>{{ t('choose') }}</option>
              <option value="text">{{ t('text') }}</option>
              <option value="image">{{ t('image') }}</option>
            </select>
          </label>
        </div>
      </template>
      <p
        v-if="catalog && !availableModels.length"
        role="alert"
        class="text-sm text-[var(--status-danger-text)]"
      >
        {{ t('error.no_protocol_models') }}
      </p>
      <ConsoleButton
        :disabled="loading || !catalog || !selectedModel"
        @click="generate"
        >{{ t('generate') }}</ConsoleButton
      >
      <p v-if="loading" role="status" class="text-sm">{{ t('loading') }}</p>
      <div v-if="error" class="sibling-space-y-2">
        <p role="alert" class="text-sm text-[var(--status-danger-text)]">
          {{ error }}
        </p>
        <ConsoleButton
          variant="secondary"
          :disabled="loading"
          @click="catalog ? generate() : load()"
          >{{ t('retry') }}</ConsoleButton
        >
      </div>
      <template v-if="configuration">
        <textarea
          readonly
          :value="configuration.content"
          :aria-label="t('preview')"
          rows="12"
          spellcheck="false"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs text-[var(--text-primary)]"
        />
        <p class="text-sm text-[var(--text-secondary)]">
          {{ t(format === 'codex' ? 'codex_install' : 'pi_install') }}
        </p>
      </template>
      <div class="sibling-space-y-1 text-xs text-[var(--text-tertiary)]">
        <p v-if="format === 'codex'">{{ t('codex_limits') }}</p>
        <p v-else>{{ t('prices_omitted') }}</p>
        <p>{{ t('route_note') }}</p>
      </div>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton variant="ghost" @click="emit('close')">{{
          t('close')
        }}</ConsoleButton>
        <ConsoleButton
          variant="secondary"
          :disabled="loading || !configuration"
          @click="copy"
          >{{ t(copied ? 'copied' : 'copy') }}</ConsoleButton
        >
        <ConsoleButton
          :disabled="loading || !configuration"
          @click="download"
          >{{ t('download') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
</template>
