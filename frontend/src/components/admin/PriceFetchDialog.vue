<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import {
  pricingConfigApi,
  type FetchedPriceSource,
  type FetchedModelPrice,
  type FetchedUpstreamPrices,
  type PriceSourceFingerprint,
  type PriceUpstream,
} from '@/api/pricingConfig'
import { modelPricingMessages } from '@/i18n/modelPricing'
import type { PricingDraft } from '@/features/pricing/draft'
import { pricingDiff } from '@/features/pricing/diff'
import {
  adjustPrice,
  type AdjustedPrice,
  type PriceAdjustment,
} from '@/features/pricing/priceAdjustment'
import PriceDiffCell from './PriceDiffCell.vue'
// exchangeRate is the site currency per USD; prices are always saved in USD.
const props = defineProps<{
  draft: PricingDraft
  exchangeRate: number
  currency: string
}>()
const emit = defineEmits<{ close: []; fill: [rows: FetchedModelPrice[]] }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const sources = ref<FetchedPriceSource[]>([])
const upstreams = ref<PriceUpstream[]>([])
const upstreamPrices = ref<FetchedUpstreamPrices>()
const source = ref(''),
  search = ref(''),
  selected = ref<string[]>([])
const loading = ref(false),
  error = ref('')
const page = ref(1),
  pageSize = ref(50)
let controller: AbortController | undefined
const upstreamKey = (upstream: PriceUpstream) => `upstream:${upstream.id}`
const upstream = computed(() =>
  upstreams.value.find((item) => upstreamKey(item) === source.value)
)
const current = computed(() =>
  upstream.value
    ? upstreamPrices.value
    : sources.value.find((item) => item.id === source.value)
)
const hasOpenRouter = computed(() =>
  upstreams.value.some((item) => item.kind === 'openrouter')
)
function upstreamName(item: PriceUpstream) {
  return item.kind === 'openrouter' ? item.name : t(`upstream_${item.kind}`)
}
// null published digest or matches means unverified, never a match.
function fingerprintState(fingerprint: PriceSourceFingerprint | null) {
  if (!fingerprint) return 'fingerprintMissing'
  if (fingerprint.matches === null) return 'fingerprintUnverified'
  return fingerprint.matches ? 'fingerprintMatched' : 'fingerprintMismatched'
}
const keyFor = (model: FetchedModelPrice) =>
  JSON.stringify([model.model_name, model.provider])
