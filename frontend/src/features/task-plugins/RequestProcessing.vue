<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import { localizedText } from '@/utils/localizedText'
import {
  requestPluginSampleBody,
  requestPluginSamplesFor,
} from './lib/request-plugin-samples'
import { useToast } from '@/composables/useToast'
import {
  getTaskPluginOptions,
  listRequestPluginInstances,
  saveRequestPluginInstances,
  dryRunRequestPlugins,
} from './api'
import type {
  RequestPluginInstance,
  RequestPluginDryRunResult,
  TaskPluginOption,
  RequestPluginSample,
} from './types'
import PluginConfigEditor from './components/PluginConfigEditor.vue'
const { t, locale } = useI18n()
const toast = useToast()
const instances = ref<RequestPluginInstance[]>([])
const options = ref<TaskPluginOption[]>([])
const configValidity = ref<Record<string, boolean>>({})
const baseline = ref('[]')
const error = ref('')
const loading = ref(true)
const saving = ref(false)
const running = ref(false)
const result = ref<RequestPluginDryRunResult | null>(null)
const format = ref('openai')
const path = ref('/v1/chat/completions')
const model = ref('')
const channel = ref('')
const body = ref('{\n  "messages": [{"role":"user","content":"Hello"}]\n}')
const reply = ref('')
const request = new AbortController()
const dirty = computed(() => JSON.stringify(instances.value) !== baseline.value)
const textError = (cause: unknown) =>
  cause instanceof Error ? cause.message : String(cause)
