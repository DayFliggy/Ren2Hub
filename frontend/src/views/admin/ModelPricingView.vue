<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { createReusableTemplate, useElementSize } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import {
  pricingConfigApi,
  type PricingValues,
  type FetchedModelPrice,
  type ModelPricingChange,
  type ModelPricingEntry,
} from '@/api/pricingConfig'
import { pricingDiff } from '@/features/pricing/diff'
import {
  pricingChanges,
  pricingChangeKind,
  pricingMode,
  pricingJSON,
  parsePricingJSON,
  fillPriceDraft,
  clonePricing,
  hasBasePrice,
  initialPricingDraft,
  validatePricing,
  type PricingChangeKind,
} from '@/features/pricing/draft'
import { translateError } from '@/features/pricing/errors'
import {
  changedPreviewRows,
  pricingPreviewRows,
  type PreviewChange,
} from '@/features/pricing/previewRows'
import { useModelPricingConfig } from '@/features/pricing/useModelPricingConfig'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { Copy, Plus } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelPricingEditor from '@/components/admin/ModelPricingEditor.vue'
import PriceDiffCell from '@/components/admin/PriceDiffCell.vue'
import PricingExposeToggle from '@/components/admin/PricingExposeToggle.vue'
import PricingJsonEditor from '@/components/admin/PricingJsonEditor.vue'
import PricingToolsPanel from '@/components/admin/PricingToolsPanel.vue'
import PriceFetchDialog from '@/components/admin/PriceFetchDialog.vue'
import PricingActionBar from '@/components/admin/PricingActionBar.vue'
import {
  pricingBarHostKey,
  type PricingBarState,
} from '@/components/admin/pricingActionBar'

const props = withDefaults(defineProps<{ embedded?: boolean }>(), {
  embedded: false,
})
const { t, te } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const route = useRoute()
// The page width is the only layout breakpoint: the list and editor columns
// need 320px + 440px plus the gap, so a narrower page (a phone, or a tablet
// beside the console sidebar) lists models alone and edits in a drawer.
const STACKED_WIDTH = 820
const root = ref<HTMLElement | null>(null)
const { width: rootWidth } = useElementSize(root)
// A zero width means the page has no layout yet (or is hidden); it keeps the
// inline editor instead of opening a drawer nobody can see.
const compact = computed(
  () => rootWidth.value > 0 && rootWidth.value < STACKED_WIDTH
)
// The editor is defined once and shown in the card or the drawer.
const [DefineEditor, ReuseEditor] = createReusableTemplate()
const settings = useSystemSettings()
const {
  baseline,
  draft,
  enabledModels,
  issues,
  changes,
  loading,
  saving,
  ready,
  reloadRequired,
  load: loadConfig,
  save: saveConfig,
} = useModelPricingConfig(settings.load)
const error = ref(''),
  message = ref('')
const tab = ref('models'),
  mode = ref('visual'),
  search = ref(''),
  filter = ref('')
const selected = ref<string[]>([]),
  editing = ref<string | null>(null),
  revision = ref(0),
  editorDirty = ref(false)
const editor = ref<InstanceType<typeof ModelPricingEditor> | null>(null)
const json = ref(pricingJSON({})),
  fetchOpen = ref(false),
  clearOpen = ref(false),
  reloadOpen = ref(false)
// The changes a save writes, listed for confirmation before they are sent.
const review = ref<ModelPricingChange[] | null>(null)
const toolsDirty = ref(false)
// The ?model= deep link opens its editor after the first load only; later
// reloads keep the editor the user chose.
let deepLinkApplied = false
const toolsRevision = ref(0)
const page = ref(1),
  pageSize = ref(20)
const activeName = computed(() =>
  editing.value === '__new__' ? '' : (editing.value ?? '')
)
const snapshotEntry = computed(() =>
  baseline.value?.entries.find((entry) => entry.model_name === activeName.value)
)
const fetchedEntry = ref<ModelPricingEntry>(),
  entryLoading = ref(false),
  entryError = ref('')