const rows = computed(() =>
  (current.value?.models ?? []).filter((model) =>
    `${model.model_name} ${model.provider}`
      .toLowerCase()
      .includes(search.value.toLowerCase())
  )
)
const visibleRows = computed(() =>
  rows.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const selectedKeys = computed(() => new Set(selected.value))
const ADJUSTMENTS: PriceAdjustment[] = ['keep', 'divide', 'multiply']
// Per-row choice keyed like the selection; the source quote is never mutated.
const adjustments = ref<Record<string, PriceAdjustment>>({})
const bulkAdjustment = ref<PriceAdjustment>('keep')
const adjustmentOf = (model: FetchedModelPrice) =>
  adjustments.value[keyFor(model)] ?? 'keep'
function adjusted(model: FetchedModelPrice): AdjustedPrice {
  return adjustPrice(model.pricing, adjustmentOf(model), props.exchangeRate)
}
const comparisons = computed(
  () =>
    new Map(
      visibleRows.value.map((model) => [
        keyFor(model),
        pricingDiff(
          props.draft[model.model_name] ?? {},
          adjusted(model).values ?? model.pricing
        ),
      ])
    )
)
const chosen = computed(() =>
  (current.value?.models ?? []).filter((model) =>
    selectedKeys.value.has(keyFor(model))
  )
)
const chosenAdjusted = computed(() =>
  chosen.value.map((model) => ({ model, result: adjusted(model) }))
)
const adjustedCount = computed(
  () => chosen.value.filter((model) => adjustmentOf(model) !== 'keep').length
)
const adjustmentFailed = computed(() =>
  chosenAdjusted.value.some(({ result }) => result.error)
)
function applyBulkAdjustment() {
  const next = { ...adjustments.value }
  for (const key of selected.value) next[key] = bulkAdjustment.value
  adjustments.value = next
}
function fill() {
  emit(
    'fill',
    chosenAdjusted.value.map(({ model, result }) => ({
      ...model,
      pricing: result.values ?? model.pricing,
    }))
  )
}
const overwrite = computed(
  () =>
    chosen.value.filter(
      (model) => Object.keys(props.draft[model.model_name] ?? {}).length
    ).length
)
const duplicate = computed(
  () =>
    new Set(chosen.value.map((model) => model.model_name)).size !==
    chosen.value.length
)
watch(source, () => {
  selected.value = []
  adjustments.value = {}
  search.value = ''
  upstreamPrices.value = undefined
  if (upstream.value) void fetchUpstream(upstream.value)
})
watch([source, search, pageSize], () => {
  page.value = 1
})
// One request at a time; a new request aborts the previous one.
async function load(task: (signal: AbortSignal) => Promise<void>) {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  try {
    await task(request.signal)
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (controller === request) loading.value = false
  }
}
function fetchPrices() {
  sources.value = []
  upstreams.value = []
  selected.value = []
  source.value = ''
  return load(async (signal) => {
    const [local, native] = await Promise.all([
      pricingConfigApi.fetch(signal),
      pricingConfigApi.upstreams(signal),
    ])
    if (signal.aborted) return
    sources.value = local
    upstreams.value = native
  })
}
function fetchUpstream(item: PriceUpstream) {
  return load(async (signal) => {
    const result = await pricingConfigApi.fetchUpstream(item, signal)
    if (!signal.aborted) upstreamPrices.value = result
  })
}
function selectVisible() {
  selected.value = rows.value
    .filter((row) => !row.unsupported_reason && Object.keys(row.pricing).length)
    .map(keyFor)
}
onMounted(fetchPrices)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <ConsoleModal
    :open="true"
    :title="t('sourceTitle')"
    size="xl"
    @close="emit('close')"
  >
    <div class="price-fetch" :aria-busy="loading">
      <p class="text-xs text-[var(--text-tertiary)]">{{ t('sourceHint') }}</p>
      <fieldset class="price-source-list">
        <legend class="sr-only">{{ t('source') }}</legend>
        <label
          v-for="{ id, name, status, models, fingerprint } in sources"
          :key="id"
          class="price-source"
          :data-selected="source === id"
        >
          <input
            v-model="source"
            type="radio"
            name="price-source"
            :value="id"
            :disabled="
              loading ||
              !models.some(
                (model) =>
                  !model.unsupported_reason && Object.keys(model.pricing).length
              )
            "
          />
          <span>{{ name }}</span>
          <small>{{ t(status) }}</small>
          <small>{{
            t('sourceCount', {
              count: models.filter((model) => !model.unsupported_reason).length,
            })
          }}</small>
          <small :data-fingerprint="fingerprintState(fingerprint)">{{
            t(fingerprintState(fingerprint))
          }}</small>
          <details v-if="fingerprint" class="price-fingerprint">
            <summary>{{ t('fingerprintDetails') }}</summary>
            <code :title="fingerprint.sha256">{{
              t('downloadedSha256', { digest: fingerprint.sha256 })
            }}</code>
            <code
              v-if="fingerprint.matches === false"
              :title="fingerprint.published_sha256 ?? ''"
              >{{
                t('publishedSha256', { digest: fingerprint.published_sha256 })
              }}</code
            >
          </details>
        </label>
      </fieldset>
      <p class="text-xs text-[var(--text-tertiary)]">{{ t('upstreamHint') }}</p>
      <fieldset v-if="upstreams.length" class="price-source-list">
        <legend class="sr-only">{{ t('upstreamSource') }}</legend>
        <label
          v-for="item in upstreams"
          :key="item.id"
          class="price-source"
          :data-selected="source === upstreamKey(item)"
        >
          <input
            v-model="source"
            type="radio"
            name="price-source"
            :value="upstreamKey(item)"
            :disabled="loading"
          />
          <span>{{ upstreamName(item) }}</span>
          <small>{{
            t(item.kind === 'openrouter' ? 'upstreamChannel' : 'upstreamPreset')
          }}</small>
          <small v-if="source === upstreamKey(item)">{{
            loading
              ? t('loading')
              : t('sourceCount', { count: upstreamPrices?.models.length ?? 0 })
          }}</small>
        </label>
      </fieldset>
      <p
        v-if="upstreams.length && !hasOpenRouter"
        class="text-xs text-[var(--text-tertiary)]"
      >
        {{ t('noOpenRouter') }}
      </p>
      <p
        v-if="error"
        class="text-xs text-[var(--status-danger-text)]"
        role="alert"
      >
        {{ error }}
      </p>
      <ul
        v-for="item in sources.filter((item) => item.errors.length)"
        :key="item.id"
        class="text-xs text-[var(--status-danger-text)]"
      >
        <li v-for="message in item.errors" :key="message">
          {{ item.name }}: {{ message }}
        </li>
      </ul>
      <ul
        v-if="upstream && upstreamPrices?.errors.length"
        class="text-xs text-[var(--status-danger-text)]"
      >
        <li v-for="message in upstreamPrices.errors" :key="message">
          {{ upstreamName(upstream) }}: {{ message }}
        </li>
      </ul>
      <ul
        v-for="item in sources.filter((item) => item.warnings.length)"
        :key="item.id"
        class="text-xs text-[var(--status-warning-text)]"
      >
        <li v-for="message in item.warnings" :key="message">
          {{ item.name }}: {{ message }}
        </li>
      </ul>
      <template v-if="current">
        <div class="price-fetch-tools">
          <SearchInput
            v-model="search"
            :placeholder="t('search')"
            :aria-label="t('search')"
            class="w-64"
          />
          <ConsoleButton variant="ghost" size="sm" @click="selectVisible">{{
            t('chooseAll')
          }}</ConsoleButton>
          <ConsoleButton variant="ghost" size="sm" @click="selected = []">{{
            t('clearSelection')
          }}</ConsoleButton>
        </div>
        <div class="price-fetch-tools">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('exchangeRateHint', { rate: exchangeRate, currency }) }}
          </p>
          <select
            v-model="bulkAdjustment"
            class="price-adjustment"
            :aria-label="t('bulkAdjustment')"
          >
            <option v-for="mode in ADJUSTMENTS" :key="mode" :value="mode">
              {{ t(`adjust_${mode}`, { rate: exchangeRate }) }}
            </option>
          </select>
          <ConsoleButton
            variant="ghost"
            size="sm"
            :disabled="!chosen.length"
            @click="applyBulkAdjustment"
            >{{ t('bulkAdjustment') }}</ConsoleButton
          >
        </div>
        <div class="price-fetch-table">
          <table>
            <thead>
              <tr>
                <th>{{ t('choose') }}</th>
                <th>{{ t('name') }}</th>
                <th>{{ t('provider') }}</th>
                <th>{{ t('current') }}</th>
                <th>{{ t('adjustment') }}</th>
                <th>{{ t('finalPrice') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in visibleRows" :key="keyFor(model)">
                <td>
                  <input
                    v-model="selected"
                    type="checkbox"
                    class="checkbox-round"
                    :value="keyFor(model)"
                    :aria-label="`${t('choose')} ${model.model_name} ${model.provider}`"
                    :disabled="
                      Boolean(model.unsupported_reason) ||
                      !Object.keys(model.pricing).length
                    "
                  />
                </td>
                <td>{{ model.model_name }}</td>
                <td>{{ model.provider }}</td>
                <td>
                  <PriceDiffCell
                    :fields="comparisons.get(keyFor(model)) ?? []"
                    side="current"
                  />
                </td>
                <td>
                  <select
                    class="price-adjustment"
                    :value="adjustmentOf(model)"
                    :aria-label="`${t('adjustment')} ${model.model_name} ${model.provider}`"
                    :disabled="
                      Boolean(model.unsupported_reason) ||
                      !Object.keys(model.pricing).length
                    "
                    @change="
                      adjustments = {
                        ...adjustments,
                        [keyFor(model)]: ($event.target as HTMLSelectElement)
                          .value as PriceAdjustment,
                      }
                    "
                  >
                    <option
                      v-for="mode in ADJUSTMENTS"
                      :key="mode"
                      :value="mode"
                    >
                      {{ t(`adjust_${mode}`, { rate: exchangeRate }) }}
                    </option>
                  </select>
                  <p
                    v-if="adjusted(model).error"
                    role="alert"
                    class="text-xs text-[var(--status-danger-text)]"
                  >
                    {{ t(`adjustError_${adjusted(model).error}`) }}
                  </p>
                </td>
                <td>
                  <p
                    v-if="model.unsupported_reason"
                    class="text-xs text-[var(--status-danger-text)]"
                  >
                    {{ model.unsupported_reason }}
                  </p>
                  <PriceDiffCell
                    v-else
                    :fields="comparisons.get(keyFor(model)) ?? []"
                    side="candidate"
                  />
                  <p
                    v-for="warning in model.warnings ?? []"
                    :key="warning"
                    class="text-xs text-[var(--text-tertiary)]"
                  >
                    {{ warning }}
                  </p>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="!rows.length" class="text-xs text-[var(--text-tertiary)]">
            {{ t('noResults') }}
          </p>
        </div>
        <TablePagination
          :page="page"
          :page-size="pageSize"
          :total="rows.length"
          @update:page="page = $event"
          @update:page-size="pageSize = $event"
        />
        <p role="status">{{ t('selected', { count: chosen.length }) }}</p>
        <p v-if="adjustedCount" role="status">
          {{ t('adjustedCount', { count: adjustedCount, rate: exchangeRate }) }}
        </p>
        <p v-if="overwrite" class="price-overwrite">
          {{ t('overwrite', { count: overwrite }) }}
        </p>
        <p
          v-if="duplicate"
          role="alert"
          class="text-xs text-[var(--status-danger-text)]"
        >
          {{ t('duplicate') }}
        </p>
      </template>
      <p v-else-if="!loading" class="text-xs text-[var(--text-tertiary)]">
        {{ t('noSource') }}
      </p>
      <div class="price-fetch-tools price-fetch-footer">
        <ConsoleButton
          variant="ghost"
          :loading="loading"
          @click="fetchPrices"
          >{{ t('retry') }}</ConsoleButton
        >
        <ConsoleButton variant="secondary" @click="emit('close')">{{
          t('cancel')
        }}</ConsoleButton>
        <ConsoleButton
          :disabled="loading || !chosen.length || duplicate || adjustmentFailed"
          @click="fill"
          >{{ t('fill') }}</ConsoleButton
        >
      </div>
    </div>
  </ConsoleModal>
</template>
<style scoped>
.price-fetch {
  display: grid;
  gap: 1rem;
  min-width: 0;
}
.price-source-list {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 0.5rem;
  border: 0;
  padding: 0;
}
.price-source {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  border: 1px solid var(--border-subtle);
  padding: 0.75rem;
  border-radius: 0.65rem;
  min-width: 0;
}
.price-source[data-selected='true'] {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.price-source small {
  width: 100%;
  color: var(--text-secondary);
}
.price-source small[data-fingerprint='fingerprintMismatched'] {
  color: var(--status-danger-text);
  font-weight: 600;
}
/* Digests are truncated visually; the full value stays in the text and title. */
.price-source code {
  width: 100%;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
.price-fingerprint {
  width: 100%;
  min-width: 0;
  color: var(--text-tertiary);
  font-size: 0.75rem;
}
.price-fingerprint summary {
  cursor: pointer;
  width: fit-content;
}
.price-fingerprint code {
  margin-top: 0.25rem;
}
.price-fetch-tools {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
.price-fetch-footer {
  justify-content: flex-end;
  border-top: 1px solid var(--border-subtle);
  padding-top: 0.75rem;
}
.price-fetch-table {
  max-height: 22rem;
  overflow: auto;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
}
.price-fetch-table table {
  min-width: 700px;
  width: 100%;
  border-collapse: collapse;
}
.price-fetch-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-table-header, var(--surface-muted));
  padding: 0.625rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--text-secondary);
  text-align: left;
  border-bottom: 1px solid var(--border-subtle);
}
.price-fetch-table tbody td {
  padding: 0.625rem 0.75rem;
  font-size: 0.8125rem;
  /* Dashed ledger line by day, solid hairline by night (tokens). */
  border-bottom: 1px var(--row-divider-style, dashed) var(--border-subtle);
}
.price-fetch-table tbody tr:last-child td {
  border-bottom: none;
}
.price-fetch-table code {
  display: block;
  max-width: 16rem;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 0.75rem;
}
.price-adjustment {
  max-width: 12rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-base, transparent);
  padding: 0.25rem 0.5rem;
  font-size: 0.75rem;
}
.price-overwrite {
  padding: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
}
@media (max-width: 640px) {
  .price-source-list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
