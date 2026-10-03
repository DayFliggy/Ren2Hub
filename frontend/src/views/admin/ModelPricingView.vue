<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
import { Coins, Copy, Plus, RefreshCw, Save, Sparkles } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelPricingEditor from '@/components/admin/ModelPricingEditor.vue'
import ToolPricingEditor from '@/components/admin/ToolPricingEditor.vue'
import PriceFetchDialog from '@/components/admin/PriceFetchDialog.vue'
import '@/components/admin/admin.css'

const props = withDefaults(defineProps<{ embedded?: boolean }>(), {
  embedded: false,
})
const { t, te } = useI18n({ useScope: 'local', messages: modelPricingMessages })
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
let controller: AbortController | undefined
watch([search, filter, tab, pageSize], () => {
  page.value = 1
  selected.value = []
})
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
})
</script>
<template>
  <section
    class="admin-page model-pricing-page"
    :class="{ 'is-embedded': embedded }"
    :aria-label="t('title')"
  >
    <div class="pricing-card-header">
      <div class="admin-toolbar pricing-toolbar">
        <h1 v-if="!embedded">{{ t('title') }}</h1>
        <p v-else-if="dirty" class="pricing-lead" role="status">
          <span class="pricing-dirty-dot" aria-hidden="true" />
          {{ t('dirty') }}
        </p>
        <span v-else class="pricing-toolbar-spacer" aria-hidden="true" />
        <div class="pricing-toolbar-actions">
          <span v-if="dirty && !embedded" class="admin-muted" role="status">{{
            t('dirty')
          }}</span>
          <ConsoleButton
            variant="ghost"
            size="sm"
            :loading="loading"
            :disabled="saving || settings.saving.value"
            @click="reload"
          >
            <RefreshCw :size="13" class="mr-1.5" aria-hidden="true" />{{
              t('reload')
            }}
          </ConsoleButton>
          <ConsoleButton
            v-if="tab !== 'tools'"
            variant="secondary"
            size="sm"
            :disabled="loading || saving || !ready"
            @click="beginFetch"
          >
            <Sparkles :size="13" class="mr-1.5" aria-hidden="true" />{{
              t('fetch')
            }}
          </ConsoleButton>
          <ConsoleButton
            v-if="tab !== 'tools'"
            size="sm"
            :loading="saving"
            :disabled="loading || !ready"
            @click="save"
          >
            <Save :size="13" class="mr-1.5" aria-hidden="true" />{{ t('save') }}
          </ConsoleButton>
        </div>
      </div>
      <div class="pricing-tabs-wrap">
        <ConsoleTabs
          :model-value="tab"
          :items="tabs"
          panel-id="model-pricing-panel"
          :density="embedded ? 'compact' : 'default'"
          :aria-label="t('title')"
          @update:model-value="
            (value) => {
              if (commitOpen()) tab = value
            }
          "
        />
      </div>
    </div>

    <fieldset
      class="pricing-controls"
      :disabled="loading || saving || settings.saving.value || !ready"
    >
      <div
        v-if="error"
        role="alert"
        class="mb-3 flex items-center gap-2 rounded-lg border border-[var(--status-danger)]/20 bg-[var(--status-danger-soft)] px-3.5 py-2.5 text-xs text-[var(--status-danger-text)]"
      >
        <span class="admin-error">{{ error }}</span>
      </div>
      <div
        v-if="message"
        role="status"
        class="mb-3 flex items-center gap-2 rounded-lg border border-[var(--status-success)]/20 bg-[var(--status-success-soft)] px-3.5 py-2.5 text-xs text-[var(--status-success-text)]"
      >
        <span class="admin-muted">{{ message }}</span>
      </div>

      <section id="model-pricing-panel" role="tabpanel">
        <div v-show="tab === 'tools'" class="pricing-tools-wrap">
          <ToolPricingEditor
            :key="toolsRevision"
            :value="toolsJSON"
            @save="saveTools"
            @dirty="toolsDirty = $event"
          />
        </div>

        <template v-if="tab !== 'tools'">
          <div class="pricing-top-actions">
            <label class="admin-check" :title="t('exposeHint')">
              <input v-model="expose" type="checkbox" @change="changeExpose" />
              {{ t('expose') }}
            </label>
            <ConsoleButton variant="ghost" size="sm" @click="clearOpen = true">
              {{ t('clear') }}
            </ConsoleButton>
            <ConsoleButton variant="secondary" size="sm" @click="switchMode">
              {{ t(mode === 'visual' ? 'json' : 'visual') }}
            </ConsoleButton>
          </div>

          <div v-if="mode === 'json'" class="pricing-json">
            <label v-for="key in PRICING_KEYS" :key="key" class="admin-field">
              <span>{{ key }}</span>
              <textarea v-model="json[key]" rows="7" spellcheck="false" />
            </label>
          </div>

          <div v-else class="pricing-workspace">
            <div class="pricing-models">
              <div class="pricing-filter">
                <SearchInput
                  v-model="search"
                  :placeholder="t('search')"
                  :aria-label="t('search')"
                  class="min-w-0 flex-1"
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

              <div v-if="selected.length" class="pricing-selected-strip">
                <span
                  class="inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--accent-text)]"
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

              <div class="pricing-table">
                <table class="admin-table">
                  <thead>
                    <tr>
                      <th>
                        <input
                          type="checkbox"
                          :aria-label="t('chooseAll')"
                          :checked="
                            pageRows.length > 0 &&
                            pageRows.every((name) => selected.includes(name))
                          "
                          @change="
                            selected = ($event.target as HTMLInputElement)
                              .checked
                              ? [...new Set([...selected, ...pageRows])]
                              : selected.filter(
                                  (name) => !pageRows.includes(name)
                                )
                          "
                        />
                      </th>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('mode') }}</th>
                      <th>{{ t('summary') }}</th>
                      <th>{{ t('actions') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="name in pageRows"
                      :key="name"
                      :data-active="editing === name"
                    >
                      <td>
                        <input
                          v-model="selected"
                          type="checkbox"
                          :value="name"
                          :aria-label="`${t('choose')} ${name}`"
                        />
                      </td>
                      <td>
                        <button
                          type="button"
                          class="pricing-model-name"
                          @click="open(name)"
                        >
                          {{ name }}
                        </button>
                      </td>
                      <td>
                        <span
                          class="pricing-mode-pill"
                          :class="`is-${pricingMode(draft[name] ?? {})}`"
                        >
                          {{
                            t(
                              !Object.keys(draft[name] ?? {}).length
                                ? 'unpriced'
                                : pricingMode(draft[name]!) === 'tiered_expr'
                                  ? 'expression'
                                  : pricingMode(draft[name]!) === 'per-request'
                                    ? 'perCall'
                                    : 'perToken'
                            )
                          }}
                        </span>
                      </td>
                      <td
                        class="pricing-summary"
                        :title="summary(draft[name] ?? {})"
                      >
                        {{ summary(draft[name] ?? {}) }}
                      </td>
                      <td>
                        <ConsoleButton
                          variant="ghost"
                          size="sm"
                          @click="remove(name)"
                        >
                          {{ t('remove') }}
                        </ConsoleButton>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <div v-if="!loading && !rows.length" class="pricing-empty">
                <div class="pricing-empty-badge">
                  <Coins :size="24" class="opacity-40" />
                </div>
                <p class="font-medium text-[var(--text-primary)]">
                  {{ t(search ? 'noResults' : 'empty') }}
                </p>
                <p class="text-xs text-[var(--text-tertiary)]">
                  {{ t('emptyHint') }}
                </p>
              </div>

              <TablePagination
                :page="page"
                :page-size="pageSize"
                :total="rows.length"
                @update:page="page = $event"
                @update:page-size="pageSize = $event"
              />
            </div>

            <div class="pricing-editor-container">
              <template v-if="!mobile">
                <ModelPricingEditor
                  v-if="editing !== null"
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
                <div v-else class="pricing-empty pricing-editor-empty">
                  <div class="pricing-empty-badge">
                    <Coins :size="30" class="text-[var(--accent)]" />
                  </div>
                  <p class="text-sm font-semibold text-[var(--text-primary)]">
                    {{ t('select') }}
                  </p>
                  <p
                    class="max-w-xs text-center text-xs text-[var(--text-tertiary)]"
                  >
                    {{ t('emptyHint') }}
                  </p>
                  <ConsoleButton
                    variant="secondary"
                    size="sm"
                    @click="open('__new__')"
                  >
                    <Plus :size="13" class="mr-1" aria-hidden="true" />{{
                      t('add')
                    }}
                  </ConsoleButton>
                </div>
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
#model-pricing-panel {
  min-width: 0;
  max-width: 100%;
}
.model-pricing-page {
  min-width: 0;
  max-width: 100%;
  container-type: inline-size;
}
.pricing-card-header {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
  min-width: 0;
  max-width: 100%;
}
.is-embedded .pricing-card-header {
  margin-bottom: 0.5rem;
}
.pricing-toolbar {
  align-items: center;
  width: 100%;
}
.pricing-lead {
  flex: 1;
  min-width: 8rem;
  margin: 0;
  font-size: 0.8125rem;
  font-weight: 500;
  line-height: 1.4;
  color: var(--status-warning-text);
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}
.pricing-dirty-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 9999px;
  background: var(--status-warning);
  display: inline-block;
  box-shadow: 0 0 0 2px var(--status-warning-soft);
}
.pricing-toolbar-spacer {
  flex: 1;
  min-width: 0;
}
.pricing-toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-left: auto;
}
.pricing-tabs-wrap {
  min-width: 0;
}
.pricing-top-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.625rem;
  margin-bottom: 0.875rem;
}
.pricing-workspace {
  display: grid;
  grid-template-columns: minmax(320px, 0.85fr) minmax(440px, 1.15fr);
  gap: 1.25rem;
  align-items: start;
}
.pricing-models {
  min-width: 0;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  box-shadow: var(--card-shadow);
  overflow: hidden;
}
.pricing-filter {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem;
  border-bottom: 1px solid var(--border-subtle);
}
.pricing-table {
  overflow: auto;
  max-height: 60vh;
}
.pricing-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
}
.pricing-table table {
  min-width: 580px;
  table-layout: fixed;
}
.pricing-table th:first-child {
  width: 2.75rem;
  text-align: center;
}
.pricing-table td:first-child {
  text-align: center;
}
.pricing-table th:nth-child(2) {
  width: 11rem;
}
.pricing-table th:nth-child(3) {
  width: 6.5rem;
}
.pricing-table th:last-child {
  width: 5.5rem;
  text-align: right;
}
.pricing-table td:last-child {
  text-align: right;
}
.pricing-model-name {
  text-align: start;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
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
.pricing-mode-pill {
  display: inline-block;
  padding: 0.125rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 500;
  border-radius: 9999px;
  white-space: nowrap;
}
.pricing-mode-pill.is-tiered_expr {
  background: var(--accent-soft);
  color: var(--accent-text);
}
.pricing-mode-pill.is-per-token {
  background: var(--surface-muted);
  color: var(--text-secondary);
}
.pricing-mode-pill.is-per-request {
  background: var(--status-warning-soft);
  color: var(--status-warning-text);
}
.pricing-mode-pill.is-unpriced {
  background: var(--surface-muted);
  color: var(--text-tertiary);
}
.pricing-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  color: var(--text-secondary);
}
.pricing-empty {
  min-height: 12rem;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 2rem 1.5rem;
  gap: 0.5rem;
  color: var(--text-secondary);
}
.pricing-empty-badge {
  width: 3.5rem;
  height: 3.5rem;
  border-radius: 9999px;
  background: var(--accent-soft);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 0.5rem;
}
.pricing-selected-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  padding: 0.625rem 1rem;
  background: color-mix(in srgb, var(--accent-soft) 50%, transparent);
  border-bottom: 1px solid var(--border-subtle);
}
.pricing-editor-container {
  min-width: 0;
}
.pricing-editor-empty {
  min-height: 28rem;
  border: 1px dashed var(--border-subtle);
  border-radius: 1rem;
  background: var(--surface-solid);
  box-shadow: var(--card-shadow);
}
.pricing-json {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  padding: 1.25rem;
  box-shadow: var(--card-shadow);
}
.pricing-tools-wrap {
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  padding: 1.25rem;
  box-shadow: var(--card-shadow);
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}
@media (max-width: 640px) {
  .pricing-tools-wrap {
    padding: 0.875rem;
  }
}
@container (max-width: 820px) {
  .pricing-workspace {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 767px) {
  .pricing-workspace,
  .pricing-json {
    grid-template-columns: minmax(0, 1fr);
  }
  .pricing-top-actions {
    justify-content: flex-start;
  }
}
</style>
