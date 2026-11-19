<script setup lang="ts">
// Upstream request-plugin-instance-editor.tsx: a plugin picker for new
// instances, then the settings, where it runs and a try panel. Ren2Hub has no
// business group, so an instance runs for all requests, for the channels
// that add it, or where its plugin's meta.scope says once an administrator
// confirms that scope.
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowLeft,
  Boxes,
  Globe,
  ListChecks,
  Pencil,
  Plug,
  Sparkles,
} from 'lucide-vue-next'

import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import IconButton from '@/components/common/IconButton.vue'
import { localizedText } from '@/utils/localizedText'
import { declaredPluginScope, samePluginScope } from '../lib/plugin-scope'
import type {
  RequestPluginInstance,
  RequestPluginScope,
  TaskPluginOption,
} from '../types'
import PluginConfigEditor from './PluginConfigEditor.vue'
import PluginIcon from './PluginIcon.vue'
import PluginScopeSummary from './PluginScopeSummary.vue'
import RequestPluginPicker from './RequestPluginPicker.vue'
import RequestPluginRunLine from './RequestPluginRunLine.vue'
import RequestPluginTryPanel from './RequestPluginTryPanel.vue'

const props = defineProps<{
  open: boolean
  /** Null adds a new instance, starting with the choice of plugin. */
  instance: RequestPluginInstance | null
  plugins: TaskPluginOption[]
  /** How many instances each plugin already has, by plugin key. */
  instanceCounts: Record<string, number>
  /** Model names to pick from; other names can be typed in. */
  models: string[]
  saving: boolean
  /** The gateway's message for the last save. */
  error: string
}>()
const emit = defineEmits<{
  close: []
  save: [instance: RequestPluginInstance]
}>()
const { t, locale } = useI18n()
const formId = useId()

type Scope = 'plugin' | 'all' | 'channels'
type ModelScope = 'all' | 'chosen'
const PLUGIN_SCOPE = {
  value: 'plugin',
  icon: Sparkles,
  title: 'scopePlugin',
  hint: 'scopePluginHint',
} as const
const SCOPES = [
  { value: 'all', icon: Globe, title: 'scopeAll', hint: 'scopeAllHint' },
  {
    value: 'channels',
    icon: Plug,
    title: 'scopeChannels',
    hint: 'scopeChannelsHint',
  },
] as const
const MODEL_SCOPES = [
  { value: 'all', icon: Boxes, title: 'modelsAll', hint: 'modelsAllHint' },
  {
    value: 'chosen',
    icon: ListChecks,
    title: 'modelsChosen',
    hint: 'modelsChosenHint',
  },
] as const

const pluginKey = ref('')
const name = ref('')
const renaming = ref(false)
const enabled = ref(true)
const scope = ref<Scope>('all')
const modelScope = ref<ModelScope>('all')
const chosenModels = ref<string[]>([])
const config = ref<Record<string, unknown>>({})
const configValid = ref(true)
// Problems show once a save is tried.
const showAllErrors = ref(false)
const nameInput = ref<HTMLInputElement | null>(null)

// Each opening starts afresh, so edits left in a closed editor are gone.
watch(
  () => props.open,
  (open) => {
    if (!open) return
    const instance = props.instance
    pluginKey.value = instance?.plugin ?? ''
    name.value = instance?.name ?? ''
    renaming.value = false
    enabled.value = instance?.enabled ?? true
    if (instance?.plugin_scope) scope.value = 'plugin'
    else scope.value = !instance || instance.global ? 'all' : 'channels'
    chosenModels.value = [...(instance?.models ?? [])]
    modelScope.value = chosenModels.value.length ? 'chosen' : 'all'
    config.value = { ...(instance?.config ?? {}) }
    configValid.value = true
    showAllErrors.value = false
  },
  { immediate: true }
)

