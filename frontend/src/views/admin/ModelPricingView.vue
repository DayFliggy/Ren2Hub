<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { api } from '@/api/client'
import {
  pricingConfigApi,
  PRICING_KEYS,
  type ModelPricingConfig,
  type PricingValues,
  type FetchedModelPrice,
} from '@/api/pricingConfig'
import {
  initialPricingDraft,
  pricingChanges,
  pricingMode,
  pricingJSON,
  parsePricingJSON,
  fillPriceDraft,
  clonePricing,
  validatePricing,
  type PricingDraft,
} from '@/features/pricing/draft'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { formatDecimalUSD, multiplyDecimal, isDecimal } from '@/utils/decimal'
import { Copy, Plus } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelPricingEditor from '@/components/admin/ModelPricingEditor.vue'
import ToolPricingEditor from '@/components/admin/ToolPricingEditor.vue'
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
const { t: tGlobal } = useI18n()
const route = useRoute(),
  mobile = useMediaQuery('(max-width: 767px)')
const settings = useSystemSettings()
const baseline = ref<ModelPricingConfig | null>(null),
  draft = ref<PricingDraft>({})
const loading = ref(false),
  saving = ref(false),
  error = ref(''),
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
  enabledModels = ref<string[]>([]),
  fetchOpen = ref(false),
  clearOpen = ref(false)
const toolsDirty = ref(false)
const ready = ref(false)
const toolsRevision = ref(0)
const page = ref(1),
  pageSize = ref(20),
  expose = ref(false),
  toolsJSON = ref('{}')
