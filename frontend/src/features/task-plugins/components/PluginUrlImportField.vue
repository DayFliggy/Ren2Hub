<script setup lang="ts">
// Upstream components/plugin-url-import-field.tsx. The fetched text only
// fills the source field; uploading stays an explicit action. Ren2Hub adds
// the fetched SHA-256, which the upload sends for server re-verification.
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TextInput from '@/components/common/TextInput.vue'

import {
  computeSourceSha256,
  fetchPluginSourceText,
  normalizePluginSourceUrl,
  PluginSourceFetchError,
} from '../lib/plugin-url'

defineProps<{ error: string; sha256: string }>()
const url = defineModel<string>({ required: true })
const emit = defineEmits<{
  error: [message: string]
  fetched: [source: string, sha256: string | null]
}>()
const { t } = useI18n()
const fetching = ref(false)

function message(error: unknown): string {
  if (!(error instanceof PluginSourceFetchError))
    return error instanceof Error ? error.message : String(error)
  if (error.reason === 'too_large') return t('taskPlugins.sourceTooLarge')
  if (error.reason === 'not_found')
    return t('taskPlugins.urlStatus', { status: error.status ?? 0 })
  return t('taskPlugins.urlUnreachable')
}

async function importUrl() {
  if (!url.value.trim() || fetching.value) return
  const normalized = normalizePluginSourceUrl(url.value)
  if (!normalized) {
    emit('error', t('taskPlugins.enterUrl'))
    return
  }
  fetching.value = true
  try {
    const text = await fetchPluginSourceText(normalized)
    emit('error', '')
    emit('fetched', text, await computeSourceSha256(text))
  } catch (error) {
    emit('error', message(error))
  } finally {
    fetching.value = false
  }
}
</script>

<template>
  <div class="sibling-space-y-1.5">
    <label
      for="task-plugin-url"
      class="block text-sm font-medium text-[var(--text-secondary)]"
      >{{ t('taskPlugins.importFromUrl') }}</label
    >
    <div class="flex gap-2">
      <TextInput
        id="task-plugin-url"
        v-model="url"
        type="url"
        inputmode="url"
        class="min-w-0 flex-1"
        placeholder="https://github.com/owner/repo/blob/main/plugin.js"
        aria-describedby="task-plugin-url-hint"
        :aria-invalid="error ? true : undefined"
        @update:model-value="emit('error', '')"
        @keydown.enter.prevent="importUrl"
      />
      <ConsoleButton
        variant="secondary"
        :disabled="!url.trim()"
        :loading="fetching"
        @click="importUrl"
      >
        <Download v-if="!fetching" :size="14" aria-hidden="true" />
        {{ fetching ? t('taskPlugins.fetching') : t('taskPlugins.fetch') }}
      </ConsoleButton>
    </div>
    <p id="task-plugin-url-hint" class="text-xs text-[var(--text-tertiary)]">
      {{ t('taskPlugins.urlHint') }}
    </p>
    <p
      v-if="sha256"
      class="break-all font-mono text-[11px] text-[var(--text-tertiary)]"
    >
      SHA-256 {{ sha256 }}
    </p>
    <p
      v-if="error"
      role="alert"
      class="text-xs text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
  </div>
</template>