const plugin = computed(() =>
  props.plugins.find((item) => item.key === pluginKey.value)
)
// An instance whose plugin is gone keeps its saved settings untouched.
const pluginMissing = computed(() => Boolean(pluginKey.value) && !plugin.value)
const pluginName = computed(() =>
  plugin.value
    ? localizedText(plugin.value.name_i18n, locale.value) || plugin.value.name
    : pluginKey.value
)
const hasSettings = computed(
  () => Object.keys(plugin.value?.configSchema?.properties ?? {}).length > 0
)
const declaredScope = computed(() => declaredPluginScope(plugin.value))
const savedScope = computed(() => props.instance?.plugin_scope ?? null)
// The scope the instance confirms: the plugin's current one, or the one a
// saved instance confirmed when the plugin has none to offer any more.
const confirmedScope = computed<RequestPluginScope | null>(
  () => declaredScope.value ?? savedScope.value
)
const scopeChanged = computed(
  () =>
    savedScope.value !== null &&
    declaredScope.value !== null &&
    !samePluginScope(savedScope.value, declaredScope.value)
)
const followsPlugin = computed(
  () => scope.value === 'plugin' && confirmedScope.value !== null
)
const scopeOptions = computed(() =>
  confirmedScope.value ? [PLUGIN_SCOPE, ...SCOPES] : SCOPES
)
const modelsMissing = computed(
  () =>
    !followsPlugin.value &&
    modelScope.value === 'chosen' &&
    chosenModels.value.length === 0
)
const modelOptions = computed(() => [
  ...new Set([...props.models, ...chosenModels.value]),
])
const draft = computed<RequestPluginInstance>(() => {
  const instance: RequestPluginInstance = {
    id: props.instance?.id ?? '',
    name: name.value.trim() || pluginName.value,
    plugin: pluginKey.value,
    enabled: enabled.value,
    global: scope.value === 'all',
    models: modelScope.value === 'chosen' ? [...chosenModels.value] : [],
    config: { ...config.value },
  }
  if (!followsPlugin.value || !confirmedScope.value) return instance
  return {
    ...instance,
    global: false,
    models: [],
    plugin_scope: {
      models: [...confirmedScope.value.models],
      channel_types: [...confirmedScope.value.channel_types],
    },
  }
})

function pick(key: string) {
  const next = props.plugins.find((item) => item.key === key)
  pluginKey.value = key
  name.value = next
    ? localizedText(next.name_i18n, locale.value) || next.name
    : key
  // A new instance takes the plugin's suggested place when it has one.
  scope.value = declaredPluginScope(next) ? 'plugin' : 'all'
  config.value = {}
  configValid.value = true
  showAllErrors.value = false
}

async function startRename() {
  renaming.value = true
  await nextTick()
  nameInput.value?.focus()
}

function finishRename() {
  renaming.value = false
  if (!name.value.trim()) name.value = pluginName.value
}

function onRenameKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' && event.key !== 'Escape') return
  // Ends the rename without saving or closing the editor.
  event.preventDefault()
  event.stopPropagation()
  finishRename()
}

function submit() {
  if ((!pluginMissing.value && !configValid.value) || modelsMissing.value) {
    showAllErrors.value = true
    return
  }
  emit('save', {
    ...draft.value,
    config: pluginMissing.value
      ? { ...(props.instance?.config ?? {}) }
      : draft.value.config,
  })
}

function close() {
  if (!props.saving) emit('close')
}
</script>