const activeName = computed(() =>
  editing.value === '__new__' ? '' : (editing.value ?? '')
)
const activeEntry = computed(() =>
  baseline.value?.entries.find((entry) => entry.model_name === activeName.value)
)
const activePricing = computed(() => draft.value[activeName.value] ?? {})
const siteCurrency = computed(() =>
  String(settings.rawValue('general_setting.custom_currency_symbol', 'USD'))
)
const exchangeRate = computed(() =>
  String(settings.rawValue('general_setting.custom_currency_exchange_rate', 1))
)
const dirty = computed(
  () =>
    toolsDirty.value ||
    editorDirty.value ||
    (baseline.value
      ? pricingChanges(baseline.value, draft.value).length > 0
      : false) ||
    (mode.value === 'json' &&
      JSON.stringify(json.value) !== JSON.stringify(pricingJSON(draft.value)))
)
const rows = computed(() => {
  const names =
    tab.value === 'unset'
      ? enabledModels.value.filter(
          (name) => !Object.keys(draft.value[name] ?? {}).length
        )
      : Object.keys(draft.value)
  return names
    .filter(
      (name) =>
        name.toLowerCase().includes(search.value.toLowerCase()) &&
        (!filter.value || pricingMode(draft.value[name] ?? {}) === filter.value)
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
  { value: '', label: t('all') },
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
  pageRows.value.map((name) => ({
    name,
    mode: pricingMode(draft.value[name] ?? {}),
    summary: summary(draft.value[name] ?? {}),
  }))
)

function modeTone(
  mode: string,
  name: string
): 'accent' | 'neutral' | 'warning' {
  if (!Object.keys(draft.value[name] ?? {}).length) return 'neutral'
  if (mode === 'tiered_expr') return 'accent'
  if (mode === 'per-request') return 'warning'
  return 'neutral'
}

function modeLabel(mode: string, name: string): string {
  if (!Object.keys(draft.value[name] ?? {}).length) return t('unpriced')
  if (mode === 'tiered_expr') return t('expression')
  if (mode === 'per-request') return t('perCall')
  return t('perToken')
}

watch([search, filter, tab, pageSize], () => {
  page.value = 1
  selected.value = []
})
let controller: AbortController | undefined

function summary(value: PricingValues) {
  if (!Object.keys(value).length) return t('unpriced')
  if (pricingMode(value) === 'tiered_expr')
    return String(value['billing_setting.billing_expr'] ?? '')
  const raw = value.ModelPrice ?? value.ModelRatio
  if (raw === undefined || !isDecimal(String(raw))) return t('unpriced')
  return `${formatDecimalUSD(value.ModelPrice === undefined ? multiplyDecimal(String(raw), '2') : String(raw))} ${t(value.ModelPrice === undefined ? 'perMillion' : 'perRequest')}`
}
async function load(forceSettings = false) {
  controller?.abort()
  controller = new AbortController()
  const request = controller
  loading.value = true
  ready.value = false
  error.value = ''
  try {
    const [config, names, settingsLoaded] = await Promise.all([
      pricingConfigApi.load(request.signal),
      api.get<string[]>('/api/channel/models_enabled', undefined, {
        signal: request.signal,
      }),
      settings.load(forceSettings),
    ])
    if (request.signal.aborted) return
    if (!settingsLoaded) throw new Error(t('loadError'))
    baseline.value = config
    draft.value = initialPricingDraft(config)
    enabledModels.value = names
    json.value = pricingJSON(draft.value)
    editorDirty.value = false
    expose.value = ['true', '1'].includes(
      String(settings.rawValue('ExposeRatioEnabled', false))
    )
    toolsJSON.value = String(
      settings.rawValue('tool_price_setting.prices', '{}')
    )
    toolsDirty.value = false
    toolsRevision.value++
    ready.value = true
    revision.value++
    if (typeof route.query.model === 'string') editing.value = route.query.model
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (controller === request) loading.value = false
  }
}
function updateDraft(name: string, pricing: PricingValues) {
  draft.value = { ...draft.value, [name]: clonePricing(pricing) }
  if (editing.value === '__new__') editing.value = name
}
function commitOpen() {
  return !editor.value || !editorDirty.value || Boolean(editor.value.commit())
}
function open(name: string) {
  if (!commitOpen()) return
  editing.value = name
  editorDirty.value = false
  revision.value++
}
function closeEditor() {
  if (commitOpen()) editing.value = null
}
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
    error.value =
      cause instanceof Error
        ? te(cause.message)
          ? t(cause.message)
          : cause.message
        : String(cause)
  }
}
async function save() {
  if (!baseline.value || !ready.value || loading.value || saving.value) return
  error.value = ''
  message.value = ''
  if (!commitOpen()) return
  saving.value = true
  try {
    if (mode.value === 'json') draft.value = parsePricingJSON(json.value)
    const changes = pricingChanges(baseline.value, draft.value)
    for (const change of changes) validatePricing(change.pricing)
    if (changes.length) await pricingConfigApi.save(changes)
    const config = await pricingConfigApi.load()
    baseline.value = config
    draft.value = initialPricingDraft(config)
    json.value = pricingJSON(draft.value)
    editorDirty.value = false
    revision.value++
    message.value = t('saved')
  } catch (cause) {
    error.value = `${t('conflict')} ${cause instanceof Error ? (te(cause.message) ? t(cause.message) : cause.message) : String(cause)}`
  } finally {
    saving.value = false
  }
}
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
      error.value =
        cause instanceof Error
          ? te(cause.message)
            ? t(cause.message)
            : cause.message
          : String(cause)
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
    error.value =
      cause instanceof Error
        ? te(cause.message)
          ? t(cause.message)
          : cause.message
        : String(cause)
  }
}
async function changeExpose() {
  if (!(await settings.saveOptions({ ExposeRatioEnabled: expose.value })))
    expose.value = !expose.value
}
async function saveTools(value: string) {
  if (!ready.value || loading.value || saving.value || settings.saving.value)
    return
  error.value = ''
  if (await settings.saveOptions({ 'tool_price_setting.prices': value })) {
    toolsJSON.value = value
    toolsDirty.value = false
    toolsRevision.value++
    message.value = t('toolsSaved')
  }
}
function reload() {
  if (saving.value || settings.saving.value) return
  if (!dirty.value || window.confirm(t('refreshConfirm'))) void load(true)
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
  controller?.abort()
  if (props.embedded) registerBar?.(null)
})
</script>
<template>
  <section
    class="model-pricing-page"
    :class="{ 'is-embedded': embedded }"
    :aria-label="t('title')"
  >
    <PageBreadcrumb
      v-if="!embedded"
      :crumbs="[tGlobal('nav.groupAdmin'), t('title')]"
    >
      <template #action>
        <PricingActionBar :state="barState" />
      </template>
    </PageBreadcrumb>

    <fieldset
      class="pricing-controls"
      :disabled="loading || saving || settings.saving.value || !ready"
    >
      <div
        v-if="error"
        role="alert"
        class="mb-3 flex items-center gap-2 rounded-xl border border-[var(--status-danger)]/20 bg-[var(--status-danger-soft)] px-3.5 py-2.5 text-xs text-[var(--status-danger-text)]"
      >
        <span>{{ error }}</span>
      </div>
      <div
        v-if="message"
        role="status"
        class="mb-3 flex items-center gap-2 rounded-xl border border-[var(--status-success)]/20 bg-[var(--status-success-soft)] px-3.5 py-2.5 text-xs text-[var(--status-success-text)]"
      >
        <span class="text-[var(--text-secondary)]">{{ message }}</span>
      </div>

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
          <label
            class="inline-flex items-center gap-2 cursor-pointer text-xs text-[var(--text-secondary)] select-none"
            :title="t('exposeHint')"
          >
            <input
              v-model="expose"
              type="checkbox"
              class="checkbox-round"
              @change="changeExpose"
            />
            <span>{{ t('expose') }}</span>
          </label>
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
        <div v-show="tab === 'tools'">
          <ConsoleCard :padded="true">
            <ToolPricingEditor
              :key="toolsRevision"
              :value="toolsJSON"
              @save="saveTools"
              @dirty="toolsDirty = $event"
            />
          </ConsoleCard>
        </div>

        <template v-if="tab !== 'tools'">
          <ConsoleCard v-if="mode === 'json'" :padded="true">
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <label v-for="key in PRICING_KEYS" :key="key" class="block">
                <span
                  class="mb-1.5 block text-xs font-mono font-medium text-[var(--text-secondary)]"
                  >{{ key }}</span
                >
                <textarea
                  v-model="json[key]"
                  rows="7"
                  spellcheck="false"
                  class="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs text-[var(--text-primary)] transition-colors focus:border-[var(--accent)] focus-ring outline-none"
                />
              </label>
            </div>
          </ConsoleCard>

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
                  <div class="grid min-w-0 gap-1.5">
                    <button
                      type="button"
                      class="pricing-model-name font-mono"
                      @click="open(row.name as string)"
                    >
                      {{ row.name }}
                    </button>
                    <div class="flex min-w-0 items-center gap-2">
                      <StatusChip
                        class="shrink-0"
                        :tone="modeTone(row.mode as string, row.name as string)"
                      >
                        {{ modeLabel(row.mode as string, row.name as string) }}
                      </StatusChip>
                      <span
                        class="pricing-summary min-w-0 font-mono"
                        :title="row.summary as string"
                      >
                        {{ row.summary }}
                      </span>
                    </div>
                  </div>
                </template>
                <template #cell-actions="{ row }">
                  <ConsoleButton
                    variant="ghost"
                    size="sm"
                    @click="remove(row.name as string)"
                  >
                    {{ t('remove') }}
                  </ConsoleButton>
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

            <div class="pricing-editor-container">
              <template v-if="!mobile">
                <ConsoleCard v-if="editing !== null" :padded="false">
                  <ModelPricingEditor
                    ref="editor"
                    :model-name="activeName"
                    :pricing="activePricing"
                    :entry="activeEntry"
                    :revision="revision"
                    :saving="saving"
                    :site-currency="siteCurrency"
                    :exchange-rate="exchangeRate"
                    @update="updateDraft"
                    @dirty="editorDirty = $event"
                    @save="save"
                  />
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
              </template>
            </div>
          </div>
        </template>
      </section>
    </fieldset>

    <ConsoleModal
      v-if="mobile && editing !== null"
      :open="true"
      :title="t('edit')"
      presentation="drawer"
      size="xl"
      :close-disabled="loading || saving"
      @close="closeEditor"
    >
      <fieldset
        class="pricing-controls"
        :disabled="loading || saving || !ready"
      >
        <ModelPricingEditor
          ref="editor"
          :model-name="activeName"
          :pricing="activePricing"
          :entry="activeEntry"
          :revision="revision"
          :saving="saving"
          :site-currency="siteCurrency"
          :exchange-rate="exchangeRate"
          @update="updateDraft"
          @dirty="editorDirty = $event"
          @save="save"
        />
      </fieldset>
    </ConsoleModal>

    <PriceFetchDialog
      v-if="fetchOpen"
      :draft="draft"
      @close="fetchOpen = false"
      @fill="fill"
    />

    <ConfirmDialog
      :open="clearOpen"
      :title="t('clear')"
      :message="t('clearConfirm')"
      :confirm-text="t('clear')"
      @confirm="clear"
      @cancel="clearOpen = false"
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
  container-type: inline-size;
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
  text-align: start;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--text-primary);
  text-decoration: none;
  border: none;
  background: transparent;
  padding: 0;
  cursor: pointer;
}
.pricing-model-name:hover {
  color: var(--accent);
  text-decoration: underline;
}
.pricing-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: var(--text-secondary);
}
.pricing-editor-container {
  min-width: 0;
}
@container (max-width: 820px) {
  .pricing-workspace {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 767px) {
  .pricing-workspace {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
