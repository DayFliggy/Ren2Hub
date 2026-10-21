<script setup lang="ts">
import {
  AlertCircle,
  BookOpen,
  ChevronDown,
  ChevronUp,
  KeyRound,
  Plus,
  RefreshCw,
  Settings,
  Shapes,
  X,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { ApiError } from '@/api/types'
import {
  assessBaseUrlTrust,
  nextTaskPluginBaseUrl,
  normalizeBaseUrlValue,
} from '@/features/channels/lib/task-plugin-base-url'
import { getTaskPluginOptions } from '@/features/task-plugins/api'
import type { TaskPluginOption } from '@/features/task-plugins/types'
import { metadataApi, type PrefillGroup } from '@/api/adminManagement'
import { appendModelNames } from '@/utils/modelEndpoints'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { useAuthStore } from '@/stores/auth'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import AdvancedChannelEditor from './AdvancedChannelEditor.vue'
import ProviderChannelSettings from './ProviderChannelSettings.vue'
import FetchedModelsDialog from './FetchedModelsDialog.vue'
import {
  advancedChannelPreset,
  parseAdvancedChannelConfig,
  parseChannelObject,
  CHANNEL_SETTING_FIELDS,
  type ChannelSettingField,
} from '@/utils/channelSettings'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import { useToast } from '@/composables/useToast'
import {
  ADMIN_CHANNEL_TYPE_META,
  adminChannelTypeMeta,
  isRetiredChannelType,
} from '@/constants/adminChannels'
import type {
  AdminChannel,
  AdminChannelCreateInput,
  AdminChannelCreateOptions,
  AdminChannelFetchModelsParams,
  AdminChannelUpdateInput,
} from '@/types/console'

const props = defineProps<{
  open: boolean
  editing: AdminChannel | null
  save: (
    input: AdminChannelCreateInput | AdminChannelUpdateInput,
    options?: AdminChannelCreateOptions
  ) => Promise<boolean>
  fetchModels: (params: AdminChannelFetchModelsParams) => Promise<string[]>
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const saving = ref(false)
const advancedOpen = ref(false)
const originalSetting = ref<Record<string, unknown>>({})
const originalOtherSettings = ref<Record<string, unknown>>({})
const settingsError = ref('')
const awsApiType = ref('invoke_model')
const awsKeyType = ref('ak_sk')
const vertexKeyType = ref('json')
const vertexProjectId = ref('')
const vertexRegion = ref('')
const responsesWebSocket = ref(false)
const passThroughBody = ref(false)
const tlsInsecureSkipVerify = ref(false)
const paramOverride = ref('')
const headerOverride = ref('')
const fieldSettings = reactive<Partial<Record<ChannelSettingField, boolean>>>(
  {}
)
const visibleFieldSettings = computed(() =>
  CHANNEL_SETTING_FIELDS.filter((field) =>
    (field.types as readonly number[]).includes(form.type)
  )
)
const multiKeyMode = ref<'random' | 'polling'>('random')
const keyMode = ref<'append' | 'replace'>('append')
const existingMultiKey = computed(
  () => props.editing?.channel_info?.is_multi_key === true
)
const validOverrides = computed(() => {
  try {
    parseChannelObject(paramOverride.value)
    parseChannelObject(headerOverride.value)
    return true
  } catch {
    return false
  }
})
const advancedConfig = ref('')
const canSensitiveWrite = computed(() =>
  auth.hasPermission('channel', 'sensitive_write')
)
const isAdvancedChannel = computed(() => [58, 62, 63].includes(form.type))
const supportsResponsesWebSocket = computed(() =>
  [1, 58, 59, 60].includes(form.type)
)
const canBindPlugin = computed(() => auth.hasPermission('task_plugin', 'bind'))
const validAdvancedConfig = computed(() => {
  if (!isAdvancedChannel.value) return true
  try {
    parseAdvancedChannelConfig(advancedConfig.value)
    return true
  } catch {
    return false
  }
})
const pluginOptions = ref<TaskPluginOption[]>([])
const pluginKeys = ref<string[]>([])
const pluginError = ref('')
let pluginOptionsRequest: AbortController | null = null
const availablePlugins = computed(() =>
  pluginOptions.value.filter(
    (item) => form.type !== 60 || item.upstreams?.includes('new_api')
  )
)
watch(
  () => props.open,
  async (open) => {
    pluginOptionsRequest?.abort()
    if (!open || !auth.hasPermission('task_plugin', 'bind')) return
    const request = new AbortController()
    pluginOptionsRequest = request
    pluginError.value = ''
    try {
      const items = await getTaskPluginOptions(request.signal)
      if (!request.signal.aborted) pluginOptions.value = items
    } catch (cause) {
      if (!request.signal.aborted)
        pluginError.value =
          cause instanceof Error ? cause.message : String(cause)
    }
  },
  { immediate: true }
)
onBeforeUnmount(() => pluginOptionsRequest?.abort())

// Upstream channel drawer: binding a task plugin proposes its default base URL
// without overwriting a hand-typed one, and flags plain HTTP / private hosts.
const boundTaskPlugin = computed(() =>
  form.type === 61
    ? pluginOptions.value.find((item) => item.key === pluginKeys.value[0])
    : undefined
)
const taskPluginBaseUrlTrust = computed(() =>
  form.type === 61 ? assessBaseUrlTrust(form.baseUrl) : null
)
// The key bound before the current change; set where a channel loads, so
// loading is never treated as a change.
let boundKeyBeforeChange: string | undefined
function selectPluginKeys() {
  if (form.type !== 61) return
  pluginKeys.value = pluginKeys.value.slice(-1)
  const previous = pluginOptions.value.find(
    (item) => item.key === boundKeyBeforeChange
  )
  const baseUrl = nextTaskPluginBaseUrl(
    form.baseUrl,
    previous?.baseUrl,
    boundTaskPlugin.value?.baseUrl
  )
  if (baseUrl !== null) form.baseUrl = baseUrl
  boundKeyBeforeChange = pluginKeys.value[0]
}

// ── Type search dropdown ───────────────────────────────────────────────────
const typeSearch = ref('')
const typeDropdownOpen = ref(false)

const typeOptions = computed(() =>
  Object.entries(ADMIN_CHANNEL_TYPE_META)
    .filter(([, meta]) => !meta.retired)
    .map(([value, meta]) => ({ value: Number(value), label: meta.label }))
    .sort((a, b) => a.label.localeCompare(b.label))
)

const filteredTypeOptions = computed(() => {
  const q = typeSearch.value.toLowerCase().trim()
  if (!q) return typeOptions.value
  return typeOptions.value.filter((opt) => opt.label.toLowerCase().includes(q))
})

function selectType(typeId: number) {
  if (isRetiredChannelType(typeId)) return
  form.type = typeId
  if ([58, 62, 63].includes(typeId))
    advancedConfig.value = JSON.stringify(
      advancedChannelPreset(typeId),
      null,
      2
    )
  typeSearch.value = ''
  typeDropdownOpen.value = false
}

function closeTypeDropdown() {
  typeDropdownOpen.value = false
  typeSearch.value = ''
}

// Escape closes an open type list only; the dialog stays open. With the list
// closed it reaches the dialog as usual.
function onTypeDropdownEscape(event: KeyboardEvent) {
  if (event.isComposing || !typeDropdownOpen.value) return
  event.preventDefault()
  event.stopPropagation()
  closeTypeDropdown()
}

// ── Form state ─────────────────────────────────────────────────────────────
const form = reactive({
  name: '',
  type: 1,
  enabled: true,
  // Credentials
  key: '',
  addMode: 'single' as 'single' | 'multi' | 'multiKey',
  baseUrl: '',
  // Models
  models: '',
  modelMapping: '',
  // Advanced
  priority: 0,
  weight: 0,
  channel_ratio: '1',
  max_concurrency: 0,
})

// ── Model mapping ──────────────────────────────────────────────────────────
const mappingRows = ref<Array<{ from: string; to: string }>>([])
const modelMappingTab = ref<'visual' | 'json'>('visual')
const modelMappingJson = ref('')

function syncJsonFromRows() {
  const obj: Record<string, string> = {}
  for (const row of mappingRows.value) {
    if (row.from.trim()) obj[row.from.trim()] = row.to.trim()
  }
  const json = Object.keys(obj).length ? JSON.stringify(obj, null, 2) : ''
  modelMappingJson.value = json
  form.modelMapping = json
}

function syncRowsFromJson() {
  try {
    const parsed = JSON.parse(modelMappingJson.value || '{}')
    mappingRows.value = Object.entries(parsed).map(([from, to]) => ({
      from,
      to: String(to),
    }))
    form.modelMapping = modelMappingJson.value
  } catch {
    // keep current rows on parse error
  }
}

function addMappingRow() {
  mappingRows.value.push({ from: '', to: '' })
  if (modelMappingTab.value !== 'visual') modelMappingTab.value = 'visual'
}

function removeMappingRow(index: number) {
  mappingRows.value.splice(index, 1)
  syncJsonFromRows()
}

function onSwitchToVisual() {
  syncRowsFromJson()
  modelMappingTab.value = 'visual'
}

function onSwitchToJson() {
  syncJsonFromRows()
  modelMappingTab.value = 'json'
}

// ── Model tags ─────────────────────────────────────────────────────────────
const modelTags = computed(() =>
  form.models
    .split(',')
    .map((m) => m.trim())
    .filter(Boolean)
)
const modelInput = ref('')
const prefillGroups = ref<PrefillGroup[]>([])
const prefillId = ref('')
const prefillLoading = ref(false)
const prefillError = ref('')
let prefillRequest: AbortController | null = null
const canUsePrefill = computed(() => auth.isAdmin)

async function loadPrefillGroups() {
  prefillRequest?.abort()
  if (!props.open || !canUsePrefill.value) return
  const request = new AbortController()
  prefillRequest = request
  prefillLoading.value = true
  prefillError.value = ''
  try {
    const rows = await metadataApi.groups(request.signal, 'model')
    if (!request.signal.aborted)
      prefillGroups.value = rows.filter((row) => row.type === 'model')
  } catch (cause) {
    if (!request.signal.aborted)
      prefillError.value =
        cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (prefillRequest === request) prefillLoading.value = false
  }
}
function appendPrefillGroup() {
  const group = prefillGroups.value.find(
    (row) => row.id === Number(prefillId.value)
  )
  if (!group || group.type !== 'model') return
  form.models = appendModelNames(modelTags.value, group.items).join(',')
}
watch(
  [() => props.open, canUsePrefill],
  () => {
    prefillRequest?.abort()
    prefillId.value = ''
    prefillGroups.value = []
    if (props.open && canUsePrefill.value) void loadPrefillGroups()
  },
  { immediate: true }
)
onBeforeUnmount(() => prefillRequest?.abort())

function addModelTag(value: string) {
  const tags = value
    .split(',')
    .map((m) => m.trim())
    .filter(Boolean)
  form.models = appendModelNames(modelTags.value, tags).join(',')
  modelInput.value = ''
}

function removeModelTag(tag: string) {
  form.models = modelTags.value.filter((t) => t !== tag).join(',')
}

function onModelInputKeydown(e: KeyboardEvent) {
  if (e.isComposing) return
  const val = modelInput.value.trim()
  if ((e.key === 'Enter' || e.key === ',') && val) {
    e.preventDefault()
    addModelTag(val)
  } else if (
    e.key === 'Backspace' &&
    !modelInput.value &&
    modelTags.value.length
  ) {
    removeModelTag(modelTags.value[modelTags.value.length - 1]!)
  }
}

// ── Quick actions ──────────────────────────────────────────────────────────
const fetchingUpstream = ref(false)
const fetchedModels = ref<string[]>([])
const modelPickerOpen = ref(false)
let fetchEpoch = 0
function applyFetchedModels(models: string[]) {
  form.models = appendModelNames(
    modelTags.value.filter((model) => !fetchedModels.value.includes(model)),
    models
  ).join(',')
  modelPickerOpen.value = false
}
const canFetchUpstream = computed(() =>
  props.editing !== null
    ? auth.hasPermission(
        'channel',
        isAdvancedChannel.value && canSensitiveWrite.value
          ? 'sensitive_write'
          : 'operate'
      )
    : canSensitiveWrite.value && form.key.trim().length > 0
)

/**
 * Discover upstream models: saved channels reuse stored credentials (by id),
 * unsaved ones probe with the key/address currently in the form.
 */
async function fetchUpstream() {
  if (fetchingUpstream.value || !canFetchUpstream.value) return
  fetchingUpstream.value = true
  const epoch = ++fetchEpoch
  try {
    const models = await props.fetchModels(
      props.editing !== null
        ? {
            channelId: props.editing.id,
            ...(isAdvancedChannel.value && canSensitiveWrite.value
              ? { advancedCustom: advancedConfig.value, baseUrl: form.baseUrl }
              : {}),
          }
        : {
            type: form.type,
            key: form.key.trim(),
            baseUrl: form.baseUrl.trim(),
            ...(isAdvancedChannel.value
              ? { advancedCustom: advancedConfig.value }
              : {}),
          }
    )
    if (epoch !== fetchEpoch || !props.open) return
    fetchedModels.value = [...new Set(models)]
    modelPickerOpen.value = true
    toast.success(t('channels.upstreamFetched', { count: models.length }))
  } catch (error) {
    if (epoch === fetchEpoch)
      toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    if (epoch === fetchEpoch) fetchingUpstream.value = false
  }
}

async function copyAllModels() {
  if (modelTags.value.length === 0) return
  try {
    await navigator.clipboard.writeText(form.models)
    toast.success(t('common.copied'))
  } catch {
    toast.error(t('common.copyFailed'))
  }
}

// ── Computed helpers ───────────────────────────────────────────────────────
const selectedTypeMeta = computed(() => adminChannelTypeMeta(form.type))
const valid = computed(
  () =>
    !settingsError.value &&
    validAdvancedConfig.value &&
    validOverrides.value &&
    form.name.trim().length > 0 &&
    (![60, 61, 62, 63].includes(form.type) || form.baseUrl.trim().length > 0) &&
    (props.editing !== null || form.key.trim().length > 0) &&
    (props.editing !== null || modelTags.value.length > 0) &&
    Number.isSafeInteger(form.type) &&
    Object.hasOwn(ADMIN_CHANNEL_TYPE_META, form.type) &&
    !isRetiredChannelType(form.type) &&
    !isRetiredChannelType(props.editing?.type ?? form.type) &&
    Number.isSafeInteger(form.priority) &&
    Number.isSafeInteger(form.weight) &&
    form.weight >= 0 &&
    form.weight <= 1_000_000 &&
    isValidChannelRatio(form.channel_ratio) &&
    Number.isSafeInteger(form.max_concurrency) &&
    form.max_concurrency >= 0
)

// ── Reset on open ──────────────────────────────────────────────────────────
watch(
  () => props.open,
  (open) => {
    fetchEpoch++
    fetchingUpstream.value = false
    modelPickerOpen.value = false
    if (!open) return
    const ch = props.editing
    form.name = ch?.name ?? ''
    form.type = ch?.type ?? 1
    form.enabled = ch ? ch.status === 1 : true
    form.key = ''
    form.addMode = 'single'
    multiKeyMode.value =
      ch?.channel_info?.multi_key_mode === 'polling' ? 'polling' : 'random'
    keyMode.value = 'append'
    paramOverride.value =
      typeof ch?.param_override === 'string' ? ch.param_override : ''
    headerOverride.value =
      typeof ch?.header_override === 'string' ? ch.header_override : ''
    form.baseUrl = ch?.base_url ?? ''
    form.models = ch?.models ?? ''
    form.modelMapping =
      typeof ch?.model_mapping === 'string' ? (ch.model_mapping ?? '') : ''
    form.priority = ch?.priority ?? 0
    form.weight = ch?.weight ?? 0
    form.channel_ratio = ch?.channel_ratio ?? '1'
    form.max_concurrency = ch?.max_concurrency ?? 0
    settingsError.value = ''
    try {
      const setting = parseChannelObject(ch?.setting)
      originalSetting.value = setting
      originalOtherSettings.value = parseChannelObject(ch?.settings)
      awsApiType.value =
        typeof originalOtherSettings.value.aws_api_type === 'string' &&
        originalOtherSettings.value.aws_api_type
          ? originalOtherSettings.value.aws_api_type
          : 'invoke_model'
      awsKeyType.value =
        originalOtherSettings.value.aws_key_type === 'api_key'
          ? 'api_key'
          : 'ak_sk'
      vertexProjectId.value =
        typeof originalOtherSettings.value.vertex_project_id === 'string'
          ? originalOtherSettings.value.vertex_project_id
          : ''
      vertexKeyType.value =
        originalOtherSettings.value.vertex_key_type === 'api_key'
          ? 'api_key'
          : 'json'
      vertexRegion.value = typeof ch?.other === 'string' ? ch.other : ''
      for (const field of CHANNEL_SETTING_FIELDS)
        fieldSettings[field.key] =
          originalOtherSettings.value[field.key] === true
      responsesWebSocket.value = setting.responses_websocket_enabled === true
      passThroughBody.value = setting.pass_through_body_enabled === true
      tlsInsecureSkipVerify.value = setting.tls_insecure_skip_verify === true
      advancedConfig.value = JSON.stringify(
        originalOtherSettings.value.advanced_custom ??
          advancedChannelPreset(form.type),
        null,
        2
      )
      pluginKeys.value =
        ch?.type === 61
          ? typeof setting.task_plugin_key === 'string' &&
            setting.task_plugin_key
            ? [setting.task_plugin_key]
            : []
          : Array.isArray(setting.task_extend_plugin_keys)
            ? setting.task_extend_plugin_keys.filter(
                (key): key is string => typeof key === 'string'
              )
            : []
    } catch {
      pluginKeys.value = []
      settingsError.value = t('channels.invalidStoredSettings')
    }
    boundKeyBeforeChange = pluginKeys.value[0]
    // parse mapping
    modelMappingJson.value = form.modelMapping
    try {
      const parsed = form.modelMapping ? JSON.parse(form.modelMapping) : {}
      mappingRows.value = Object.entries(parsed).map(([from, to]) => ({
        from,
        to: String(to),
      }))
    } catch {
      mappingRows.value = []
    }
    modelMappingTab.value = 'visual'
    typeSearch.value = ''
    typeDropdownOpen.value = false
    advancedOpen.value = false
    modelInput.value = ''
  },
  { immediate: true }
)

// ── Submit ─────────────────────────────────────────────────────────────────
function close() {
  if (!saving.value) emit('close')
}

async function submit() {
  if (
    !valid.value ||
    saving.value ||
    !auth.hasPermission('channel', props.editing ? 'write' : 'sensitive_write')
  )
    return
  saving.value = true
  try {
    if (modelMappingTab.value === 'visual') syncJsonFromRows()
    const base: AdminChannelUpdateInput = {
      name: form.name.trim(),
      type: form.type,
      base_url: form.baseUrl.trim(),
      models: form.models.trim(),
      model_mapping: form.modelMapping.trim(),
      priority: Number(form.priority),
      weight: Number(form.weight),
      channel_ratio: form.channel_ratio,
      max_concurrency: form.max_concurrency,
    }
    const settings = { ...originalSetting.value }
    if (canSensitiveWrite.value) {
      if (form.type === 41) {
        if (vertexRegion.value.trim() !== (props.editing?.other ?? ''))
          base.other = vertexRegion.value.trim()
      }
    }
    if (
      canSensitiveWrite.value &&
      passThroughBody.value !== (settings.pass_through_body_enabled === true)
    )
      settings.pass_through_body_enabled = passThroughBody.value
    if (
      canSensitiveWrite.value &&
      tlsInsecureSkipVerify.value !==
        (settings.tls_insecure_skip_verify === true)
    )
      settings.tls_insecure_skip_verify = tlsInsecureSkipVerify.value
    if (
      canSensitiveWrite.value &&
      supportsResponsesWebSocket.value &&
      responsesWebSocket.value !==
        (settings.responses_websocket_enabled === true)
    )
      settings.responses_websocket_enabled = responsesWebSocket.value
    if (canBindPlugin.value && (form.type === 60 || form.type === 61)) {
      settings.task_plugin_key =
        form.type === 61 ? (pluginKeys.value[0] ?? '') : ''
      settings.task_extend_plugin_keys =
        form.type === 60 ? pluginKeys.value : []
    }
    if (JSON.stringify(settings) !== JSON.stringify(originalSetting.value))
      base.setting = JSON.stringify(settings)
    if (canSensitiveWrite.value) {
      if (paramOverride.value !== (props.editing?.param_override ?? ''))
        base.param_override = paramOverride.value.trim()
      if (headerOverride.value !== (props.editing?.header_override ?? ''))
        base.header_override = headerOverride.value.trim()
      if (
        existingMultiKey.value &&
        multiKeyMode.value !== props.editing?.channel_info?.multi_key_mode
      )
        base.multi_key_mode = multiKeyMode.value
    }
    if (canSensitiveWrite.value) {
      const otherSettings = { ...originalOtherSettings.value }
      if (form.type === 33) {
        if (awsApiType.value !== (otherSettings.aws_api_type || 'invoke_model'))
          otherSettings.aws_api_type = awsApiType.value
        if (awsKeyType.value !== (otherSettings.aws_key_type || 'ak_sk'))
          otherSettings.aws_key_type = awsKeyType.value
      }
      if (form.type === 41) {
        const project =
          vertexKeyType.value === 'api_key' ? vertexProjectId.value.trim() : ''
        if (project !== (otherSettings.vertex_project_id ?? ''))
          otherSettings.vertex_project_id = project
      }
      if (
        form.type === 41 &&
        vertexKeyType.value !== (otherSettings.vertex_key_type || 'json')
      )
        otherSettings.vertex_key_type = vertexKeyType.value
      for (const field of visibleFieldSettings.value) {
        const value = fieldSettings[field.key] === true
        if (value !== (originalOtherSettings.value[field.key] === true))
          otherSettings[field.key] = value
      }
      if (isAdvancedChannel.value)
        otherSettings.advanced_custom = parseAdvancedChannelConfig(
          advancedConfig.value
        )
      if (
        JSON.stringify(otherSettings) !==
        JSON.stringify(originalOtherSettings.value)
      )
        base.settings = JSON.stringify(otherSettings)
    }

    if (props.editing === null) {
      const input: AdminChannelCreateInput = {
        ...base,
        key: form.key.trim(),
        status: form.enabled ? 1 : 2,
      }
      const created = await props.save(input, {
        batchKeys: form.addMode === 'multi',
        ...(form.addMode === 'multiKey'
          ? { multiKey: true, multiKeyMode: multiKeyMode.value }
          : {}),
      })
      if (created) emit('close')
      return
    }

    // The key rides along only when actually replaced, so admins without
    // ChannelSensitiveWrite can still save routing-level edits.
    const replacementKey = form.key.trim()
    if (canSensitiveWrite.value && replacementKey) base.key = replacementKey
    if (canSensitiveWrite.value && replacementKey && existingMultiKey.value)
      base.key_mode = keyMode.value
    if (await props.save(base)) emit('close')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="editing ? t('channels.editTitle') : t('channels.createTitle')"
    size="lg"
    @close="close"
  >
    <div class="sibling-space-y-4 text-left">
      <p
        v-if="settingsError"
        role="alert"
        class="text-sm text-[var(--status-danger)]"
      >
        {{ settingsError }}
      </p>
      <FormField
        v-if="canBindPlugin && (form.type === 60 || form.type === 61)"
        :label="t('taskPlugins.binding')"
        :hint="t('taskPlugins.bindingHint')"
      >
        <select
          v-model="pluginKeys"
          multiple
          :size="Math.min(5, Math.max(2, availablePlugins.length))"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
          @change="selectPluginKeys"
        >
          <option
            v-for="plugin in availablePlugins"
            :key="plugin.key"
            :value="plugin.key"
          >
            {{ plugin.name || plugin.key }}
          </option>
        </select>
        <p
          v-if="pluginError"
          role="alert"
          class="text-sm text-[var(--status-danger)]"
        >
          {{ pluginError }}
        </p>
      </FormField>
      <!-- ══ Section: 基本信息 ══════════════════════════════════════════ -->
      <section class="channel-form-section channel-form-section--basic">
        <div class="channel-form-section-header">
          <div class="channel-form-section-icon">
            <Shapes :size="16" />
          </div>
          <div>
            <p class="channel-form-section-title">
              {{ t('channels.sectionBasic') }}
            </p>
            <p class="channel-form-section-desc">
              {{ t('channels.sectionBasicDesc') }}
            </p>
          </div>
        </div>

        <div class="channel-form-section-body">
          <!-- Type searchable dropdown + Name -->
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <p
                class="mb-1.5 text-sm font-medium text-[var(--text-secondary)]"
              >
                {{ t('channels.type') }}
                <span class="text-[var(--status-danger)]">*</span>
              </p>
              <!-- Custom searchable type selector -->
              <div class="relative" @keydown.esc="onTypeDropdownEscape">
                <button
                  type="button"
                  class="flex h-10 w-full items-center gap-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-left text-sm focus-ring"
                  :aria-expanded="typeDropdownOpen"
                  :disabled="editing !== null && !canSensitiveWrite"
                  @click="typeDropdownOpen = !typeDropdownOpen"
                >
                  <VendorLogo :vendor="selectedTypeMeta.supplier" :size="20" />
                  <span
                    class="min-w-0 flex-1 truncate font-medium text-[var(--text-primary)]"
                  >
                    {{ selectedTypeMeta.label }}
                  </span>
                  <ChevronDown
                    :size="15"
                    class="shrink-0 text-[var(--text-tertiary)]"
                  />
                </button>

                <div
                  v-if="typeDropdownOpen"
                  class="absolute left-0 top-[calc(100%+4px)] z-20 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] shadow-lg"
                >
                  <div class="p-2">
                    <input
                      v-model="typeSearch"
                      type="text"
                      class="h-9 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 text-sm focus-ring outline-hidden placeholder:text-[var(--text-tertiary)]"
                      :placeholder="t('channels.typeFilter') + '...'"
                      autofocus
                      @click.stop
                    />
                  </div>
                  <ul
                    class="subtle-scroll max-h-52 overflow-y-auto pb-2"
                    role="listbox"
                    :aria-label="t('channels.type')"
                  >
                    <li
                      v-for="opt in filteredTypeOptions"
                      :key="opt.value"
                      role="option"
                      :aria-selected="form.type === opt.value"
                      class="mx-2 flex cursor-pointer items-center gap-2 rounded-lg px-2 py-2 text-sm hover:bg-[var(--surface-muted)]"
                      :class="
                        form.type === opt.value
                          ? 'font-semibold text-[var(--accent)]'
                          : 'text-[var(--text-primary)]'
                      "
                      @click="selectType(opt.value)"
                    >
                      <VendorLogo
                        :vendor="adminChannelTypeMeta(opt.value).supplier"
                        :size="20"
                      />
                      {{ opt.label }}
                    </li>
                    <li
                      v-if="filteredTypeOptions.length === 0"
                      class="px-4 py-3 text-sm text-[var(--text-tertiary)]"
                    >
                      {{ t('common.noResults') }}
                    </li>
                  </ul>
                </div>
              </div>
            </div>

            <FormField :label="t('channels.channelName') + ' *'">
              <TextInput
                v-model="form.name"
                name="admin-channel-name"
                :placeholder="t('channels.channelNamePlaceholder')"
                autocomplete="off"
              />
            </FormField>
          </div>

          <FormField :label="t('channels.channelRatio')"
            ><TextInput
              v-model="form.channel_ratio"
              inputmode="decimal"
              name="channel-ratio"
          /></FormField>
          <FormField
            :label="t('channels.maxConcurrency')"
            :hint="t('channels.maxConcurrencyHint')"
            ><input
              v-model.number="form.max_concurrency"
              type="number"
              min="0"
              step="1"
              class="console-input w-full"
              name="channel-max-concurrency"
          /></FormField>
          <!-- Enabled toggle (create only; existing channels toggle status from the table) -->
          <div
            v-if="editing === null"
            class="flex items-center justify-between rounded-xl border border-[var(--border-subtle)] px-4 py-3"
          >
            <div>
              <p class="text-sm font-medium text-[var(--text-primary)]">
                {{ t('channels.enabledLabel') }}
              </p>
              <p class="text-xs text-[var(--text-tertiary)]">
                {{ t('channels.enabledDesc') }}
              </p>
            </div>
            <ConsoleToggle
              v-model="form.enabled"
              :label="t('channels.enabledLabel')"
            />
          </div>
        </div>
      </section>

      <!-- ══ Section: 凭证 ══════════════════════════════════════════════ -->
      <section class="channel-form-section channel-form-section--credentials">
        <div class="channel-form-section-header">
          <div class="channel-form-section-icon">
            <KeyRound :size="16" />
          </div>
          <div>
            <p class="channel-form-section-title">
              {{ t('channels.sectionCredentials') }}
            </p>
            <p class="channel-form-section-desc">
              {{ t('channels.sectionCredentialsDesc') }}
            </p>
          </div>
        </div>

        <div class="channel-form-section-body">
          <ProviderChannelSettings
            v-model:aws-api-type="awsApiType"
            v-model:aws-key-type="awsKeyType"
            v-model:vertex-key-type="vertexKeyType"
            v-model:vertex-project-id="vertexProjectId"
            v-model:vertex-region="vertexRegion"
            :channel-type="form.type"
            :disabled="!canSensitiveWrite"
          />
          <!-- API address -->
          <FormField
            :label="
              form.type === 61
                ? t('channels.apiAddressRequired')
                : t('channels.apiAddress')
            "
            :hint="
              form.type === 61
                ? t('channels.apiAddressRequiredDesc')
                : t('channels.apiAddressDesc')
            "
          >
            <TextInput
              v-model="form.baseUrl"
              name="admin-channel-base-url"
              :disabled="!canSensitiveWrite"
              :placeholder="
                form.type === 61
                  ? t('channels.apiAddressRequiredPlaceholder')
                  : t('channels.apiAddressPlaceholder')
              "
              autocomplete="url"
            />
            <p
              v-if="form.type === 61 && canBindPlugin && boundTaskPlugin"
              class="mt-1.5 flex flex-wrap items-center gap-x-1 text-xs text-[var(--text-tertiary)]"
            >
              <template v-if="boundTaskPlugin.baseUrl">
                <span>{{ t('taskPlugins.pluginDefault') }}:</span>
                <span class="break-all font-mono">{{
                  boundTaskPlugin.baseUrl
                }}</span>
                <button
                  v-if="
                    normalizeBaseUrlValue(form.baseUrl) !==
                    normalizeBaseUrlValue(boundTaskPlugin.baseUrl)
                  "
                  type="button"
                  class="text-[var(--accent-text)] underline focus-ring"
                  @click="form.baseUrl = boundTaskPlugin.baseUrl"
                >
                  {{ t('taskPlugins.useDefault') }}
                </button>
              </template>
              <span v-else>{{ t('taskPlugins.baseUrlRequired') }}</span>
            </p>
            <p
              v-if="
                taskPluginBaseUrlTrust?.plainHttp ||
                taskPluginBaseUrlTrust?.privateHost
              "
              role="status"
              class="mt-2 flex gap-2 rounded-lg border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-3 py-2 text-xs text-[var(--status-warning-text)]"
            >
              <AlertCircle
                :size="14"
                class="mt-0.5 shrink-0"
                aria-hidden="true"
              />
              <span
                >{{
                  taskPluginBaseUrlTrust?.plainHttp
                    ? t('taskPlugins.plainHttp')
                    : ''
                }}{{
                  taskPluginBaseUrlTrust?.plainHttp &&
                  taskPluginBaseUrlTrust?.privateHost
                    ? ' '
                    : ''
                }}{{
                  taskPluginBaseUrlTrust?.privateHost
                    ? t('taskPlugins.privateHost')
                    : ''
                }}</span
              >
            </p>
          </FormField>

          <!-- Add mode selector (batch keys apply to creation only) -->
          <div>
            <div
              v-if="editing === null"
              class="mb-1.5 flex flex-wrap items-center justify-between gap-2"
            >
              <p class="text-sm font-medium text-[var(--text-secondary)]">
                {{ t('channels.addMode') }}
              </p>
              <div
                class="flex gap-1 rounded-lg bg-[var(--surface-hover)] p-0.5"
                role="radiogroup"
                :aria-label="t('channels.addMode')"
              >
                <button
                  v-for="mode in [
                    { value: 'single', label: t('channels.addModeSingle') },
                    { value: 'multi', label: t('channels.addModeMulti') },
                    { value: 'multiKey', label: t('channels.addModeMultiKey') },
                  ]"
                  :key="mode.value"
                  type="button"
                  role="radio"
                  :aria-checked="form.addMode === mode.value"
                  class="rounded-md px-3 py-1 text-xs font-medium transition-colors"
                  :class="
                    form.addMode === mode.value
                      ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
                      : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]'
                  "
                  @click="
                    form.addMode = mode.value as 'single' | 'multi' | 'multiKey'
                  "
                >
                  {{ mode.label }}
                </button>
              </div>
            </div>

            <!-- API key input -->
            <FormField
              :label="
                t('channels.apiKeyLabel') + (editing === null ? ' *' : '')
              "
              :hint="
                form.addMode !== 'single' || existingMultiKey
                  ? t('channels.apiKeyMultiHint')
                  : t('channels.apiKeyHint')
              "
            >
              <template v-if="form.addMode === 'single' && !existingMultiKey">
                <TextInput
                  v-model="form.key"
                  type="password"
                  name="admin-channel-key"
                  :disabled="!canSensitiveWrite"
                  :placeholder="t('channels.apiKeyPlaceholder')"
                  autocomplete="off"
                />
              </template>
              <template v-else>
                <textarea
                  v-model="form.key"
                  name="admin-channel-key-multi"
                  :disabled="!canSensitiveWrite"
                  rows="4"
                  class="channel-form-textarea focus-ring"
                  :placeholder="t('channels.apiKeyPlaceholder')"
                  autocomplete="off"
                />
              </template>
            </FormField>
            <div
              v-if="form.addMode === 'multiKey' || existingMultiKey"
              class="mt-3 grid gap-3 sm:grid-cols-2"
            >
              <FormField :label="t('channels.multiKeyStrategy')"
                ><select
                  v-model="multiKeyMode"
                  :aria-label="t('channels.multiKeyStrategy')"
                  :disabled="!canSensitiveWrite"
                  class="channel-form-textarea"
                >
                  <option value="random">
                    {{ t('channels.multiKeyRandom') }}
                  </option>
                  <option value="polling">
                    {{ t('channels.multiKeyPolling') }}
                  </option>
                </select></FormField
              >
              <FormField
                v-if="existingMultiKey"
                :label="t('channels.keyUpdateMode')"
                ><select
                  v-model="keyMode"
                  :aria-label="t('channels.keyUpdateMode')"
                  :disabled="!canSensitiveWrite"
                  class="channel-form-textarea"
                >
                  <option value="append">{{ t('channels.keyAppend') }}</option>
                  <option value="replace">
                    {{ t('channels.keyReplace') }}
                  </option>
                </select></FormField
              >
            </div>
          </div>
        </div>
      </section>

      <!-- ══ Section: 模型 ══════════════════════════════════════════════ -->
      <section class="channel-form-section channel-form-section--models">
        <div class="channel-form-section-header">
          <div class="channel-form-section-icon">
            <BookOpen :size="16" />
          </div>
          <div>
            <p class="channel-form-section-title">
              {{ t('channels.sectionModels') }}
            </p>
            <p class="channel-form-section-desc">
              {{ t('channels.sectionModelsDesc') }}
            </p>
          </div>
        </div>

        <div class="channel-form-section-body">
          <!-- Models tag input -->
          <div>
            <div class="mb-1.5 flex items-center justify-between">
              <p class="text-sm font-medium text-[var(--text-secondary)]">
                {{ t('channels.modelsLabel') }}
              </p>
              <span class="text-xs text-[var(--text-tertiary)]">
                {{
                  t('channels.modelsSelectedCount', { count: modelTags.length })
                }}
              </span>
            </div>
            <div
              class="min-h-10 flex flex-wrap gap-1.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 py-2 focus-within:border-[var(--border-strong)]"
            >
              <span
                v-for="tag in modelTags"
                :key="tag"
                class="inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-2 py-0.5 text-xs font-medium text-[var(--text-secondary)]"
              >
                {{ tag }}
                <button
                  type="button"
                  :aria-label="`Remove ${tag}`"
                  class="ml-0.5 rounded-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                  @click="removeModelTag(tag)"
                >
                  <X :size="11" />
                </button>
              </span>
              <input
                v-model="modelInput"
                type="text"
                class="min-w-[140px] flex-1 bg-transparent text-sm outline-hidden placeholder:text-[var(--text-tertiary)]"
                :placeholder="
                  modelTags.length ? '' : t('channels.modelsSearchPlaceholder')
                "
                @keydown="onModelInputKeydown"
                @blur="modelInput.trim() && addModelTag(modelInput)"
              />
            </div>

            <div v-if="canUsePrefill" class="mt-3 grid gap-2">
              <label
                class="text-sm text-[var(--text-secondary)]"
                for="channel-prefill-group"
                >{{ t('channels.prefillGroups') }}</label
              >
              <div class="flex flex-wrap items-center gap-2">
                <select
                  id="channel-prefill-group"
                  v-model="prefillId"
                  :disabled="prefillLoading || saving"
                  class="min-w-0 flex-1 channel-form-input"
                >
                  <option value="">
                    {{
                      prefillLoading
                        ? t('common.loading')
                        : t('channels.selectPrefillGroup')
                    }}
                  </option>
                  <option
                    v-for="group in prefillGroups"
                    :key="group.id"
                    :value="String(group.id)"
                  >
                    {{ group.name }}
                  </option>
                </select>
                <ConsoleButton
                  variant="secondary"
                  :disabled="!prefillId || saving || prefillLoading"
                  @click="appendPrefillGroup"
                  ><Plus :size="16" />{{
                    t('channels.appendPrefillGroup')
                  }}</ConsoleButton
                >
                <ConsoleButton
                  v-if="prefillError"
                  variant="ghost"
                  :loading="prefillLoading"
                  @click="loadPrefillGroups"
                  ><RefreshCw :size="16" />{{
                    t('common.retry')
                  }}</ConsoleButton
                >
              </div>
              <p
                v-if="prefillError"
                role="alert"
                class="text-sm text-[var(--status-danger)]"
              >
                {{ prefillError }}
              </p>
              <p
                v-else-if="!prefillLoading && !prefillGroups.length"
                class="text-sm text-[var(--text-secondary)]"
              >
                {{ t('channels.noPrefillGroups') }}
              </p>
            </div>

            <!-- Quick actions -->
            <div class="mt-2">
              <p class="mb-1.5 text-xs text-[var(--text-tertiary)]">
                {{ t('channels.quickActionsDesc') }}
              </p>
              <div class="flex flex-wrap gap-1.5">
                <button
                  type="button"
                  class="channel-form-quick-action"
                  :disabled="fetchingUpstream || !canFetchUpstream"
                  @click="fetchUpstream"
                >
                  {{
                    fetchingUpstream
                      ? t('common.loading')
                      : t('channels.quickFetchUpstream')
                  }}
                </button>
                <button
                  type="button"
                  class="channel-form-quick-action"
                  :disabled="modelTags.length === 0"
                  @click="copyAllModels"
                >
                  {{ t('channels.quickCopyAll') }}
                </button>
                <button
                  type="button"
                  class="channel-form-quick-action"
                  :disabled="modelTags.length === 0"
                  @click="form.models = ''"
                >
                  {{ t('channels.quickClearAll') }}
                </button>
              </div>
            </div>
          </div>

          <!-- Model mapping -->
          <div>
            <div class="mb-2 flex items-center justify-between">
              <div>
                <p class="text-sm font-medium text-[var(--text-secondary)]">
                  {{ t('channels.modelMapping') }}
                </p>
                <p class="text-xs text-[var(--text-tertiary)]">
                  {{ t('channels.modelMappingDesc') }}
                </p>
              </div>
              <div
                class="flex gap-1 rounded-lg bg-[var(--surface-hover)] p-0.5"
              >
                <button
                  type="button"
                  class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
                  :class="
                    modelMappingTab === 'visual'
                      ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
                      : 'text-[var(--text-tertiary)]'
                  "
                  @click="onSwitchToVisual"
                >
                  {{ t('channels.modelMappingVisual') }}
                </button>
                <button
                  type="button"
                  class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
                  :class="
                    modelMappingTab === 'json'
                      ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
                      : 'text-[var(--text-tertiary)]'
                  "
                  @click="onSwitchToJson"
                >
                  {{ t('channels.modelMappingJson') }}
                </button>
              </div>
            </div>

            <!-- Visual mapping editor -->
            <template v-if="modelMappingTab === 'visual'">
              <div
                v-if="mappingRows.length === 0"
                class="rounded-xl border border-dashed border-[var(--border-subtle)] px-4 py-5 text-center text-sm text-[var(--text-tertiary)]"
              >
                {{ t('channels.modelMappingEmpty') }}
              </div>
              <div v-else class="sibling-space-y-2">
                <div
                  v-for="(row, index) in mappingRows"
                  :key="index"
                  class="flex items-center gap-2"
                >
                  <input
                    v-model="row.from"
                    type="text"
                    class="channel-form-input flex-1"
                    placeholder="gpt-4"
                    @change="syncJsonFromRows"
                  />
                  <span class="shrink-0 text-xs text-[var(--text-tertiary)]"
                    >→</span
                  >
                  <input
                    v-model="row.to"
                    type="text"
                    class="channel-form-input flex-1"
                    placeholder="gpt-4-turbo"
                    @change="syncJsonFromRows"
                  />
                  <button
                    type="button"
                    class="shrink-0 rounded-lg p-1.5 text-[var(--text-tertiary)] hover:bg-[var(--surface-muted)] hover:text-[var(--status-danger)]"
                    @click="removeMappingRow(index)"
                  >
                    <X :size="13" />
                  </button>
                </div>
              </div>
              <button
                type="button"
                class="mt-2 w-full rounded-xl border border-dashed border-[var(--border-subtle)] py-2 text-sm text-[var(--text-tertiary)] hover:border-[var(--border-strong)] hover:text-[var(--text-secondary)] transition-colors"
                @click="addMappingRow"
              >
                {{ t('channels.addMapping') }}
              </button>
            </template>

            <!-- JSON mapping editor -->
            <template v-else>
              <textarea
                v-model="modelMappingJson"
                rows="5"
                class="channel-form-textarea focus-ring font-mono text-xs"
                placeholder='{"gpt-4": "gpt-4-turbo"}'
                @blur="syncRowsFromJson"
              />
            </template>
          </div>
        </div>
      </section>

      <!-- ══ Section: 高级设置 (折叠) ═══════════════════════════════════ -->
      <section class="channel-form-section channel-form-section--advanced">
        <button
          type="button"
          class="channel-form-section-header w-full text-left"
          :aria-expanded="advancedOpen"
          @click="advancedOpen = !advancedOpen"
        >
          <div class="channel-form-section-icon">
            <Settings :size="16" />
          </div>
          <div class="flex-1">
            <p class="channel-form-section-title">
              {{ t('channels.sectionAdvanced') }}
            </p>
            <p class="channel-form-section-desc">
              {{ t('channels.sectionAdvancedDesc') }}
            </p>
          </div>
          <ChevronDown
            v-if="!advancedOpen"
            :size="16"
            class="shrink-0 text-[var(--text-tertiary)]"
          />
          <ChevronUp
            v-else
            :size="16"
            class="shrink-0 text-[var(--text-tertiary)]"
          />
        </button>

        <div
          v-show="advancedOpen"
          class="channel-form-section-body sibling-space-y-4"
        >
          <FormField
            v-for="field in visibleFieldSettings"
            :key="field.key"
            :label="t(`channels.fieldSettingsLabels.${field.key}`)"
            :hint="t(`channels.fieldSettingsHints.${field.key}`)"
          >
            <ConsoleToggle
              :model-value="fieldSettings[field.key] === true"
              :label="t(`channels.fieldSettingsLabels.${field.key}`)"
              :disabled="saving || !canSensitiveWrite"
              @update:model-value="fieldSettings[field.key] = $event"
            />
          </FormField>
          <FormField
            :label="t('channels.passThroughBody')"
            :hint="t('channels.passThroughBodyHint')"
            ><ConsoleToggle
              v-model="passThroughBody"
              :label="t('channels.passThroughBody')"
              :disabled="saving || !canSensitiveWrite"
          /></FormField>
          <FormField
            :label="t('channels.tlsInsecureSkipVerify')"
            :hint="t('channels.tlsInsecureSkipVerifyHint')"
            ><ConsoleToggle
              v-model="tlsInsecureSkipVerify"
              :label="t('channels.tlsInsecureSkipVerify')"
              :disabled="saving || !canSensitiveWrite"
          /></FormField>
          <FormField
            :label="t('channels.parameterOverrides')"
            :hint="t('channels.jsonOverridesHint')"
          >
            <textarea
              v-model="paramOverride"
              :disabled="saving || !canSensitiveWrite"
              :aria-label="t('channels.parameterOverrides')"
              class="channel-form-textarea font-mono"
              rows="4"
            />
          </FormField>
          <FormField
            :label="t('channels.headerOverrides')"
            :hint="t('channels.jsonOverridesHint')"
          >
            <textarea
              v-model="headerOverride"
              :disabled="saving || !canSensitiveWrite"
              :aria-label="t('channels.headerOverrides')"
              class="channel-form-textarea font-mono"
              rows="4"
            />
          </FormField>
          <p
            v-if="!validOverrides"
            role="alert"
            class="text-sm text-[var(--status-danger)]"
          >
            {{ t('channels.invalidOverrides') }}
          </p>
          <FormField
            v-if="supportsResponsesWebSocket"
            :label="t('channels.responsesWebSocket')"
            :hint="t('channels.responsesWebSocketHint')"
          >
            <ConsoleToggle
              v-model="responsesWebSocket"
              :label="t('channels.responsesWebSocket')"
              :disabled="saving || !canSensitiveWrite"
            />
          </FormField>
          <FormField
            v-if="isAdvancedChannel"
            :label="t('channels.advancedRoutes')"
            :hint="
              t(
                form.type === 58
                  ? 'channels.advancedRoutesHint'
                  : 'channels.advancedPresetHint'
              )
            "
          >
            <AdvancedChannelEditor
              v-model="advancedConfig"
              :disabled="saving || !canSensitiveWrite || form.type !== 58"
            />
          </FormField>
          <div class="grid gap-4 sm:grid-cols-2">
            <FormField :label="t('channels.priority')">
              <input
                v-model.number="form.priority"
                type="number"
                :min="Number.MIN_SAFE_INTEGER"
                :max="Number.MAX_SAFE_INTEGER"
                step="1"
                name="admin-channel-priority"
                :aria-label="t('channels.priority')"
                class="channel-form-number focus-ring"
              />
            </FormField>
            <FormField :label="t('channels.weight')">
              <input
                v-model.number="form.weight"
                type="number"
                min="0"
                max="1000000"
                step="1"
                name="admin-channel-weight"
                :aria-label="t('channels.weight')"
                class="channel-form-number focus-ring"
              />
            </FormField>
          </div>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          :disabled="saving"
          @click="close"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          size="lg"
          :loading="saving"
          :disabled="!valid"
          @click="submit"
        >
          {{ t('channels.saveChannel') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <FetchedModelsDialog
    :open="open && modelPickerOpen"
    :models="fetchedModels"
    @close="modelPickerOpen = false"
    @apply="applyFetchedModels"
  />
</template>

<style scoped>
/* ── Section layout ──────────────────────────────────────────────────────── */
/* Each functional block is a soft card so day/night themes both separate
   sections by surface tint instead of a bare divider line. */
.channel-form-section {
  padding: 1rem 1.125rem 1.125rem;
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  background: var(--surface-muted);
}
.channel-form-section-header {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
}
.channel-form-section-body {
  margin-top: 1rem;
}
.channel-form-section-body > * + * {
  margin-top: 1rem;
}
.channel-form-section-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  border-radius: 0.5rem;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  flex-shrink: 0;
  margin-top: 0.1rem;
}
/* Per-section accent tints reuse semantic status tokens so both the Desert
   Ledger (day) and One Night (dark) palettes stay coherent automatically. */
.channel-form-section--basic .channel-form-section-icon {
  background: var(--accent-soft);
  border-color: transparent;
  color: var(--accent-text);
}
.channel-form-section--credentials .channel-form-section-icon {
  background: var(--status-warning-soft);
  border-color: transparent;
  color: var(--status-warning-text);
}
.channel-form-section--models .channel-form-section-icon {
  background: var(--status-success-soft);
  border-color: transparent;
  color: var(--status-success-text);
}
.channel-form-section--advanced .channel-form-section-icon {
  background: var(--status-info-soft);
  border-color: transparent;
  color: var(--status-info-text);
}
.channel-form-section-title {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-primary);
}
.channel-form-section-desc {
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
/* ── Inputs ──────────────────────────────────────────────────────────────── */
.channel-form-number {
  width: 100%;
  height: 2.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  padding: 0 1rem;
  color: var(--text-primary);
  font-size: 0.875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  outline: none;
}
.channel-form-number:focus {
  border-color: var(--border-strong);
}
.channel-form-quick-action {
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-solid);
  padding: 0.25rem 0.625rem;
  font-size: 0.75rem;
  color: var(--text-secondary);
  transition:
    background-color 0.15s,
    color 0.15s;
}
.channel-form-quick-action:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--text-primary);
}
.channel-form-quick-action:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.channel-form-input {
  height: 2.25rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.625rem;
  background: var(--surface-solid);
  padding: 0 0.75rem;
  color: var(--text-primary);
  font-size: 0.8125rem;
  outline: none;
}
.channel-form-input:focus {
  border-color: var(--border-strong);
}
.channel-form-textarea {
  width: 100%;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  padding: 0.625rem 0.875rem;
  color: var(--text-primary);
  font-size: 0.875rem;
  resize: vertical;
  outline: none;
  min-height: 5.5rem;
}
.channel-form-textarea:focus {
  border-color: var(--border-strong);
}
</style>