<template>
  <ConsoleModal
    v-if="!pluginKey"
    :open="open"
    size="xl"
    :title="t('pluginExtensions.rp.addTitle')"
    :subtitle="t('pluginExtensions.rp.addHint')"
    @close="close"
  >
    <RequestPluginPicker
      :plugins="plugins"
      :instance-counts="instanceCounts"
      @pick="pick"
    />
  </ConsoleModal>
  <ConsoleModal
    v-else
    :open="open"
    :size="plugin ? 'wide' : 'lg'"
    :close-disabled="saving"
    :title="name || pluginName"
    @close="close"
  >
    <template #header="{ titleId, subtitleId }">
      <div class="flex items-center gap-3.5 text-left" data-editor-title>
        <span
          aria-hidden="true"
          class="grid h-12 w-12 shrink-0 place-items-center rounded-xl bg-[var(--surface-muted)]"
        >
          <PluginIcon
            :plugin="plugin ?? { key: pluginKey, name: pluginKey }"
            :size="26"
          />
        </span>
        <span class="flex min-w-0 flex-1 flex-col gap-1">
          <span class="flex min-w-0 items-center gap-1.5">
            <input
              v-if="renaming"
              ref="nameInput"
              v-model="name"
              :aria-label="t('pluginExtensions.rp.name')"
              maxlength="64"
              class="h-8 w-full max-w-sm rounded-lg border border-[var(--border-strong)] bg-[var(--surface-solid)] px-2 text-base font-semibold text-[var(--text-primary)] outline-none focus-ring"
              @blur="finishRename"
              @keydown="onRenameKeydown"
            />
            <template v-else>
              <h2
                :id="titleId"
                class="truncate text-lg font-semibold text-[var(--text-primary)]"
              >
                {{ name || pluginName }}
              </h2>
              <IconButton
                :label="t('pluginExtensions.rp.rename')"
                :disabled="saving"
                @click="startRename"
              >
                <Pencil :size="14" aria-hidden="true" />
              </IconButton>
            </template>
          </span>
          <RequestPluginRunLine v-if="plugin" :plugin="plugin" />
        </span>
      </div>
      <p
        v-if="plugin"
        :id="subtitleId"
        class="mt-3 text-left text-sm text-[var(--text-secondary)]"
      >
        {{ localizedText(plugin.description, locale) }}
      </p>
    </template>
    <div
      :class="
        plugin
          ? 'grid gap-8 lg:grid-cols-[minmax(0,1fr)_360px]'
          : 'sibling-space-y-7'
      "
      data-instance-editor
    >
      <form
        :id="formId"
        class="sibling-space-y-7 min-w-0"
        novalidate
        @submit.prevent="submit"
      >
        <section class="sibling-space-y-2.5">
          <h3 class="text-xs font-medium text-[var(--text-tertiary)]">
            {{ t('pluginExtensions.rp.settings') }}
          </h3>
          <template v-if="plugin">
            <fieldset v-if="hasSettings" :disabled="saving">
              <PluginConfigEditor
                :key="pluginKey"
                v-model="config"
                :schema="plugin.configSchema"
                @validity="configValid = $event"
              />
            </fieldset>
            <p
              v-else
              class="rounded-xl border border-[var(--border-subtle)] p-4 text-sm text-[var(--text-tertiary)]"
            >
              {{ t('pluginExtensions.rp.noSettings') }}
            </p>
            <p
              v-if="showAllErrors && !configValid"
              role="alert"
              class="text-xs text-[var(--status-danger-text)]"
            >
              {{ t('pluginExtensions.invalidConfig') }}
            </p>
          </template>
          <p
            v-else
            class="rounded-xl border border-[var(--border-subtle)] p-4 text-sm text-[var(--text-tertiary)]"
          >
            {{ t('pluginExtensions.rp.pluginMissing') }}
          </p>
        </section>
        <section class="sibling-space-y-2.5">
          <h3
            :id="`${formId}-scope`"
            class="text-xs font-medium text-[var(--text-tertiary)]"
          >
            {{ t('pluginExtensions.rp.whereItRuns') }}
          </h3>
          <div
            role="radiogroup"
            :aria-labelledby="`${formId}-scope`"
            class="grid gap-2"
            :class="confirmedScope ? 'sm:grid-cols-3' : 'sm:grid-cols-2'"
          >
            <label
              v-for="item in scopeOptions"
              :key="item.value"
              class="scope-card"
              :data-checked="scope === item.value || undefined"
            >
              <span class="min-w-0 flex-1 sibling-space-y-1">
                <span class="flex items-center gap-1.5 text-sm font-medium">
                  <component
                    :is="item.icon"
                    :size="16"
                    aria-hidden="true"
                    class="text-[var(--text-tertiary)]"
                  />
                  {{ t(`pluginExtensions.rp.${item.title}`) }}
                </span>
                <span class="block text-xs text-[var(--text-tertiary)]">{{
                  t(`pluginExtensions.rp.${item.hint}`)
                }}</span>
              </span>
              <input
                v-model="scope"
                type="radio"
                :name="`${formId}-scope`"
                :value="item.value"
                :disabled="saving"
                class="scope-radio"
              />
            </label>
          </div>
          <div
            v-if="followsPlugin && confirmedScope"
            class="sibling-space-y-2 rounded-xl border border-[var(--border-subtle)] p-3"
            data-plugin-scope-confirm
          >
            <p v-if="scopeChanged" class="text-sm font-medium">
              {{ t('pluginExtensions.rp.scopeChangedNotice') }}
            </p>
            <p v-if="plugin && !declaredScope" class="text-sm font-medium">
              {{ t('pluginExtensions.rp.scopeDroppedNotice') }}
            </p>
            <PluginScopeSummary :scope="confirmedScope" />
          </div>
          <div
            v-if="!followsPlugin"
            role="radiogroup"
            :aria-label="t('pluginExtensions.rp.models')"
            class="grid gap-2 pt-1 sm:grid-cols-2"
          >
            <label
              v-for="item in MODEL_SCOPES"
              :key="item.value"
              class="scope-card"
              :data-checked="modelScope === item.value || undefined"
            >
              <span class="min-w-0 flex-1 sibling-space-y-1">
                <span class="flex items-center gap-1.5 text-sm font-medium">
                  <component
                    :is="item.icon"
                    :size="16"
                    aria-hidden="true"
                    class="text-[var(--text-tertiary)]"
                  />
                  {{ t(`pluginExtensions.rp.${item.title}`) }}
                </span>
                <span class="block text-xs text-[var(--text-tertiary)]">{{
                  t(`pluginExtensions.rp.${item.hint}`)
                }}</span>
              </span>
              <input
                v-model="modelScope"
                type="radio"
                :name="`${formId}-models`"
                :value="item.value"
                :disabled="saving"
                class="scope-radio"
              />
            </label>
          </div>
          <ChipPicker
            v-if="!followsPlugin && modelScope === 'chosen'"
            v-model="chosenModels"
            :options="modelOptions"
            :placeholder="t('pluginExtensions.rp.modelsPlaceholder')"
            creatable
            :disabled="saving"
          />
          <p
            v-if="showAllErrors && modelsMissing"
            role="alert"
            class="text-xs text-[var(--status-danger-text)]"
          >
            {{ t('pluginExtensions.rp.modelsRequired') }}
          </p>
        </section>
        <p
          v-if="error"
          role="alert"
          class="whitespace-pre-wrap break-words text-sm text-[var(--status-danger-text)]"
        >
          {{ error }}
        </p>
      </form>
      <RequestPluginTryPanel
        v-if="plugin"
        :plugin="plugin"
        :instance="configValid ? draft : null"
      />
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-3">
          <ConsoleButton
            v-if="!instance"
            variant="ghost"
            size="sm"
            :disabled="saving"
            @click="pluginKey = ''"
          >
            <ArrowLeft :size="14" aria-hidden="true" />
            {{ t('pluginExtensions.rp.otherPlugins') }}
          </ConsoleButton>
          <label class="flex items-center gap-2.5 text-sm">
            <ConsoleToggle
              v-model="enabled"
              :disabled="saving"
              :label="t('pluginExtensions.rp.enabled')"
            />
            {{ t('pluginExtensions.rp.enabled') }}
          </label>
        </div>
        <div class="flex gap-2">
          <ConsoleButton variant="secondary" :disabled="saving" @click="close">
            {{ t('pluginExtensions.rp.cancel') }}
          </ConsoleButton>
          <ConsoleButton type="submit" :form="formId" :loading="saving">
            {{
              saving
                ? t('pluginExtensions.rp.saving')
                : t('pluginExtensions.rp.save')
            }}
          </ConsoleButton>
        </div>
      </div>
    </template>
  </ConsoleModal>
</template>

<style scoped>
.scope-card {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  padding: 0.875rem 1rem;
  cursor: pointer;
  color: var(--text-primary);
  transition:
    border-color 150ms,
    background-color 150ms;
}
.scope-card:hover {
  background: var(--state-hover-layer);
}
.scope-card[data-checked] {
  border-color: var(--accent);
  background: var(--accent-soft, var(--state-hover-layer));
}
.scope-card:has(:focus-visible) {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.scope-radio {
  margin-top: 0.125rem;
  height: 1rem;
  width: 1rem;
  flex-shrink: 0;
  accent-color: var(--accent);
}
</style>
