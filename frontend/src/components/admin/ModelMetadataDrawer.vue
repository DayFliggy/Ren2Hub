<script setup lang="ts">
/** Model metadata drawer. Pricing is edited in Model management › Pricing. */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from 'lucide-vue-next'
import {
  metadataApi,
  type ModelInput,
  type ModelMetadata,
  type ModelVendor,
} from '@/api/adminManagement'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import DataTable from '@/components/common/DataTable.vue'
import FilterSelect, {
  type SelectOption,
} from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { adminChannelTypeMeta } from '@/constants/adminChannels'
import { vendorLogoMeta } from '@/constants/console'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import { parseModelEndpoints } from '@/utils/modelEndpoints'
import MetadataEndpointEditor from './MetadataEndpointEditor.vue'
import MetadataIcon from './MetadataIcon.vue'
import MetadataTagInput from './MetadataTagInput.vue'
import ModelCapabilitiesEditor from './ModelCapabilitiesEditor.vue'
import {
  endpointTemplates,
  parseMetadataTags,
  serializeMetadataEndpoints,
} from './modelMetadataForm'

const props = defineProps<{
  /** null creates a model; otherwise the drawer loads this metadata row. */
  modelId: number | null
  initialName?: string
  vendors: ModelVendor[]
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, te } = useI18n({
  useScope: 'local',
  messages: modelMetadataMessages,
})
const RULES = [
  'exactMatch',
  'prefixMatch',
  'containsMatch',
  'suffixMatch',
] as const
const iconSuggestions = Object.keys(vendorLogoMeta)

const form = reactive<ModelInput>({
  model_name: props.initialName ?? '',
  description: '',
  icon: '',
  tags: '',
  vendor_id: 0,
  endpoints: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
})
const tags = ref<string[]>([])
const iconMode = ref<'inherit' | 'custom'>('inherit')
const detail = ref<ModelMetadata | null>(null)
const tab = ref('metadata')
const loading = ref(props.modelId !== null)
const loadFailed = ref(false)
const saving = ref(false)
const error = ref('')
const discardOpen = ref(false)
const template = ref('')
const editorKey = ref(0)
const endpointEditor = ref<InstanceType<typeof MetadataEndpointEditor> | null>(
  null
)
const tagInput = ref<InstanceType<typeof MetadataTagInput> | null>(null)
const baseline = ref('')
const controller = new AbortController()

const vendor = computed(() =>
  props.vendors.find((item) => item.id === form.vendor_id)
)
const vendorModel = computed({
  get: () => String(form.vendor_id),
  set: (val: string) => {
    form.vendor_id = Number(val) || 0
  },
})
const vendorOptions = computed<SelectOption[]>(() => [
  { value: '0', label: t('noVendor') },
  ...props.vendors.map((item) => ({
    value: String(item.id),
    label: item.name,
  })),
])
const tabs = computed(() => [
  { key: 'metadata', label: t('modelMetadata') },
  { key: 'connections', label: t('connections') },
])
const renamed = computed(
  () =>
    detail.value !== null &&
    form.model_name.trim() !== '' &&
    form.model_name.trim() !== detail.value?.model_name
)
const effectiveIcon = computed(() =>
  iconMode.value === 'custom' ? form.icon.trim() : vendor.value?.icon || ''
)
const supportedEndpoints = computed(() => {
  const source = detail.value?.endpoints.trim()
  if (!source) return []
  try {
    const value = parseModelEndpoints(source)
    return Array.isArray(value) ? value : Object.keys(value)
  } catch {
    return []
  }
})
const status = computed({
  get: () => form.status === 1,
  set: (value: boolean) => (form.status = value ? 1 : 0),
})
const syncOfficial = computed({
  get: () => form.sync_official === 1,
  set: (value: boolean) => (form.sync_official = value ? 1 : 0),
})