const activeEntry = computed(() => snapshotEntry.value ?? fetchedEntry.value)
let entryController: AbortController | undefined
// Unconfigured plugin models are absent from the all-model snapshot. Fetch
// their schema separately without replacing the workspace's saved baseline.
async function loadActiveEntry() {
  entryController?.abort()
  fetchedEntry.value = undefined
  entryError.value = ''
  entryLoading.value = false
  if (!activeName.value || !baseline.value || snapshotEntry.value) return
  const request = new AbortController()
  entryController = request
  entryLoading.value = true
  try {
    const entry = await pricingConfigApi.loadEntry(
      activeName.value,
      request.signal
    )
    if (!request.signal.aborted) fetchedEntry.value = entry
  } catch (cause) {
    if (!request.signal.aborted) entryError.value = translateError(cause, t, te)
  } finally {
    if (entryController === request) entryLoading.value = false
  }
}
watch([activeName, baseline], loadActiveEntry)
const activePricing = computed(() => draft.value[activeName.value] ?? {})
const activeIssues = computed(() =>
  issues.value.filter((issue) => issue.model_name === activeName.value)
)
const issueModelCount = computed(
  () => new Set(issues.value.map((issue) => issue.model_name)).size
)
// After a save whose refresh failed, nothing is pending until a reload.
const changeCount = computed(() =>
  reloadRequired.value ? 0 : changes.value.length
)
// The saved prices every draft change is compared with. After a save whose
// refresh failed, the server already holds the draft.
const saved = computed(() =>
  baseline.value && !reloadRequired.value
    ? initialPricingDraft(baseline.value)
    : draft.value
)
// A pending deletion still lists its saved price, struck through.
function shownPricing(name: string): PricingValues {
  const pending = draft.value[name]
  return pending && Object.keys(pending).length
    ? pending
    : (saved.value[name] ?? {})
}
const siteCurrency = computed(() =>
  String(settings.rawValue('general_setting.custom_currency_symbol', 'USD'))
)
const exchangeRate = computed(() =>
  String(settings.rawValue('general_setting.custom_currency_exchange_rate', 1))
)
const dirty = computed(() => {
  if (toolsDirty.value) return true
  // The saved draft only differs from the stale baseline until it reloads.
  if (reloadRequired.value) return false
  return (
    editorDirty.value ||
    changes.value.length > 0 ||
    (mode.value === 'json' &&
      JSON.stringify(json.value) !== JSON.stringify(pricingJSON(draft.value)))
  )
})
const changeKind = (name: string) =>
  pricingChangeKind(saved.value[name] ?? {}, draft.value[name] ?? {})