const pluginOptions = computed(() =>
  options.value.filter((item) => item.transform)
)
async function load() {
  error.value = ''
  loading.value = true
  try {
    const [rows, plugins] = await Promise.all([
      listRequestPluginInstances(request.signal),
      getTaskPluginOptions(request.signal),
    ])
    if (request.signal.aborted) return
    instances.value = rows
    options.value = plugins
    baseline.value = JSON.stringify(rows)
  } catch (cause) {
    if (!request.signal.aborted) error.value = textError(cause)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
function add() {
  instances.value.push({
    id: crypto.randomUUID(),
    name: '',
    plugin: pluginOptions.value[0]?.key ?? '',
    enabled: true,
    global: false,
    models: [],
    config: {},
  })
}
function move(index: number, delta: number) {
  const item = instances.value.splice(index, 1)[0]
  if (item) instances.value.splice(index + delta, 0, item)
}
async function save() {
  if (
    instances.value.some(
      (instance) => configValidity.value[instance.id] === false
    )
  ) {
    error.value = t('pluginExtensions.invalidConfig')
    return
  }
  saving.value = true
  error.value = ''
  try {
    const rows = await saveRequestPluginInstances(
      instances.value,
      request.signal
    )
    if (request.signal.aborted) return
    instances.value = rows
    baseline.value = JSON.stringify(rows)
    toast.success(t('pluginExtensions.saved'))
  } catch (cause) {
    if (!request.signal.aborted) error.value = textError(cause)
  } finally {
    saving.value = false
  }
}
async function run(instance?: RequestPluginInstance) {
  if (
    running.value ||
    (instance && configValidity.value[instance.id] === false)
  )
    return
  running.value = true
  error.value = ''
  result.value = null
  try {
    const parsed: unknown = JSON.parse(body.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
      throw new Error(t('pluginExtensions.invalidConfig'))
    const channelId = channel.value.trim() ? Number(channel.value) : undefined
    if (
      channelId !== undefined &&
      (!Number.isSafeInteger(channelId) || channelId <= 0)
    )
      throw new Error(t('pluginExtensions.channel'))
    const response = await dryRunRequestPlugins(
      {
        format: format.value,
        path: path.value,
        model: model.value,
        body: parsed,
        channel_id: channelId,
        ...(reply.value ? { reply: reply.value } : {}),
        ...(instance ? { instance } : {}),
      },
      request.signal
    )
    if (!request.signal.aborted) result.value = response
  } catch (cause) {
    if (!request.signal.aborted) error.value = textError(cause)
  } finally {
    running.value = false
  }
}
function loadSample(sample: RequestPluginSample) {
  format.value = sample.format
  path.value =
    sample.format === 'openai_responses'
      ? '/v1/responses'
      : sample.format === 'claude'
        ? '/v1/messages'
        : sample.format === 'gemini'
          ? `/v1beta/models/${model.value || 'gemini-2.5-flash'}:generateContent`
          : '/v1/chat/completions'
  // A local PNG keeps image examples deterministic and never sends a remote image URL.
  const image =
    'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII='
  body.value = JSON.stringify(
    requestPluginSampleBody(sample.body, sample.format, image),
    null,
    2
  )
  const sampleModel = sample.body.model
  if (typeof sampleModel === 'string') model.value = sampleModel
  reply.value = sample.reply ?? ''
}
function modelsChanged(instance: RequestPluginInstance, event: Event) {
  instance.models = (event.target as HTMLTextAreaElement).value
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
}
onBeforeRouteLeave(() =>
  saving.value
    ? false
    : !dirty.value || window.confirm(t('pluginExtensions.unsaved'))
)
onBeforeUnmount(() => request.abort())
void load()
</script>
<template>
  <div class="min-w-0 sibling-space-y-4">
    <PageBreadcrumb
      :crumbs="[t('nav.groupAdmin'), t('pluginExtensions.title')]"
    />
    <p class="text-sm text-[var(--text-secondary)]">
      {{ t('pluginExtensions.intro') }}
    </p>
    <ErrorBanner v-if="error" :message="error" />
    <div class="flex flex-wrap gap-2">
      <ConsoleButton
        :disabled="loading || saving || !pluginOptions.length"
        @click="add"
        >{{ t('pluginExtensions.add') }}</ConsoleButton
      ><ConsoleButton :disabled="loading || saving || !dirty" @click="save">{{
        t('pluginExtensions.save')
      }}</ConsoleButton>
    </div>
    <p v-if="!loading && !instances.length" class="text-sm">
      {{ t('pluginExtensions.empty') }}
    </p>
    <section
      v-for="(instance, index) in instances"
      :key="instance.id"
      class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 sibling-space-y-3"
    >
      <fieldset :disabled="saving" class="grid gap-3 md:grid-cols-2">
        <FormField :label="t('pluginExtensions.name')"
          ><TextInput v-model="instance.name"
        /></FormField>
        <FormField :label="t('pluginExtensions.plugin')"
          ><select
            v-model="instance.plugin"
            class="plugin-control"
            @change="instance.config = {}"
          >
            <option
              v-if="!pluginOptions.some((p) => p.key === instance.plugin)"
              :value="instance.plugin"
            >
              {{ instance.plugin }} · {{ t('pluginExtensions.unavailable') }}
            </option>
            <option
              v-for="plugin in pluginOptions"
              :key="plugin.key"
              :value="plugin.key"
            >
              {{ localizedText(plugin.name_i18n, locale) || plugin.name }}
            </option>
          </select></FormField
        >
        <div class="flex items-center gap-2">
          <ConsoleToggle
            v-model="instance.enabled"
            :label="t('pluginExtensions.enabled')"
          />{{ t('pluginExtensions.enabled') }}
        </div>
        <div class="flex items-center gap-2">
          <ConsoleToggle
            v-model="instance.global"
            :label="t('pluginExtensions.global')"
          />{{ t('pluginExtensions.global') }}
        </div>
        <FormField :label="t('pluginExtensions.models')" class="md:col-span-2">
          <textarea
            :value="instance.models.join('\n')"
            class="plugin-control"
            rows="2"
            @change="modelsChanged(instance, $event)"
          />
        </FormField>
        <PluginConfigEditor
          :key="instance.plugin"
          v-model="instance.config"
          :schema="
            pluginOptions.find((p) => p.key === instance.plugin)?.configSchema
          "
          class="md:col-span-2"
          @validity="configValidity[instance.id] = $event"
        />
      </fieldset>
      <div class="flex flex-wrap gap-2">
        <ConsoleButton
          v-for="(sample, sampleIndex) in requestPluginSamplesFor(
            pluginOptions.find((p) => p.key === instance.plugin)?.samples ?? [],
            locale
          )"
          :key="sampleIndex"
          size="sm"
          variant="secondary"
          @click="loadSample(sample)"
          >{{ t('pluginExtensions.sample') }} ·
          {{ localizedText(sample.name, locale) }}</ConsoleButton
        >
      </div>
      <div class="flex flex-wrap gap-2">
        <ConsoleButton
          size="sm"
          variant="secondary"
          :disabled="saving || index === 0"
          @click="move(index, -1)"
          >{{ t('pluginExtensions.up') }}</ConsoleButton
        ><ConsoleButton
          size="sm"
          variant="secondary"
          :disabled="saving || index === instances.length - 1"
          @click="move(index, 1)"
          >{{ t('pluginExtensions.down') }}</ConsoleButton
        ><ConsoleButton
          size="sm"
          variant="secondary"
          :disabled="saving"
          @click="instances.splice(index, 1)"
          >{{ t('pluginExtensions.remove') }}</ConsoleButton
        ><ConsoleButton
          size="sm"
          :disabled="running || !model"
          @click="run(instance)"
          >{{ t('pluginExtensions.runOne') }}</ConsoleButton
        >
      </div>
    </section>
    <section
      class="rounded-xl border border-[var(--border-subtle)] p-4 sibling-space-y-3"
    >
      <h2 class="font-medium">{{ t('pluginExtensions.try') }}</h2>
      <div class="grid gap-3 md:grid-cols-2">
        <FormField :label="t('pluginExtensions.format')"
          ><select v-model="format" class="plugin-control">
            <option
              v-for="value in [
                'openai',
                'openai_responses',
                'claude',
                'gemini',
              ]"
              :key="value"
            >
              {{ value }}
            </option>
          </select></FormField
        >
        <FormField :label="t('pluginExtensions.path')"
          ><TextInput v-model="path"
        /></FormField>
        <FormField :label="t('pluginExtensions.model')"
          ><TextInput v-model="model"
        /></FormField>
        <FormField :label="t('pluginExtensions.channel')"
          ><TextInput v-model="channel" inputmode="numeric"
        /></FormField>
        <FormField :label="t('pluginExtensions.body')" class="md:col-span-2">
          <textarea v-model="body" class="plugin-control font-mono" rows="7" />
        </FormField>
        <FormField :label="t('pluginExtensions.reply')" class="md:col-span-2">
          <textarea v-model="reply" class="plugin-control" rows="3" />
        </FormField>
      </div>
      <ConsoleButton :disabled="running || !model" @click="run()">{{
        t('pluginExtensions.run')
      }}</ConsoleButton>
      <div v-if="result">
        <h3 class="font-medium">{{ t('pluginExtensions.result') }}</h3>
        <pre
          class="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[var(--surface-muted)] p-3 text-xs"
          >{{ JSON.stringify(result, null, 2) }}</pre>
      </div>
    </section>
  </div>
</template>
<style scoped>
.plugin-control {
  width: 100%;
  padding: 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-muted);
  color: var(--text-primary);
}
</style>