function snapshot() {
  return JSON.stringify([form, tags.value, iconMode.value])
}
function dirty() {
  return (
    snapshot() !== baseline.value ||
    Boolean(endpointEditor.value?.isDirty()) ||
    Boolean(tagInput.value?.isDirty())
  )
}
useUnsavedChanges({
  dirty: () => !loading.value && !loadFailed.value && dirty(),
  busy: () => saving.value,
  message: () => t('discardMessage'),
})
function message(cause: unknown) {
  const text = cause instanceof Error ? cause.message : String(cause)
  return te(text) ? t(text) : text
}

async function load() {
  if (props.modelId === null) {
    baseline.value = snapshot()
    return
  }
  loading.value = true
  loadFailed.value = false
  error.value = ''
  try {
    const model = await metadataApi.model(props.modelId, controller.signal)
    if (controller.signal.aborted) return
    detail.value = model
    Object.assign(form, {
      id: model.id,
      model_name: model.model_name,
      description: model.description,
      icon: model.icon,
      tags: model.tags,
      vendor_id: model.vendor_id,
      endpoints: model.endpoints,
      status: model.status,
      sync_official: model.sync_official,
      name_rule: model.name_rule,
      capabilities: model.capabilities ?? undefined,
    })
    iconMode.value = model.icon ? 'custom' : 'inherit'
    tags.value = parseMetadataTags(model.tags)
    baseline.value = snapshot()
  } catch (cause) {
    if (controller.signal.aborted) return
    error.value = `${t('loadModelFailed')}: ${message(cause)}`
    loadFailed.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

function setIconMode(value: string) {
  iconMode.value = value as 'inherit' | 'custom'
  // Inheriting means no model icon of its own.
  if (value === 'inherit') form.icon = ''
}

/** Upstream template loading replaces the whole endpoint field. */
function loadTemplate() {
  const name = template.value
  template.value = ''
  const endpoint = endpointTemplates[name]
  if (!endpoint) return
  form.endpoints = serializeMetadataEndpoints([{ type: name, ...endpoint }])
  editorKey.value++
}

async function save() {
  if (saving.value || loading.value || loadFailed.value) return
  tagInput.value?.commit()
  error.value = ''
  saving.value = true
  try {
    const name = form.model_name.trim()
    if (!name) throw new Error('nameRequired')
    const cleanTags = tags.value.map((tag) => tag.trim()).filter(Boolean)
    if (new Set(cleanTags).size !== cleanTags.length)
      throw new Error('duplicateTags')
    const endpoints = endpointEditor.value?.validate() ?? form.endpoints
    await metadataApi.saveModel({
      ...form,
      model_name: name,
      tags: cleanTags.join(','),
      icon: iconMode.value === 'custom' ? form.icon.trim() : '',
      endpoints,
    })
    emit('saved')
  } catch (cause) {
    error.value = message(cause)
  } finally {
    saving.value = false
  }
}

function requestClose() {
  if (saving.value) return
  if (!loading.value && !loadFailed.value && dirty()) discardOpen.value = true
  else emit('close')
}

onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>
<template>
  <ConsoleModal
    :open="true"
    :title="
      modelId === null
        ? t('createModel')
        : detail?.model_name || t('modelMetadata')
    "
    :subtitle="t('drawerSubtitle')"
    size="xl"
    presentation="drawer"
    :close-disabled="saving"
    @close="requestClose"
  >
    <div v-if="loading" role="status" class="space-y-4">
      <span class="sr-only">{{ t('loading') }}</span>
      <SkeletonBlock v-for="i in 6" :key="i" rounded="lg" class="h-12" />
    </div>
    <div v-else-if="loadFailed" role="alert" class="space-y-3">
      <p class="text-sm text-[var(--status-danger-text)]">{{ error }}</p>
      <ConsoleButton variant="secondary" @click="load"
        ><RefreshCw :size="15" aria-hidden="true" />{{
          t('retry')
        }}</ConsoleButton
      >
    </div>
    <template v-else>
      <ConsoleTabs
        v-if="detail"
        v-model="tab"
        :items="tabs"
        panel-id="model-drawer-panel"
        class="mb-5"
      />
      <div
        id="model-drawer-panel"
        :role="detail ? 'tabpanel' : undefined"
        :aria-labelledby="detail ? `model-drawer-panel-tab-${tab}` : undefined"
      >
        <form
          v-show="tab === 'metadata'"
          id="model-metadata-form"
          @submit.prevent="save"
        >
          <fieldset :disabled="saving" class="min-w-0 space-y-6">
            <section class="drawer-section">
              <h3 class="drawer-section-title">
                {{ t('basicInformation') }}
              </h3>
              <div class="grid items-start gap-4 sm:grid-cols-2">
                <div class="sm:col-span-2">
                  <FormField :label="t('modelName')">
                    <TextInput
                      id="model-metadata-name"
                      v-model="form.model_name"
                      required
                      autocomplete="off"
                      class="font-mono"
                      :placeholder="t('modelNamePlaceholder')"
                      :aria-describedby="
                        renamed
                          ? 'model-metadata-name-hint model-metadata-name-warning'
                          : 'model-metadata-name-hint'
                      "
                    />
                  </FormField>
                  <small
                    id="model-metadata-name-hint"
                    class="mt-1 block text-xs text-[var(--text-tertiary)]"
                    >{{ t('modelNameHint') }}</small
                  >
                  <small
                    v-if="renamed"
                    id="model-metadata-name-warning"
                    role="status"
                    class="mt-1 block text-xs text-[var(--status-warning-text)]"
                    >{{ t('renameWarning') }}</small
                  >
                </div>
                <div class="sm:col-span-2">
                  <FormField :label="t('description')">
                    <textarea
                      v-model="form.description"
                      rows="3"
                      :placeholder="t('descriptionPlaceholder')"
                      class="w-full rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)] p-3 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] outline-none focus:border-[var(--accent)] focus-ring"
                    />
                  </FormField>
                </div>
                <!-- A group of controls, not one labelled input: a wrapping
                     <label> would forward caption clicks to the first control
                     and switch the icon back to inherit. -->
                <fieldset
                  class="min-w-0 sm:col-span-2"
                  :aria-describedby="
                    iconMode === 'custom'
                      ? 'model-metadata-icon-hint'
                      : undefined
                  "
                >
                  <legend
                    class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
                  >
                    {{ t('icon') }}
                  </legend>
                  <div class="space-y-3">
                    <SegmentedToggle
                      :model-value="iconMode"
                      :options="[
                        { value: 'inherit', label: t('inheritIcon') },
                        { value: 'custom', label: t('customIcon') },
                      ]"
                      :label="t('icon')"
                      size="sm"
                      @update:model-value="setIconMode"
                    />
                    <div v-if="iconMode === 'custom'" class="flex gap-2">
                      <TextInput
                        v-model="form.icon"
                        list="model-icon-suggestions"
                        class="min-w-0 flex-1"
                        :aria-label="t('icon')"
                        :placeholder="t('iconPlaceholder')"
                      />
                      <ConsoleButton
                        variant="ghost"
                        size="sm"
                        :disabled="!form.icon"
                        @click="form.icon = ''"
                      >
                        {{ t('clear') }}
                      </ConsoleButton>
                      <datalist id="model-icon-suggestions">
                        <option
                          v-for="name in iconSuggestions"
                          :key="name"
                          :value="name"
                        />
                      </datalist>
                    </div>
                    <div
                      class="flex items-center gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 py-2.5"
                    >
                      <MetadataIcon
                        :icon="effectiveIcon"
                        :name="vendor?.name || form.model_name || '?'"
                        :size="32"
                      />
                      <div class="min-w-0 text-xs">
                        <p class="text-[var(--text-tertiary)]">
                          {{ t('effectiveIcon') }}
                        </p>
                        <p
                          class="truncate font-mono text-[var(--text-primary)]"
                        >
                          {{ effectiveIcon || t('defaultIcon') }}
                        </p>
                        <p
                          v-if="iconMode === 'inherit'"
                          class="text-[var(--text-tertiary)]"
                        >
                          {{
                            vendor
                              ? t('inheritedFrom', { vendor: vendor.name })
                              : t('noVendor')
                          }}
                        </p>
                      </div>
                    </div>
                  </div>
                  <p
                    v-if="iconMode === 'custom'"
                    id="model-metadata-icon-hint"
                    class="mt-1.5 block text-xs text-[var(--text-tertiary)]"
                  >
                    {{ t('iconHint') }}
                  </p>
                </fieldset>
                <div>
                  <span
                    class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
                    >{{ t('vendor') }}</span
                  >
                  <FilterSelect
                    v-model="vendorModel"
                    :options="vendorOptions"
                    :label="t('vendor')"
                    class="w-full"
                  />
                </div>
                <!-- Same reason as the icon group: a <label> caption click
                     would activate the first tag's delete button. -->
                <fieldset
                  class="min-w-0"
                  aria-describedby="model-metadata-tags-hint"
                >
                  <legend
                    class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
                  >
                    {{ t('tags') }}
                  </legend>
                  <MetadataTagInput
                    ref="tagInput"
                    v-model="tags"
                    :label="t('tags')"
                    :remove-label="t('delete')"
                    :placeholder="t('tagsPlaceholder')"
                    :disabled="saving"
                  />
                  <p
                    id="model-metadata-tags-hint"
                    class="mt-1.5 block text-xs text-[var(--text-tertiary)]"
                  >
                    {{ t('tagsHint') }}
                  </p>
                </fieldset>
              </div>
            </section>
            <section class="drawer-section">
              <h3 class="drawer-section-title">{{ t('matchingRules') }}</h3>
              <fieldset class="space-y-2">
                <legend class="mb-2 text-sm text-[var(--text-secondary)]">
                  {{ t('nameRule') }}
                </legend>
                <div class="grid gap-2 sm:grid-cols-2">
                  <label
                    v-for="(rule, index) in RULES"
                    :key="rule"
                    class="flex min-h-[44px] cursor-pointer items-center gap-2.5 rounded-xl border px-3 py-2.5 text-sm transition-colors"
                    :class="
                      form.name_rule === index
                        ? 'border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--accent-text)]'
                        : 'border-[var(--border-subtle)] text-[var(--text-primary)] hover:bg-[var(--surface-hover)]'
                    "
                  >
                    <input
                      v-model.number="form.name_rule"
                      type="radio"
                      name="model-name-rule"
                      :value="index"
                      class="accent-[var(--accent)] shrink-0"
                    />
                    {{ t(rule) }}
                  </label>
                </div>
                <small class="text-xs text-[var(--text-tertiary)]">{{
                  t('nameRuleHint')
                }}</small>
              </fieldset>
            </section>
            <section class="drawer-section">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h3 class="drawer-section-title">{{ t('endpoints') }}</h3>
                <div class="w-52">
                  <label class="sr-only" for="template-select">{{
                    t('loadTemplate')
                  }}</label>
                  <select
                    id="template-select"
                    v-model="template"
                    class="h-11 w-full rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-2.5 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent)] focus-ring sm:h-8"
                    @change="loadTemplate"
                  >
                    <option value="">{{ t('loadTemplate') }}</option>
                    <option
                      v-for="(_, name) in endpointTemplates"
                      :key="name"
                      :value="name"
                    >
                      {{ name }}
                    </option>
                  </select>
                </div>
              </div>
              <p class="mb-2 mt-1 text-sm text-[var(--text-secondary)]">
                {{ t('endpointConfiguration') }}
              </p>
              <MetadataEndpointEditor
                :key="editorKey"
                ref="endpointEditor"
                v-model="form.endpoints"
                :disabled="saving"
              />
            </section>
            <section class="drawer-section">
              <ModelCapabilitiesEditor v-model="form.capabilities" />
            </section>
            <section class="drawer-section">
              <h3 class="drawer-section-title">{{ t('statusSync') }}</h3>
              <div class="space-y-4">
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <p class="text-sm text-[var(--text-primary)]">
                      {{ t('squareVisibility') }}
                    </p>
                    <p class="text-xs text-[var(--text-tertiary)]">
                      {{ t('squareVisibilityHint') }}
                    </p>
                  </div>
                  <ConsoleToggle
                    v-model="status"
                    :label="t('squareVisibility')"
                  />
                </div>
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <p class="text-sm text-[var(--text-primary)]">
                      {{ t('allowMetadataSync') }}
                    </p>
                    <p class="text-xs text-[var(--text-tertiary)]">
                      {{ t('allowMetadataSyncHint') }}
                    </p>
                  </div>
                  <ConsoleToggle
                    v-model="syncOfficial"
                    :label="t('allowMetadataSync')"
                  />
                </div>
              </div>
            </section>
          </fieldset>
        </form>
        <div v-if="detail" v-show="tab === 'connections'" class="space-y-6">
          <p class="text-sm text-[var(--text-secondary)]">
            {{ t('connectionsHint') }}
          </p>
          <section class="drawer-section">
            <h3 class="drawer-section-title">{{ t('boundChannels') }}</h3>
            <DataTable
              v-if="detail.bound_channels.length"
              :columns="[
                { key: 'name', label: t('channel') },
                { key: 'type', label: t('type') },
              ]"
              :rows="
                detail.bound_channels.map((channel, index) => ({
                  id: index,
                  name: channel.name,
                  type: adminChannelTypeMeta(channel.type).label,
                }))
              "
              row-key="id"
            />
            <p v-else class="text-sm text-[var(--text-tertiary)]">
              {{ t('noChannels') }}
            </p>
          </section>
          <section
            v-for="list in [
              {
                title: 'enableGroups',
                empty: 'noGroups',
                items: detail.enable_groups,
              },
              {
                title: 'supportedEndpoints',
                empty: 'noEndpoints',
                items: supportedEndpoints,
              },
              ...(detail.name_rule === 0
                ? []
                : [
                    {
                      title: 'matchedModels',
                      empty: 'noMatchedModels',
                      items: detail.matched_models,
                    },
                  ]),
            ]"
            :key="list.title"
            class="drawer-section"
          >
            <h3 class="drawer-section-title">{{ t(list.title) }}</h3>
            <div v-if="list.items.length" class="flex flex-wrap gap-1.5">
              <StatusChip
                v-for="(item, index) in list.items"
                :key="index"
                class="font-mono"
                >{{ item }}</StatusChip
              >
            </div>
            <p v-else class="text-sm text-[var(--text-tertiary)]">
              {{ t(list.empty) }}
            </p>
          </section>
        </div>
      </div>
    </template>
    <template #footer>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <p
          v-if="error && !loadFailed"
          role="alert"
          class="mr-auto text-sm text-[var(--status-danger-text)]"
        >
          {{ error }}
        </p>
        <ConsoleButton
          variant="ghost"
          :disabled="saving"
          @click="requestClose"
          >{{ t('close') }}</ConsoleButton
        >
        <ConsoleButton
          type="submit"
          form="model-metadata-form"
          :loading="saving"
          :disabled="loading || loadFailed"
          >{{ saving ? t('saving') : t('saveMetadata') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
  <ConfirmDialog
    :open="discardOpen"
    :title="t('discardTitle')"
    :message="t('discardMessage')"
    :confirm-text="t('discard')"
    @confirm="emit('close')"
    @cancel="discardOpen = false"
  />
</template>
<style scoped>
.drawer-section + .drawer-section {
  border-top: 1px solid var(--border-subtle);
  padding-top: 1.5rem;
}
.drawer-section-title {
  margin-bottom: 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--text-tertiary);
}
</style>
