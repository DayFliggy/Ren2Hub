<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle } from 'lucide-vue-next'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import {
  isMetadataLocalChanged,
  isMetadataSourceChanged,
  metadataApi,
  type SyncPreview,
  type SyncResult,
} from '@/api/adminManagement'
import { modelMetadataSyncMessages } from '@/i18n/modelMetadataSync'

type Conflict = SyncPreview['conflicts'][number]
type ConflictField = Conflict['fields'][number]
// The apply request lists every previewed missing model with the preview's
// catalog version; the backend creates only those and rejects a stale
// preview. So only `update` candidates carry a selection; `create` rows are
// informational.
type Candidate =
  | { kind: 'create'; model_name: string }
  | { kind: 'update'; model_name: string; fields: ConflictField[] }

// The backend ignores `source`; the official repository is the only source.
const SOURCE = 'official'
const STEPS = ['stepSelect', 'stepPreview', 'stepConfirm', 'stepResult']
const FIELD_LABELS: Record<string, string> = {
  description: 'fieldDescription',
  icon: 'fieldIcon',
  tags: 'fieldTags',
  vendor: 'fieldVendor',
  name_rule: 'fieldNameRule',
  status: 'fieldStatus',
}
const NAME_RULES = ['ruleExact', 'rulePrefix', 'ruleContains', 'ruleSuffix']

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMetadataSyncMessages,
})
const language = ref(locale.value === 'zh-CN' ? 'zh-CN' : 'en')
const step = ref(0)
const preview = ref<SyncPreview | null>(null)
const result = ref<SyncResult | null>(null)
// model_name -> overwrite fields; presence of a key means the model is chosen.
const selection = ref<Record<string, string[]>>({})
const search = ref('')
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const syncError = ref('')
// The upstream catalog changed after the preview; the selection was discarded.
const sourceChanged = ref(false)
const localChanged = ref(false)
let controller: AbortController | undefined

const languageOptions = computed(() => [
  { value: 'zh-CN', label: t('localeZh') },
  { value: 'en', label: t('localeEn') },
  { value: 'ja', label: t('localeJa') },
])
const candidates = computed<Candidate[]>(() => [
  ...(preview.value?.missing ?? []).map((model_name) => ({
    kind: 'create' as const,
    model_name,
  })),
  ...(preview.value?.conflicts ?? []).map((conflict) => ({
    kind: 'update' as const,
    ...conflict,
  })),
])
const filtered = computed(() => {
  const keyword = search.value.trim().toLocaleLowerCase()
  return candidates.value.filter((item) =>
    item.model_name.toLocaleLowerCase().includes(keyword)
  )
})
const filteredUpdates = computed(() =>
  filtered.value.filter((item) => item.kind === 'update')
)
const visible = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const pageUpdates = computed(() =>
  visible.value.filter((item) => item.kind === 'update')
)
const pageChosenCount = computed(
  () => pageUpdates.value.filter(isSelected).length
)
const missingNames = computed(() => preview.value?.missing ?? [])
const chosenUpdates = computed(() =>
  (preview.value?.conflicts ?? []).filter((item) =>
    Object.hasOwn(selection.value, item.model_name)
  )
)
const selectedCount = computed(
  () => missingNames.value.length + chosenUpdates.value.length
)
const hiddenSelectedCount = computed(
  () => selectedCount.value - filtered.value.filter(isSelected).length
)
const changedFieldCount = computed(() =>
  chosenUpdates.value.reduce((total, item) => total + item.fields.length, 0)
)
// Follows the preview's field order so the request is independent of click order.
const overwrite = computed(() =>
  chosenUpdates.value
    .map((item) => ({
      model_name: item.model_name,
      local_version: item.local_version,
      fields: item.fields
        .map((field) => field.field)
        .filter((field) => selection.value[item.model_name].includes(field)),
    }))
    .filter((item) => item.fields.length)
)
const selectedFieldCount = computed(() =>
  overwrite.value.reduce((total, item) => total + item.fields.length, 0)
)
const planCount = computed(
  () => missingNames.value.length + overwrite.value.length
)
const busy = computed(() => loading.value || saving.value)
const canAdvance = computed(() =>
  step.value === 0 ? selectedCount.value > 0 : planCount.value > 0
)
const primaryLabel = computed(() => {
  if (step.value === 0) return t('previewSelected')
  if (step.value === 1) return t('reviewConfirm')
  if (saving.value) return t('applying')
  return t('applyChanges', { count: planCount.value })
})