const rows = computed(() => {
  const names =
    tab.value === 'unset'
      ? enabledModels.value.filter(
          // Built-in audio ratios alone do not price a model. A pending
          // deletion is listed under the priced models, with its undo.
          (name) =>
            !hasBasePrice(draft.value[name] ?? {}) &&
            changeKind(name) !== 'deleted'
        )
      : // A deleted saved price stays listed, with undo, until saving.
        [...new Set([...Object.keys(saved.value), ...Object.keys(draft.value)])]
  return names
    .filter(
      (name) =>
        name.toLowerCase().includes(search.value.toLowerCase()) &&
        (!filter.value || pricingMode(shownPricing(name)) === filter.value)
    )
    .sort()
})
const pageRows = computed(() =>
  rows.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const tabs = computed(() => [
  { key: 'models', label: t(props.embedded ? 'priced' : 'models') },
  { key: 'unset', label: t('unset') },
  { key: 'tools', label: t('tools') },
])
const modeOptions = computed(() => [
  { value: '', label: t('filterAll') },
  { value: 'tiered_expr', label: t('expression') },
  { value: 'per-token', label: t('token') },
  { value: 'per-request', label: t('request') },
])
// One header bar state. Standalone renders it here; embedded hands the same
// live state to the hosting page, which renders it in its own header.
const barState = computed<PricingBarState>(() => ({
  dirty: dirty.value,
  loading: loading.value,
  saving: saving.value,
  settingsSaving: settings.saving.value,
  ready: ready.value,
  tab: tab.value,
  changeCount: changeCount.value,
  reload,
  beginFetch,
  save,
}))
const registerBar = inject(pricingBarHostKey, null)
if (props.embedded) {
  if (!registerBar)
    throw new Error('Embedded model pricing requires a pricing bar host')
  registerBar(barState)
}

// Name, mode and price summary share one cell so the list stays readable in
// the narrow list column and on phones without hidden horizontal scrolling.
const columns = computed<TableColumn[]>(() => [
  { key: 'name', label: t('name') },
  { key: 'actions', label: t('actions'), width: '6rem', align: 'right' },
])

const tableRows = computed(() =>
  pageRows.value.map((name) => {
    const before = saved.value[name] ?? {},
      after = draft.value[name] ?? {},
      pricing = shownPricing(name)
    const change = changeKind(name)
    return {
      name,
      change,
      saved: Object.keys(before).length > 0,
      priced: Object.keys(pricing).length > 0,
      mode: pricingMode(pricing),
      summary: summary(pricing),
      changes:
        change === 'modified'
          ? changedPreviewRows(before, after).map(changeLine)
          : [],
    }
  })
)
const reviewRows = computed(() =>
  (review.value ?? []).map((item) => {
    const before = saved.value[item.model_name] ?? {}
    return {
      name: item.model_name,
      change: pricingChangeKind(before, item.pricing),
      fields: pricingDiff(before, item.pricing),
    }
  })
)
const CHANGE_CHIPS: Record<
  PricingChangeKind,
  { label: string; tone: 'warning' | 'danger' }
> = {
  // The warning tone matches the header's unsaved marker.
  added: { label: 'diffadded', tone: 'warning' },
  modified: { label: 'diffchanged', tone: 'warning' },
  // Ren2Hub has no built-in price to fall back to: the model is unpriced.
  deleted: { label: 'pendingDelete', tone: 'danger' },
}

function modeTone(
  mode: string,
  priced: boolean
): 'accent' | 'neutral' | 'warning' {
  if (!priced) return 'neutral'
  if (mode === 'tiered_expr') return 'accent'
  if (mode === 'per-request') return 'warning'
  return 'neutral'
}

function modeLabel(mode: string, priced: boolean): string {
  if (!priced) return t('unpriced')
  if (mode === 'tiered_expr') return t('expression')
  if (mode === 'per-request') return t('perCall')
  return t('perToken')
}

watch([search, filter, tab, pageSize], () => {
  page.value = 1
  selected.value = []
})
// The list shows the first preview row: the expression, the fixed price or
// the input price, in the same USD units as the editor preview.
function summary(value: PricingValues) {
  const first = pricingPreviewRows(value)[0]
  if (!first) return t('unpriced')
  return first.unit === 'none' ? first.value : `${first.value} ${t(first.unit)}`
}
// One changed price line of a modified row: label old → new, the unit after
// the last price that is set.
function changeLine(change: PreviewChange): { key: string; text: string } {
  const label = change.translateLabel ? t(change.label) : change.label
  const unit = change.unit === 'none' ? '' : ` ${t(change.unit)}`
  const before = change.before
    ? change.before + (change.after ? '' : unit)
    : t('diffUnset')
  const after = change.after ? change.after + unit : t('diffUnset')
  return { key: change.key, text: `${label} ${before} → ${after}` }
}
// Editor, JSON and revision state follow every newly loaded baseline.
function resetDrafts() {
  json.value = pricingJSON(draft.value)
  editorDirty.value = false
  revision.value++
}
async function load(forceSettings = false) {
  error.value = ''
  try {
    if (!(await loadConfig(forceSettings))) return
    resetDrafts()
    toolsDirty.value = false
    toolsRevision.value++
    if (!deepLinkApplied && typeof route.query.model === 'string')
      editing.value = route.query.model
    deepLinkApplied = true
  } catch (cause) {
    error.value = translateError(cause, t, te)
  }
}
function updateDraft(name: string, pricing: PricingValues) {
  draft.value = { ...draft.value, [name]: clonePricing(pricing) }
  if (editing.value === '__new__') {
    // The draft is committed, so the editor shown under its name is clean.
    editorDirty.value = false
    editing.value = name
  }
}
function commitOpen() {
  return !editor.value || !editorDirty.value || Boolean(editor.value.commit())
}
function open(name: string) {
  if (!commitOpen()) return
  error.value = ''
  if (mode.value === 'json') {
    try {
      draft.value = parsePricingJSON(json.value)
    } catch (cause) {
      error.value = translateError(cause, t, te)
      return
    }
    mode.value = 'visual'
  }
  if (tab.value === 'tools') tab.value = 'models'
  editing.value = name
  editorDirty.value = false
  revision.value++
}
function closeEditor() {
  if (commitOpen()) editing.value = null
}
// Moving the editor between the card and the drawer mounts it again from the
// page draft, so it moves only once its edit commits. An edit that does not
// commit stays where it is, showing its error. A fixed edit stays too, so
// the editor never remounts while the user types; the next resize, save,
// model switch or close moves it.
const drawer = ref(compact.value)
function placeEditor() {
  if (drawer.value === compact.value || !commitOpen()) return
  drawer.value = compact.value
}
// Runs after the render, once the editor's dirty flag has settled: an input
// fixed from invalid reports the editor clean until its expression is
// written back in the same flush, and that moment must not move the editor.
watch([rootWidth, editing, editorDirty], placeEditor, { flush: 'post' })
function switchMode() {
  error.value = ''
  try {
    if (mode.value === 'visual') {
      if (!commitOpen()) return
      json.value = pricingJSON(draft.value)
      mode.value = 'json'
    } else {
      draft.value = parsePricingJSON(json.value)
      mode.value = 'visual'
      revision.value++
    }
  } catch (cause) {
    error.value = translateError(cause, t, te)
  }
}
function save() {
  if (!baseline.value || !ready.value || loading.value || saving.value) return
  error.value = ''
  message.value = ''
  if (!commitOpen()) return
  let pending
  try {
    if (mode.value === 'json') draft.value = parsePricingJSON(json.value)
    pending = pricingChanges(baseline.value, draft.value)
    for (const change of pending) validatePricing(change.pricing)
  } catch (cause) {
    error.value = translateError(cause, t, te)
    return
  }
  if (!pending.length) {
    // A commit can normalize an editor draft back to the loaded baseline.
    // There is nothing to persist or report in that case.
    editorDirty.value = false
    json.value = pricingJSON(draft.value)
    return
  }
  review.value = pending
}
// Sends exactly the changes the administrator confirmed.
async function confirmSave() {
  const pending = review.value
  if (!pending || saving.value) return
  const result = await saveConfig(pending)
  review.value = null
  if (result.status === 'aborted') return
  if (result.status === 'saved') {
    resetDrafts()
    message.value = t('saved')
    return
  }
  const reason = translateError(result.cause, t, te)
  if (result.status === 'rejected') {
    error.value = `${t(result.conflict ? 'conflict' : 'saveFailed')} ${reason}`
    return
  }
  // The saved draft only differs from the stale baseline until it reloads.
  editorDirty.value = false
  error.value = `${t('savedReloadFailed')} ${reason}`
}
// Deleting a saved price leaves a pending deletion; an unsaved model is gone.
function remove(name: string) {
  const next = { ...draft.value }
  delete next[name]
  draft.value = next
  selected.value = selected.value.filter((item) => item !== name)
  if (editing.value === name) {
    editing.value = null
    editorDirty.value = false
  }
}
// Puts a model back to its saved price; a model never saved leaves the draft.
function undo(name: string) {
  const price = saved.value[name]
  if (!price) return remove(name)
  draft.value = { ...draft.value, [name]: clonePricing(price) }
  if (editing.value !== name) return
  // An open editor shows the restored price.
  editorDirty.value = false
  revision.value++
}
function clear() {
  draft.value = {}
  editing.value = null
  json.value = pricingJSON({})
  editorDirty.value = false
  clearOpen.value = false
  message.value = t('allCleared')
}
function copy() {
  if (!commitOpen() || !editing.value || !draft.value[editing.value]) return
  for (const name of selected.value)
    draft.value[name] = clonePricing(draft.value[editing.value]!)
  revision.value++
}
function beginFetch() {
  if (!commitOpen()) return
  if (mode.value === 'json') {
    try {
      draft.value = parsePricingJSON(json.value)
    } catch (cause) {
      error.value = translateError(cause, t, te)
      return
    }
  }
  fetchOpen.value = true
}
function fill(rows: FetchedModelPrice[]) {
  try {
    draft.value = fillPriceDraft(draft.value, rows)
    json.value = pricingJSON(draft.value)
    revision.value++
    editorDirty.value = false
    fetchOpen.value = false
    message.value = t('imported', { count: rows.length })
  } catch (cause) {
    error.value = translateError(cause, t, te)
  }
}
function toolsSaved() {
  error.value = ''
  toolsDirty.value = false
  message.value = t('toolsSaved')
}
function reload() {
  if (saving.value || settings.saving.value) return
  if (dirty.value) reloadOpen.value = true
  else void load(true)
}
function confirmReload() {
  reloadOpen.value = false
  void load(true)
}
useUnsavedChanges({
  dirty: () => dirty.value,
  message: () => t('unsaved'),
  busy: () => saving.value || settings.saving.value,
})
onMounted(() => {
  void load()
})
onBeforeUnmount(() => {
  entryController?.abort()
  if (props.embedded) registerBar?.(null)
})
</script>
<template>
  <section
    ref="root"
    class="model-pricing-page"
    :class="{ 'is-embedded': embedded, 'is-compact': compact }"
    :aria-label="t('title')"
  >
    <DefineEditor>
      <div
        v-if="entryError"
        role="alert"
        class="p-4 text-sm text-[var(--status-danger-text)]"
      >
        <p>{{ entryError }}</p>
        <ConsoleButton class="mt-3" size="sm" @click="loadActiveEntry">{{
          t('retry')
        }}</ConsoleButton>
      </div>
      <p v-else-if="entryLoading" role="status" class="p-4 text-sm">
        {{ t('loading') }}
      </p>
      <ModelPricingEditor
        v-else
        ref="editor"
        :model-name="activeName"
        :pricing="activePricing"
        :entry="activeEntry"
        :revision="revision"
        :saving="saving"
        :site-currency="siteCurrency"
        :exchange-rate="exchangeRate"
        :issues="activeIssues"
        @update="updateDraft"
        @dirty="editorDirty = $event"
        @save="save"
      />
    </DefineEditor>
    <div v-if="!embedded" class="mb-3 flex justify-end">
      <PricingActionBar :state="barState" />
    </div>

    <fieldset
      class="pricing-controls"
      :disabled="loading || saving || settings.saving.value || !ready"
    >
      <div
        v-if="error"
        role="alert"
        class="mb-3 flex items-center gap-2 rounded-xl border bg-[var(--status-danger-soft)] px-3.5 py-2.5 text-xs text-[var(--status-danger-text)]"
      >
        <span>{{ error }}</span>
      </div>
      <div
        v-if="message"
        role="status"
        class="mb-3 flex items-center gap-2 rounded-xl border bg-[var(--status-success-soft)] px-3.5 py-2.5 text-xs text-[var(--status-success-text)]"
      >
        <span class="text-[var(--text-secondary)]">{{ message }}</span>
      </div>

      <details
        v-if="issues.length"
        open
        class="mb-4 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3.5 text-xs text-[var(--status-warning-text)]"
        data-pricing-issues
      >
        <summary class="cursor-pointer font-medium">
          {{ t('pricingIssues', { count: issueModelCount }, issueModelCount) }}
        </summary>
        <p class="mt-2">{{ t('pricingIssuesHint') }}</p>
        <ul class="mt-2 sibling-space-y-2">
          <li
            v-for="issue in issues"
            :key="`${issue.model_name}:${issue.plugin_key}:${issue.reason}`"
            class="break-words"
          >
            <button
              type="button"
              class="break-all font-mono underline focus-ring"
              @click="open(issue.model_name)"
            >
              {{ issue.model_name }}
            </button>
            — {{ issue.plugin_name || issue.plugin_key }} ({{
              issue.plugin_key
            }}): {{ t(`pricingIssue_${issue.reason}`) }}
          </li>
        </ul>
      </details>

      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <SegmentedToggle
          :model-value="tab"
          :options="
            tabs.map((item) => ({ value: item.key, label: item.label }))
          "
          :label="t('title')"
          size="sm"
          @update:model-value="
            (value) => {
              if (commitOpen()) tab = value
            }
          "
        />
        <div
          v-if="tab !== 'tools'"
          class="flex flex-wrap items-center gap-3 text-xs"
        >
          <PricingExposeToggle />
          <ConsoleButton variant="ghost" size="sm" @click="clearOpen = true">
            {{ t('clear') }}
          </ConsoleButton>
          <ConsoleButton variant="secondary" size="sm" @click="switchMode">
            {{ t(mode === 'visual' ? 'json' : 'visual') }}
          </ConsoleButton>
        </div>
      </div>

      <section
        class="pricing-panel"
        :aria-label="tabs.find((item) => item.key === tab)?.label"
      >
        <PricingToolsPanel
          v-show="tab === 'tools'"
          :revision="toolsRevision"
          @dirty="toolsDirty = $event"
          @saved="toolsSaved"
        />

        <template v-if="tab !== 'tools'">
          <PricingJsonEditor v-if="mode === 'json'" v-model="json" />

          <div v-else class="pricing-workspace">
            <ConsoleCard :padded="false" class="pricing-models overflow-hidden">
              <div
                class="pricing-filter flex flex-wrap items-center gap-3 p-4 border-b border-[var(--border-subtle)]"
              >
                <SearchInput
                  v-model="search"
                  :placeholder="t('search')"
                  :aria-label="t('search')"
                  class="min-w-[12rem] flex-1"
                />
                <FilterSelect
                  v-model="filter"
                  :options="modeOptions"
                  :label="t('mode')"
                  :prefix-label="t('mode')"
                  size="sm"
                />
                <ConsoleButton
                  v-if="tab === 'models'"
                  size="sm"
                  @click="open('__new__')"
                >
                  <Plus :size="13" aria-hidden="true" />{{ t('add') }}
                </ConsoleButton>
              </div>

              <div
                v-if="selected.length"
                class="pricing-selected-strip flex flex-wrap items-center gap-2 border-b border-[var(--border-subtle)] bg-[var(--accent-soft)] px-4 py-2.5 transition-colors"
              >
                <span
                  class="inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--accent-text)] mr-auto"
                >
                  <span class="h-1.5 w-1.5 rounded-full bg-[var(--accent)]" />
                  {{ t('selected', { count: selected.length }) }}
                </span>
                <ConsoleButton
                  variant="secondary"
                  size="sm"
                  :disabled="!editing"
                  @click="copy"
                >
                  <Copy :size="13" aria-hidden="true" />{{ t('copy') }}
                </ConsoleButton>
              </div>

              <DataTable
                :columns="columns"
                :rows="tableRows"
                row-key="name"
                selectable
                checkbox-shape="round"
                adaptive-scroll
                merge-page-selection
                min-table-width="0"
                :selected="selected"
                :selection-keys="pageRows"
                :row-selection-label="
                  (row) => t('selectModel', { name: row.name })
                "
                :loading="loading"
                :empty-title="search ? t('noResults') : t('empty')"
                :empty-hint="t('emptyHint')"
                :row-class="
                  (row) =>
                    row.name === editing
                      ? 'bg-[var(--accent-soft)] font-medium'
                      : undefined
                "
                @update:selected="selected = $event as string[]"
              >
                <template #cell-name="{ row }">
                  <div
                    class="grid min-w-0 gap-1.5"
                    :data-model-change="row.change ?? undefined"
                  >
                    <div class="flex min-w-0 flex-wrap items-center gap-2">
                      <!-- A pending deletion has no draft to edit; it offers undo. -->
                      <span
                        v-if="row.change === 'deleted'"
                        class="pricing-model-name pricing-struck font-mono"
                        >{{ row.name }}</span
                      >
                      <button
                        v-else
                        type="button"
                        class="pricing-model-name font-mono"
                        @click="open(row.name)"
                      >
                        {{ row.name }}
                      </button>
                      <StatusChip
                        v-if="row.change"
                        class="shrink-0"
                        :tone="CHANGE_CHIPS[row.change].tone"
                      >
                        {{ t(CHANGE_CHIPS[row.change].label) }}
                      </StatusChip>
                    </div>
                    <!-- As in NewAPI, only the name and the price are struck
                         through; the mode chip stays readable. -->
                    <div class="flex min-w-0 items-center gap-2">
                      <StatusChip
                        class="shrink-0"
                        :tone="modeTone(row.mode, row.priced)"
                      >
                        {{ modeLabel(row.mode, row.priced) }}
                      </StatusChip>
                      <span
                        class="pricing-summary min-w-0 font-mono"
                        :class="{ 'pricing-struck': row.change === 'deleted' }"
                        :title="row.summary"
                      >
                        {{ row.summary }}
                      </span>
                    </div>
                    <ul v-if="row.changes.length" class="grid gap-0.5">
                      <li
                        v-for="line in row.changes.slice(0, 2)"
                        :key="line.key"
                        class="pricing-summary pricing-change font-mono"
                      >
                        {{ line.text }}
                      </li>
                      <li
                        v-if="row.changes.length > 2"
                        class="pricing-summary pricing-change"
                      >
                        {{
                          t('moreChanges', { count: row.changes.length - 2 })
                        }}
                      </li>
                    </ul>
                    <p
                      v-if="row.change === 'deleted'"
                      class="text-xs text-[var(--status-danger-text)]"
                    >
                      {{ t('deleteResult') }}
                    </p>
                  </div>
                </template>
                <template #cell-actions="{ row }">
                  <!-- Undo returns any change to the saved price; a model never
                       saved leaves the draft. Only a saved price can be
                       deleted, as in NewAPI not from the unpriced list. -->
                  <div class="flex flex-wrap justify-end gap-1">
                    <ConsoleButton
                      v-if="row.change"
                      variant="ghost"
                      size="sm"
                      :aria-label="t('undoFor', { name: row.name })"
                      @click="undo(row.name)"
                    >
                      {{ t('undo') }}
                    </ConsoleButton>
                    <ConsoleButton
                      v-if="
                        row.saved && row.change !== 'deleted' && tab !== 'unset'
                      "
                      variant="ghost"
                      size="sm"
                      :aria-label="t('removeFor', { name: row.name })"
                      @click="remove(row.name)"
                    >
                      {{ t('remove') }}
                    </ConsoleButton>
                  </div>
                </template>
                <template #footer>
                  <TablePagination
                    :page="page"
                    :page-size="pageSize"
                    :total="rows.length"
                    @update:page="page = $event"
                    @update:page-size="pageSize = $event"
                  />
                </template>
              </DataTable>
            </ConsoleCard>

            <div v-if="!drawer" class="pricing-editor-container">
              <ConsoleCard v-if="editing !== null" :padded="false">
                <ReuseEditor />
              </ConsoleCard>
              <ConsoleCard
                v-else
                class="pricing-editor-empty h-full flex flex-col justify-center min-h-[28rem]"
              >
                <EmptyState :title="t('select')" :hint="t('emptyHint')">
                  <template #action>
                    <ConsoleButton
                      variant="secondary"
                      size="sm"
                      @click="open('__new__')"
                    >
                      <Plus :size="13" class="mr-1" aria-hidden="true" />{{
                        t('add')
                      }}
                    </ConsoleButton>
                  </template>
                </EmptyState>
              </ConsoleCard>
            </div>
          </div>
        </template>
      </section>
    </fieldset>

    <!-- The editor's own header names the dialog, so the drawer has none. -->
    <ConsoleModal
      v-if="drawer && editing !== null"
      :open="true"
      :aria-label="t('edit')"
      presentation="drawer"
      size="xl"
      :close-disabled="loading || saving || settings.saving.value"
      @close="closeEditor"
    >
      <fieldset
        class="pricing-controls"
        :disabled="loading || saving || settings.saving.value || !ready"
      >
        <ReuseEditor />
      </fieldset>
    </ConsoleModal>

    <PriceFetchDialog
      v-if="fetchOpen"
      :draft="draft"
      :exchange-rate="Number(exchangeRate)"
      :currency="siteCurrency"
      @close="fetchOpen = false"
      @fill="fill"
    />

    <!-- Mounted on demand so it stacks above an open editor drawer. -->
    <ConfirmDialog
      v-if="review"
      :open="true"
      :title="t('reviewTitle')"
      :message="
        t('reviewHint', { count: reviewRows.length }, reviewRows.length)
      "
      :confirm-text="t('confirmSave')"
      :cancel-text="t('cancel')"
      :loading="saving"
      size="lg"
      @confirm="confirmSave"
      @cancel="review = null"
    >
      <ul class="pricing-review" data-pricing-review>
        <li
          v-for="item in reviewRows"
          :key="item.name"
          :data-model-change="item.change ?? undefined"
        >
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <span class="pricing-review-name font-mono">{{ item.name }}</span>
            <StatusChip
              v-if="item.change"
              class="shrink-0"
              :tone="CHANGE_CHIPS[item.change].tone"
            >
              {{ t(CHANGE_CHIPS[item.change].label) }}
            </StatusChip>
          </div>
          <div>
            <p class="pricing-review-label">{{ t('currentPrice') }}</p>
            <p v-if="item.change === 'added'" class="pricing-review-empty">
              {{ t('unpriced') }}
            </p>
            <PriceDiffCell v-else :fields="item.fields" side="current" />
          </div>
          <div>
            <p class="pricing-review-label">{{ t('afterSave') }}</p>
            <p
              v-if="item.change === 'deleted'"
              class="text-xs font-medium text-[var(--status-danger-text)]"
            >
              {{ t('deleteResult') }}
            </p>
            <PriceDiffCell v-else :fields="item.fields" side="candidate" />
          </div>
        </li>
      </ul>
    </ConfirmDialog>

    <ConfirmDialog
      :open="clearOpen"
      :title="t('clear')"
      :message="t('clearConfirm')"
      :confirm-text="t('clear')"
      @confirm="clear"
      @cancel="clearOpen = false"
    />

    <ConfirmDialog
      :open="reloadOpen"
      :title="t('reload')"
      :message="t('refreshConfirm')"
      :confirm-text="t('reload')"
      @confirm="confirmReload"
      @cancel="reloadOpen = false"
    />
  </section>
</template>

<style scoped>
.pricing-controls {
  min-width: 0;
  max-width: 100%;
  margin: 0;
  border: 0;
  padding: 0;
}
.pricing-panel {
  min-width: 0;
  max-width: 100%;
}
.model-pricing-page {
  min-width: 0;
  max-width: 100%;
}
.pricing-workspace {
  display: grid;
  grid-template-columns: minmax(320px, 0.85fr) minmax(440px, 1.15fr);
  gap: 1.25rem;
  align-items: start;
}
.pricing-models {
  min-width: 0;
}
.pricing-model-name {
  min-height: 1.5rem;
  text-align: start;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--text-primary);
  text-decoration: none;
  border: none;
  background: transparent;
  padding: 0;
}
button.pricing-model-name {
  cursor: pointer;
}
button.pricing-model-name:hover {
  /* --accent on the light surface is 2.4:1; the text variant meets 4.5:1. */
  color: var(--accent-text);
  text-decoration: underline;
}
/* Matches ConsoleButton: 44px touch targets below the sm breakpoint. */
@media (max-width: 639px) {
  button.pricing-model-name {
    min-height: 2.75rem;
  }
}
.pricing-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: var(--text-secondary);
}
.pricing-struck {
  text-decoration: line-through;
  color: var(--text-secondary);
}
/* A changed line wraps: truncation would hide the new value. */
.pricing-change {
  overflow: visible;
  text-overflow: clip;
  white-space: normal;
  overflow-wrap: anywhere;
  color: var(--text-primary);
}
.pricing-review {
  display: grid;
  width: 100%;
  margin-top: 1rem;
  text-align: start;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
}
.pricing-review > li {
  display: grid;
  gap: 0.625rem;
  min-width: 0;
  padding: 0.75rem;
}
/* The wide dialog compares current and after-save prices side by side. */
@media (min-width: 640px) {
  .pricing-review > li {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    column-gap: 1.25rem;
  }
  .pricing-review > li > :first-child {
    grid-column: 1 / -1;
  }
}
.pricing-review > li + li {
  border-top: 1px solid var(--border-subtle);
}
.pricing-review :deep(.price-diff) {
  min-width: 0;
  max-width: none;
}
.pricing-review-name {
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--text-primary);
}
.pricing-review-label {
  margin-bottom: 0.25rem;
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--text-secondary);
}
.pricing-review-empty {
  font-size: 0.75rem;
  font-style: italic;
  color: var(--text-tertiary);
}
.pricing-editor-container {
  min-width: 0;
}
/* As in NewAPI's editor panel, Save stays in view while the form scrolls.
   The page scrolls here instead of an inner panel, so the footer sticks to
   the bottom of the scroll zone. The drawer keeps it in flow. */
.pricing-editor-container :deep(.pricing-editor-footer) {
  position: sticky;
  bottom: 0;
  z-index: 1;
  /* Matches the hosting ConsoleCard's rounded-2xl bottom corners. */
  border-radius: 0 0 1rem 1rem;
}
/* compact is set from the page width; see STACKED_WIDTH. */
.is-compact .pricing-workspace {
  grid-template-columns: minmax(0, 1fr);
}
.is-compact .pricing-summary {
  overflow: visible;
  text-overflow: clip;
  white-space: normal;
  overflow-wrap: anywhere;
}
</style>
