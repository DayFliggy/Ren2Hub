<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
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
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ModelPricingEditor from '@/components/admin/ModelPricingEditor.vue'
import ToolPricingEditor from '@/components/admin/ToolPricingEditor.vue'
import PriceFetchDialog from '@/components/admin/PriceFetchDialog.vue'
import '@/components/admin/admin.css'
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
  { key: 'models', label: t('models') },
  { key: 'unset', label: t('unset') },
  { key: 'tools', label: t('tools') },
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
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value || saving.value || settings.saving.value)
    event.preventDefault()
}
const keepDraft = () =>
  !saving.value &&
  !settings.saving.value &&
  (!dirty.value || window.confirm(t('unsaved')))
onBeforeRouteLeave(keepDraft)
// Billing sections share one route record, so switching to another section
// is a route update that unmounts this workspace.
onBeforeRouteUpdate((to, from) =>
  to.params.section === from.params.section ? true : keepDraft()
)
onMounted(() => {
  void load()
  window.addEventListener('beforeunload', beforeUnload)
})
onBeforeUnmount(() => {
  controller?.abort()
  window.removeEventListener('beforeunload', beforeUnload)
})
</script>
<template>
  <main class="admin-page model-pricing-page">
    <div class="admin-toolbar">
      <h1>{{ t('title') }}</h1>
      <span v-if="dirty" class="admin-muted" role="status">{{
        t('dirty')
      }}</span
      ><ConsoleButton
        variant="ghost"
        :loading="loading"
        :disabled="saving || settings.saving.value"
        @click="reload"
        >{{ t('reload') }}</ConsoleButton
      ><ConsoleButton
        v-if="tab !== 'tools'"
        variant="secondary"
        :disabled="loading || saving || !ready"
        @click="beginFetch"
        >{{ t('fetch') }}</ConsoleButton
      ><ConsoleButton
        v-if="tab !== 'tools'"
        :loading="saving"
        :disabled="loading || !ready"
        @click="save"
        >{{ t('save') }}</ConsoleButton
      >
    </div>
    <fieldset
      class="pricing-controls"
      :disabled="loading || saving || settings.saving.value || !ready"
    >
      <ConsoleTabs
        :model-value="tab"
        :items="tabs"
        panel-id="model-pricing-panel"
        @update:model-value="
          (value) => {
            if (commitOpen()) tab = value
          }
        "
      />
      <p v-if="error" role="alert" class="admin-error">{{ error }}</p>
      <p v-if="message" role="status" class="admin-muted">{{ message }}</p>
      <section id="model-pricing-panel" role="tabpanel">
        <ToolPricingEditor
          :key="toolsRevision"
          v-show="tab === 'tools'"
          :value="toolsJSON"
          @save="saveTools"
          @dirty="toolsDirty = $event"
        />
        <template v-if="tab !== 'tools'">
          <div class="pricing-top-actions">
            <label class="admin-check" :title="t('exposeHint')"
              ><input
                v-model="expose"
                type="checkbox"
                @change="changeExpose"
              />{{ t('expose') }}</label
            ><ConsoleButton
              variant="ghost"
              size="sm"
              @click="clearOpen = true"
              >{{ t('clear') }}</ConsoleButton
            ><ConsoleButton variant="secondary" size="sm" @click="switchMode">{{
              t(mode === 'visual' ? 'json' : 'visual')
            }}</ConsoleButton>
          </div>
          <div v-if="mode === 'json'" class="pricing-json">
            <label v-for="key in PRICING_KEYS" :key="key" class="admin-field"
              ><span>{{ key }}</span
              ><textarea v-model="json[key]" rows="7" spellcheck="false" />
            </label>
          </div>
          <div v-else class="pricing-workspace">
            <div class="pricing-models">
              <div class="pricing-filter">
                <label class="admin-field"
                  ><span class="sr-only">{{ t('search') }}</span
                  ><input v-model="search" :placeholder="t('search')" /></label
                ><select v-model="filter" :aria-label="t('mode')">
                  <option value="">{{ t('all') }}</option>
                  <option value="tiered_expr">{{ t('expression') }}</option>
                  <option value="per-token">{{ t('token') }}</option>
                  <option value="per-request">
                    {{ t('request') }}
                  </option></select
                ><ConsoleButton
                  v-if="tab === 'models'"
                  size="sm"
                  @click="open('__new__')"
                  >{{ t('add') }}</ConsoleButton
                >
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
                          >{{ t('remove') }}</ConsoleButton
                        >
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div v-if="!loading && !rows.length" class="pricing-empty">
                <p>{{ t(search ? 'noResults' : 'empty') }}</p>
                <p>{{ t('emptyHint') }}</p>
              </div>
              <TablePagination
                :page="page"
                :page-size="pageSize"
                :total="rows.length"
                @update:page="page = $event"
                @update:page-size="pageSize = $event"
              />
              <div v-if="selected.length" class="pricing-top-actions">
                <span>{{ t('selected', { count: selected.length }) }}</span
                ><ConsoleButton
                  variant="secondary"
                  size="sm"
                  :disabled="!editing"
                  @click="copy"
                  >{{ t('copy') }}</ConsoleButton
                >
              </div>
            </div>
            <template v-if="!mobile"
              ><ModelPricingEditor
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
                <p>{{ t('select') }}</p>
                <ConsoleButton variant="secondary" @click="open('__new__')">{{
                  t('add')
                }}</ConsoleButton>
              </div></template
            >
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
      ><fieldset
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
        /></fieldset
    ></ConsoleModal>
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
  </main>
</template>
<style scoped>
.pricing-controls {
  min-width: 0;
  margin: 0;
  border: 0;
  padding: 0;
}
.model-pricing-page {
  min-width: 0;
  /* The settings rail narrows this page, so the split follows its own width. */
  container-type: inline-size;
}
.pricing-top-actions,
.pricing-filter {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.65rem;
  margin: 1rem 0;
}
.pricing-top-actions {
  justify-content: flex-end;
}
.pricing-filter label {
  flex: 1 1 10rem;
}
.pricing-workspace {
  display: grid;
  grid-template-columns: minmax(300px, 0.8fr) minmax(430px, 1.2fr);
  gap: 1.25rem;
  align-items: start;
}
.pricing-models {
  min-width: 0;
}
.pricing-table {
  overflow: auto;
  max-height: 62vh;
  border: 1px solid var(--border-subtle);
  border-radius: 0.65rem;
}
.pricing-table table {
  min-width: 650px;
  table-layout: fixed;
}
.pricing-table th:first-child {
  width: 2.5rem;
}
.pricing-table th:nth-child(2) {
  width: 12rem;
}
.pricing-table th:nth-child(3),
.pricing-table th:last-child {
  width: 5rem;
}
.pricing-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
}
.pricing-model-name {
  text-align: start;
  overflow-wrap: anywhere;
}
tr[data-active='true'] {
  background: var(--surface-hover);
}
.pricing-empty {
  min-height: 10rem;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 1.5rem;
  gap: 0.75rem;
  border: 1px dashed var(--border-subtle);
  color: var(--text-secondary);
  border-radius: 0.75rem;
}
.pricing-editor-empty {
  min-height: 28rem;
  margin-top: 1rem;
}
.pricing-json {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
textarea {
  font-family: monospace;
}
@container (max-width: 760px) {
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
