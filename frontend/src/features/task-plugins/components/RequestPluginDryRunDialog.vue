<script setup lang="ts">
// Upstream request-plugin-dry-run-dialog.tsx: runs the saved instances on a
// sample request; nothing is sent or charged. Ren2Hub has no business group.
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Play } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { dryRunRequestPlugins } from '../api'
import {
  REQUEST_PLUGIN_DRY_RUN_FORMATS,
  parseRequestPluginBody,
  parseRequestPluginChannel,
  requestPluginDryRunExample,
  requestPluginDryRunPath,
} from '../lib/request-plugins'
import type { RequestPluginDryRunResult } from '../types'
import JsonCodeEditor from './JsonCodeEditor.vue'
import RequestPluginDryRunResultView from './RequestPluginDryRunResult.vue'

const props = defineProps<{
  open: boolean
  instanceName: (id: string) => string
  pluginName: (key: string) => string
}>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const DEFAULT_FORMAT = REQUEST_PLUGIN_DRY_RUN_FORMATS[0]
const formatOptions = REQUEST_PLUGIN_DRY_RUN_FORMATS.map((item) => ({
  value: item.value,
  label: item.label,
}))

const format = ref<string>(DEFAULT_FORMAT.value)
const model = ref<string>(DEFAULT_FORMAT.model)
const channel = ref('')
const body = ref('')
const inputError = ref('')
const runError = ref('')
const sentBody = ref('')
const running = ref(false)
const result = ref<RequestPluginDryRunResult | null>(null)
let request: AbortController | null = null

function cancel() {
  request?.abort()
  request = null
  running.value = false
}

function reset() {
  cancel()
  format.value = DEFAULT_FORMAT.value
  model.value = DEFAULT_FORMAT.model
  channel.value = ''
  body.value = exampleBody(DEFAULT_FORMAT.value, DEFAULT_FORMAT.model)
  inputError.value = ''
  runError.value = ''
  sentBody.value = ''
  result.value = null
}

function exampleBody(value: string, name: string) {
  return JSON.stringify(requestPluginDryRunExample(value, name), null, 2)
}

// Each opening starts afresh, as upstream remounts the form.
watch(
  () => props.open,
  (open) => {
    if (open) reset()
    else cancel()
  },
  { immediate: true }
)

function changeFormat(value: string) {
  const next = REQUEST_PLUGIN_DRY_RUN_FORMATS.find(
    (item) => item.value === value
  )
  if (!next) return
  format.value = next.value
  model.value = next.model
  body.value = exampleBody(next.value, next.model)
}

async function run() {
  const parsed = parseRequestPluginBody(body.value)
  if (!parsed) {
    inputError.value = t('pluginExtensions.rp.bodyNotObject')
    return
  }
  const channelId = parseRequestPluginChannel(channel.value)
  if (channelId === null) {
    inputError.value = t('pluginExtensions.rp.channelInvalid')
    return
  }
  inputError.value = ''
  runError.value = ''
  cancel()
  const controller = new AbortController()
  request = controller
  running.value = true
  const name = model.value.trim()
  const sent = JSON.stringify(parsed, null, 2)
  try {
    const response = await dryRunRequestPlugins(
      {
        format: format.value,
        path: requestPluginDryRunPath(format.value, name),
        model: name,
        channel_id: channelId,
        body: parsed,
      },
      controller.signal
    )
    if (controller.signal.aborted) return
    sentBody.value = sent
    result.value = response
  } catch (cause) {
    if (controller.signal.aborted) return
    result.value = null
    runError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (request === controller) {
      request = null
      running.value = false
    }
  }
}

onBeforeUnmount(cancel)
</script>

<template>
  <ConsoleModal
    :open="open"
    size="xl"
    :title="t('pluginExtensions.rp.dryRun')"
    :subtitle="t('pluginExtensions.rp.dryRunHint')"
    @close="emit('close')"
  >
    <div class="sibling-space-y-5" data-dry-run-dialog>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField :label="t('pluginExtensions.rp.format')" group>
          <FilterSelect
            :model-value="format"
            :options="formatOptions"
            :label="t('pluginExtensions.rp.format')"
            @update:model-value="changeFormat"
          />
        </FormField>
        <FormField :label="t('pluginExtensions.rp.model')">
          <TextInput v-model="model" :placeholder="DEFAULT_FORMAT.model" />
        </FormField>
        <FormField
          :label="t('pluginExtensions.rp.channelId')"
          :hint="t('pluginExtensions.rp.channelHint')"
        >
          <TextInput
            v-model="channel"
            inputmode="numeric"
            :placeholder="t('pluginExtensions.rp.channelPlaceholder')"
          />
        </FormField>
      </div>
      <div class="sibling-space-y-1.5">
        <span class="block text-sm font-medium text-[var(--text-secondary)]">{{
          t('pluginExtensions.rp.body')
        }}</span>
        <JsonCodeEditor
          id="request-plugin-dry-run-body"
          v-model="body"
          :label="t('pluginExtensions.rp.body')"
        />
        <p
          v-if="inputError"
          role="alert"
          class="text-xs text-[var(--status-danger-text)]"
        >
          {{ inputError }}
        </p>
      </div>
      <div class="flex justify-end">
        <ConsoleButton
          :loading="running"
          :disabled="running || !model.trim()"
          @click="run"
        >
          <Play v-if="!running" :size="16" aria-hidden="true" />
          {{ t('pluginExtensions.rp.run') }}
        </ConsoleButton>
      </div>
      <p
        v-if="runError"
        role="alert"
        class="whitespace-pre-wrap break-words text-sm text-[var(--status-danger-text)]"
      >
        {{ runError }}
      </p>
      <RequestPluginDryRunResultView
        v-if="result"
        :result="result"
        :sent-body="sentBody"
        :instance-name="instanceName"
        :plugin-name="pluginName"
      />
    </div>
  </ConsoleModal>
</template>