watch([search, pageSize], () => {
  page.value = 1
})
watch(
  [language, () => props.open],
  (_, __, cleanup) => {
    reset()
    cleanup(() => controller?.abort())
  },
  { immediate: true }
)
function reset() {
  controller?.abort()
  loading.value = false
  step.value = 0
  preview.value = null
  result.value = null
  selection.value = {}
  search.value = ''
  page.value = 1
  pageSize.value = 20
  loadError.value = ''
  syncError.value = ''
  sourceChanged.value = false
  localChanged.value = false
}
async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  loadError.value = ''
  sourceChanged.value = false
  preview.value = null
  selection.value = {}
  page.value = 1
  try {
    const data = await metadataApi.preview(
      SOURCE,
      language.value,
      current.signal
    )
    if (!current.signal.aborted) preview.value = data
  } catch (cause) {
    if (!current.signal.aborted) loadError.value = messageOf(cause)
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
async function synchronize() {
  const plan = preview.value
  if (saving.value || !planCount.value || !plan) return
  saving.value = true
  syncError.value = ''
  try {
    result.value = await metadataApi.sync(SOURCE, language.value, {
      source_version: plan.version,
      create_models: plan.missing,
      overwrite: overwrite.value,
    })
    step.value = 3
    emit('saved')
  } catch (cause) {
    if (!isMetadataSourceChanged(cause) && !isMetadataLocalChanged(cause)) {
      syncError.value = messageOf(cause)
      return
    }
    // Nothing was written; the old selection no longer matches the upstream.
    reset()
    if (isMetadataLocalChanged(cause)) localChanged.value = true
    else sourceChanged.value = true
  } finally {
    saving.value = false
  }
}
function next() {
  if (step.value < 2) step.value++
  else void synchronize()
}
function previewAgain() {
  step.value = 0
  syncError.value = ''
  void load()
}
function messageOf(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}
function isSelected(item: Candidate) {
  return (
    item.kind === 'create' || Object.hasOwn(selection.value, item.model_name)
  )
}
function chooseModels(items: Candidate[], checked: boolean) {
  const names = items
    .filter((item) => item.kind === 'update')
    .map((item) => item.model_name)
  const rest = Object.entries(selection.value).filter(
    ([name]) => !names.includes(name)
  )
  // Re-checking a model keeps the fields already picked for it.
  const added = checked
    ? names.map((name): [string, string[]] => [
        name,
        selection.value[name] ?? [],
      ])
    : []
  selection.value = Object.fromEntries([...rest, ...added])
}
function chooseField(name: string, field: string, checked: boolean) {
  const fields = selection.value[name].filter((value) => value !== field)
  selection.value = {
    ...selection.value,
    [name]: checked ? [...fields, field] : fields,
  }
}
function chooseAllFields(checked: boolean) {
  const next = { ...selection.value }
  for (const item of chosenUpdates.value)
    next[item.model_name] = checked
      ? item.fields.map((field) => field.field)
      : []
  selection.value = next
}
function fieldChosen(name: string, field: string) {
  return selection.value[name]?.includes(field) ?? false
}
function checkedOf(event: Event) {
  return (event.target as HTMLInputElement).checked
}
// Unknown fields from a newer backend still render, under their raw name.
function fieldLabel(field: string) {
  const key = FIELD_LABELS[field]
  return key ? t(key) : field
}
function impact(field: string) {
  if (field === 'name_rule') return t('nameRuleImpact')
  if (field === 'status') return t('statusImpact')
  return ''
}
function display(field: string, value: string | number) {
  if (field === 'status') return value === 1 ? t('shown') : t('notShown')
  const rule = NAME_RULES[Number(value)]
  if (field === 'name_rule' && rule) return t(rule)
  return String(value) || '—'
}
</script>
<template>
  <ConsoleModal
    :open="open"
    :title="t('syncTitle')"
    :subtitle="t('syncSubtitle')"
    size="xl"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <ol
      class="mb-5 flex flex-wrap items-center gap-2 border-b border-[var(--border-subtle)] pb-3 text-xs sm:text-sm"
      :aria-label="t('syncSteps')"
    >
      <li
        v-for="(label, index) in STEPS"
        :key="label"
        :aria-current="step === index ? 'step' : undefined"
        class="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 transition-colors"
        :class="
          step === index
            ? 'bg-[var(--accent-soft)] font-medium text-[var(--accent-text)]'
            : step > index
              ? 'text-[var(--text-secondary)]'
              : 'text-[var(--text-tertiary)]'
        "
      >
        <span
          class="flex h-5 w-5 items-center justify-center rounded-full text-[0.6875rem]"
          :class="
            step === index
              ? 'bg-[var(--accent)] text-[var(--accent-contrast)] font-semibold'
              : step > index
                ? 'bg-[var(--accent-soft)] text-[var(--accent-text)]'
                : 'border border-[var(--border-subtle)] bg-[var(--surface-muted)] text-[var(--text-tertiary)]'
          "
        >
          {{ index + 1 }}
        </span>
        <span>{{ t(label) }}</span>
      </li>
    </ol>
    <div v-if="step === 0" class="grid gap-4">
      <div class="flex flex-wrap items-end gap-3">
        <div class="grid gap-1">
          <span class="text-sm text-[var(--text-secondary)]">{{
            t('metadataLanguage')
          }}</span>
          <FilterSelect
            v-model="language"
            :options="languageOptions"
            :label="t('metadataLanguage')"
            :disabled="busy"
            class="w-44"
          />
        </div>
        <ConsoleButton variant="secondary" :disabled="busy" @click="load">
          {{ t('loadPreview') }}
        </ConsoleButton>
      </div>
      <div v-if="loading" class="grid gap-2" role="status">
        <span class="sr-only">{{ t('loading') }}</span>
        <SkeletonBlock v-for="i in 4" :key="i" rounded="lg" class="h-10" />
      </div>
      <!-- Same layout as the sync failure below, with the preview's own title. -->
      <div
        v-else-if="loadError"
        class="flex flex-col items-center justify-center px-6 py-8 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t('previewFailed') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ loadError }}
        </p>
        <ConsoleButton class="mt-5" variant="secondary" @click="load">
          {{ t('retry') }}
        </ConsoleButton>
      </div>
      <div
        v-else-if="sourceChanged || localChanged"
        class="flex flex-col items-center justify-center px-6 py-8 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-warning-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t(localChanged ? 'localChangedTitle' : 'sourceChangedTitle') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ t(localChanged ? 'localChangedHint' : 'sourceChangedHint') }}
        </p>
        <ConsoleButton class="mt-5" variant="secondary" @click="load">
          {{ t('previewAgain') }}
        </ConsoleButton>
      </div>
      <template v-else-if="preview">
        <SearchInput
          v-model="search"
          :placeholder="t('searchModels')"
          :aria-label="t('searchModelsLabel')"
          name="metadata-sync-search"
          class="w-full sm:w-64"
        />
        <div class="flex flex-wrap items-center gap-3">
          <p class="text-xs text-[var(--text-tertiary)]" role="status">
            {{
              t('candidateStats', {
                total: filtered.length,
                create: filtered.length - filteredUpdates.length,
                update: filteredUpdates.length,
              })
            }}
          </p>
          <ConsoleButton
            variant="secondary"
            size="sm"
            :disabled="busy || !filteredUpdates.length"
            @click="chooseModels(filteredUpdates, true)"
          >
            {{ t('selectAllSyncable', { count: filtered.length }) }}
          </ConsoleButton>
          <ConsoleButton
            variant="ghost"
            size="sm"
            :disabled="busy || !chosenUpdates.length"
            @click="selection = {}"
          >
            {{ t('clearSelection') }}
          </ConsoleButton>
        </div>
        <EmptyState
          v-if="!filtered.length"
          :title="candidates.length ? t('noFilterMatch') : t('noSyncable')"
          :hint="candidates.length ? t('adjustSearch') : ''"
        />
        <template v-else>
          <div
            class="overflow-x-auto rounded-lg border border-[var(--border-subtle)]"
          >
            <table class="w-full text-sm">
              <thead
                class="bg-[var(--surface-table-header,var(--surface-muted))] text-left"
              >
                <tr class="text-[var(--text-secondary)]">
                  <th class="w-12 px-3 py-2">
                    <input
                      type="checkbox"
                      class="checkbox-round"
                      :checked="
                        pageUpdates.length > 0 &&
                        pageChosenCount === pageUpdates.length
                      "
                      :indeterminate="
                        pageChosenCount > 0 &&
                        pageChosenCount < pageUpdates.length
                      "
                      :disabled="busy || !pageUpdates.length"
                      :aria-label="t('selectPageSyncable')"
                      @change="chooseModels(pageUpdates, checkedOf($event))"
                    />
                  </th>
                  <th class="px-3 py-2 font-medium">{{ t('model') }}</th>
                  <th class="px-3 py-2 font-medium">
                    {{ t('plannedAction') }}
                  </th>
                </tr>
              </thead>
              <tbody class="sync-rows">
                <tr v-for="item in visible" :key="item.model_name">
                  <td class="px-3 py-2">
                    <input
                      v-if="item.kind === 'create'"
                      type="checkbox"
                      class="checkbox-round"
                      checked
                      disabled
                      :aria-label="t('autoIncluded', { name: item.model_name })"
                    />
                    <input
                      v-else
                      type="checkbox"
                      class="checkbox-round"
                      :checked="isSelected(item)"
                      :disabled="busy"
                      :aria-label="t('selectModel', { name: item.model_name })"
                      @change="chooseModels([item], checkedOf($event))"
                    />
                  </td>
                  <td
                    class="break-all px-3 py-2 font-mono text-[var(--text-primary)]"
                  >
                    {{ item.model_name }}
                  </td>
                  <td class="px-3 py-2">
                    <StatusChip>
                      {{
                        item.kind === 'create'
                          ? t('kindCreate')
                          : t('kindUpdate')
                      }}
                    </StatusChip>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <TablePagination
            :page="page"
            :page-size="pageSize"
            :total="filtered.length"
            @update:page="page = $event"
            @update:page-size="pageSize = $event"
          />
        </template>
      </template>
    </div>
    <div v-else-if="step === 1" class="grid gap-5">
      <p v-if="missingNames.length" class="text-sm text-[var(--text-tertiary)]">
        {{ t('newModelsHint') }}
      </p>
      <div v-if="changedFieldCount" class="grid gap-3">
        <p class="text-sm text-[var(--text-tertiary)]">
          {{ t('existingModelsHint') }}
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <ConsoleButton
            variant="secondary"
            size="sm"
            :disabled="busy || selectedFieldCount === changedFieldCount"
            @click="chooseAllFields(true)"
          >
            {{ t('selectAllFields') }}
          </ConsoleButton>
          <ConsoleButton
            variant="ghost"
            size="sm"
            :disabled="busy || !selectedFieldCount"
            @click="chooseAllFields(false)"
          >
            {{ t('clearFieldSelection') }}
          </ConsoleButton>
          <p class="text-xs text-[var(--text-tertiary)]" role="status">
            {{
              t('fieldsSelected', {
                selected: selectedFieldCount,
                total: changedFieldCount,
              })
            }}
          </p>
        </div>
      </div>
      <section v-if="missingNames.length" class="grid gap-2">
        <h3 class="text-sm font-semibold text-[var(--text-primary)]">
          {{ t('kindCreate') }} ·
          {{ t('modelCount', { count: missingNames.length }) }}
        </h3>
        <ul class="flex max-h-40 flex-wrap gap-2 overflow-y-auto">
          <li v-for="name in missingNames" :key="name" class="max-w-full">
            <StatusChip class="max-w-full">
              <span class="break-all font-mono text-[var(--text-primary)]">{{
                name
              }}</span>
            </StatusChip>
          </li>
        </ul>
      </section>
      <section
        v-for="item in chosenUpdates"
        :key="item.model_name"
        class="grid gap-2"
      >
        <h3 class="break-all text-sm font-semibold text-[var(--text-primary)]">
          <span class="font-mono">{{ item.model_name }}</span> ·
          {{ t('kindUpdate') }}
        </h3>
        <div
          class="overflow-x-auto rounded-lg border border-[var(--border-subtle)]"
        >
          <table class="w-full min-w-[40rem] text-sm">
            <thead
              class="bg-[var(--surface-table-header,var(--surface-muted))] text-left"
            >
              <tr class="text-[var(--text-secondary)]">
                <th class="w-16 px-3 py-2 font-medium">{{ t('apply') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('field') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('currentValue') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('changeTo') }}</th>
              </tr>
            </thead>
            <tbody class="sync-rows align-top">
              <tr v-for="field in item.fields" :key="field.field">
                <td class="px-3 py-2">
                  <input
                    type="checkbox"
                    class="checkbox-round"
                    :checked="fieldChosen(item.model_name, field.field)"
                    :disabled="busy"
                    :aria-label="
                      t('applyField', {
                        field: fieldLabel(field.field),
                        name: item.model_name,
                      })
                    "
                    @change="
                      chooseField(
                        item.model_name,
                        field.field,
                        checkedOf($event)
                      )
                    "
                  />
                </td>
                <td class="w-48 px-3 py-2">
                  <span class="text-[var(--text-primary)]">{{
                    fieldLabel(field.field)
                  }}</span>
                  <p
                    v-if="impact(field.field)"
                    class="mt-1 text-xs text-[var(--status-warning-text)]"
                  >
                    {{ impact(field.field) }}
                  </p>
                </td>
                <td class="px-3 py-2">
                  <pre class="sync-value text-[var(--text-tertiary)]">{{
                    display(field.field, field.local)
                  }}</pre>
                </td>
                <td class="px-3 py-2">
                  <pre class="sync-value text-[var(--text-primary)]">{{
                    display(field.field, field.upstream)
                  }}</pre>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
    <div v-else-if="step === 2" class="grid gap-4">
      <p class="text-sm text-[var(--text-primary)]">{{ t('confirmNote') }}</p>
      <ul class="grid gap-1 text-sm text-[var(--text-primary)]">
        <li>{{ t('planCreate', { count: missingNames.length }) }}</li>
        <li>
          {{
            t('planOverwrite', {
              models: overwrite.length,
              fields: selectedFieldCount,
            })
          }}
        </li>
      </ul>
      <div
        v-if="overwrite.length"
        class="overflow-x-auto rounded-lg border border-[var(--border-subtle)]"
      >
        <table class="w-full text-sm">
          <thead class="bg-[var(--surface-muted)] text-left">
            <tr class="text-[var(--text-secondary)]">
              <th class="px-3 py-2 font-medium">{{ t('model') }}</th>
              <th class="px-3 py-2 font-medium">{{ t('fields') }}</th>
            </tr>
          </thead>
          <tbody class="sibling-divide-y sibling-divide-[var(--border-subtle)]">
            <tr v-for="item in overwrite" :key="item.model_name">
              <td
                class="break-all px-3 py-2 font-mono text-[var(--text-primary)]"
              >
                {{ item.model_name }}
              </td>
              <td class="px-3 py-2 text-[var(--text-secondary)]">
                {{ item.fields.map(fieldLabel).join(', ') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="text-sm text-[var(--text-tertiary)]">{{ t('importNote') }}</p>
      <div
        v-if="syncError"
        class="flex flex-col items-center justify-center px-6 py-8 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t('syncFailed') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ syncError }}
        </p>
        <ConsoleButton
          class="mt-5"
          variant="secondary"
          :disabled="busy"
          @click="previewAgain"
        >
          {{ t('previewAgain') }}
        </ConsoleButton>
      </div>
    </div>
    <div v-else-if="result" class="grid gap-4" role="status">
      <h3 class="font-semibold text-[var(--text-primary)]">
        {{ t('syncCompleted') }}
      </h3>
      <p class="text-sm text-[var(--text-primary)]">
        {{
          t('syncSummary', {
            created: result.created_models,
            updated: result.updated_models,
            vendors: result.created_vendors,
          })
        }}
      </p>
      <section v-if="result.skipped_models.length" class="grid gap-2">
        <h4 class="text-sm font-medium text-[var(--text-secondary)]">
          {{ t('skippedModels') }}
        </h4>
        <ul class="flex flex-wrap gap-2">
          <li
            v-for="name in result.skipped_models"
            :key="name"
            class="max-w-full"
          >
            <StatusChip class="max-w-full">
              <span class="break-all font-mono text-[var(--text-primary)]">{{
                name
              }}</span>
            </StatusChip>
          </li>
        </ul>
      </section>
    </div>
    <template #footer>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <div class="mr-auto min-w-0 text-sm text-[var(--text-tertiary)]">
          <p>{{ t('selectedModels', { count: selectedCount }) }}</p>
          <p v-if="step === 0 && hiddenSelectedCount" class="text-xs">
            {{ t('hiddenSelected', { count: hiddenSelectedCount }) }}
          </p>
        </div>
        <ConsoleButton
          variant="ghost"
          :disabled="saving"
          @click="emit('close')"
        >
          {{ t('close') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="step === 1 || step === 2"
          variant="ghost"
          :disabled="busy"
          @click="step--"
        >
          {{ t('back') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="step < 3"
          :loading="saving"
          :disabled="busy || !preview || !canAdvance"
          @click="next"
        >
          {{ primaryLabel }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
<style scoped>
/* Ledger rows: dashed by day, solid by night (tokens --row-divider-style). */
.sync-rows > tr + tr {
  border-top: 1px var(--row-divider-style, dashed) var(--border-subtle);
}
.sync-value {
  max-height: 12rem;
  max-width: 24rem;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: inherit;
  font-size: 0.75rem;
}
</style>
