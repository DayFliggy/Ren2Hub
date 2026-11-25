<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PerfBucketPoint } from '@/api/liveContracts'
import { getModelPerformanceDetail } from '@/api/modelMarket'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import type { MarketModel, ModelPerformanceState } from '@/types/modelMarket'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { formatMarketRatio } from '@/utils/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import CatalogHealthBadge from './CatalogHealthBadge.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'
import ModelStatistics from './ModelStatistics.vue'
import VendorLogo from './VendorLogo.vue'
import CopyModelButton from './CopyModelButton.vue'

const props = defineProps<{
  model: MarketModel | null
  offerId?: string
  performance?: ModelPerformanceState
  serverAddress?: string
}>()
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMarketMessages,
})
const toast = useToast()
const descriptionPrefix = useId()
const tabPrefix = useId()
const descriptionId = (kind: string, id: string) =>
  `${descriptionPrefix}-${kind}-${encodeURIComponent(id)}`
const titleElement = ref<HTMLElement | null>(null)
const titleClipped = ref(false)
const pricingHeading = ref<HTMLElement | null>(null)
const selectionAnnouncement = ref('')
const activeTab = ref<'quote' | 'integrate'>('quote')

const detailSeries = ref<PerfBucketPoint[]>([])
const detailSeriesLoading = ref(false)
const detailSeriesRequest = useLatestRequest()

watch(
  [() => props.model?.name ?? null, () => props.performance?.status ?? 'empty'],
  async ([modelName, perfStatus]) => {
    detailSeries.value = []
    detailSeriesLoading.value = false
    if (!modelName || perfStatus !== 'ready') return
    detailSeriesLoading.value = true
    const result = await detailSeriesRequest.run((signal) =>
      getModelPerformanceDetail(modelName, signal)
    )
    if (result.stale) return
    detailSeriesLoading.value = false
    if (result.ok) detailSeries.value = result.value.series
  },
  { immediate: true }
)

watch(
  [titleElement, () => props.model?.name],
  ([title], _previous, onCleanup) => {
    titleClipped.value = false
    selectionAnnouncement.value = ''
    if (!title || !props.model) return
    let active = true
    const measure = () => {
      if (active)
        titleClipped.value = title.scrollHeight > title.clientHeight + 1
    }
    const observer = new ResizeObserver(measure)
    observer.observe(title)
    void nextTick(measure)
    onCleanup(() => {
      active = false
      observer.disconnect()
    })
  },
  { flush: 'post' }
)

async function viewPricingDetails() {
  activeTab.value = 'quote'
  await nextTick()
  pricingHeading.value?.focus({ preventScroll: true })
  pricingHeading.value?.scrollIntoView({ block: 'start', behavior: 'auto' })
}

const quoteRows = computed(() => [
  ...(props.model?.baselines ?? []).map((pricing) => ({
    kind: 'baseline' as const,
    id: pricing.id,
    name: t('benchmark'),
    ratio: '1',
    pricing,
    health: undefined,
  })),
  ...(props.model?.offers ?? []).map((offer) => ({
    kind: 'offer' as const,
    id: offer.id,
    name: offer.channelName,
    ratio: offer.ratio,
    pricing: offer.pricing,
    health: offer.health,
  })),
])

function selectQuote(quote: (typeof quoteRows.value)[number]) {
  selection.value = { kind: quote.kind, id: quote.id }
  selectionAnnouncement.value = t('selectionUpdated', {
    channel: quote.name,
    variant: pricingVariantLabel(quote.pricing, t('defaultVariant')),
  })
}

const selection = ref<{ kind: 'offer' | 'baseline'; id: string } | null>(null)
watch(
  [() => props.model, () => props.offerId],
  ([model, initialOfferId], [previousModel, previousOfferId]) => {
    if (!model) {
      selection.value = null
      return
    }
    if (previousModel?.id !== model.id) {
      activeTab.value = 'quote'
    }
    if (
      previousModel?.id === model.id &&
      initialOfferId === previousOfferId &&
      selection.value
    ) {
      const existing =
        selection.value.kind === 'offer' ? model.offers : model.baselines
      if (existing.some((entry) => entry.id === selection.value?.id)) return
    }
    const offer =
      model.offers.find((item) => item.id === initialOfferId) ?? model.offers[0]
    selection.value = offer
      ? { kind: 'offer', id: offer.id }
      : model.baselines[0]
        ? { kind: 'baseline', id: model.baselines[0].id }
        : null
  },
  { immediate: true }
)

const selectedOffer = computed(() =>
  selection.value?.kind === 'offer'
    ? props.model?.offers.find((offer) => offer.id === selection.value?.id)
    : undefined
)
const selectedPricing = computed(
  () =>
    selectedOffer.value?.pricing ??
    props.model?.baselines.find(
      (baseline) =>
        selection.value?.kind === 'baseline' &&
        baseline.id === selection.value.id
    )
)
const selectedName = computed(
  () => selectedOffer.value?.channelName ?? t('benchmark')
)
const selectedEndpoints = computed(() => {
  const types =
    selectedOffer.value?.endpointTypes ??
    selectedPricing.value?.endpointTypes ??
    []
  return (
    props.model?.endpoints.filter((endpoint) =>
      types.includes(endpoint.type)
    ) ?? []
  )
})
const integrateEndpoints = computed(() =>
  selectedEndpoints.value.length
    ? selectedEndpoints.value
    : (props.model?.endpoints ?? [])
)

const normalizedBaseUrl = computed(() => {
  const raw = props.serverAddress?.trim()
  if (raw) return raw.replace(/\/+$/, '')
  if (typeof window !== 'undefined' && window.location?.origin)
    return window.location.origin.replace(/\/+$/, '')
  return 'https://api.example.com'
})

const curlSnippet = computed(() => {
  const model = props.model
  if (!model) return ''
  const primary = integrateEndpoints.value[0]
  const path = primary?.path || '/v1/chat/completions'
  const url = `${normalizedBaseUrl.value}${path.startsWith('/') ? path : `/${path}`}`
  const modelName = model.name

  if (path.includes('/v1/messages')) {
    return [
      `curl ${url} \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -H "x-api-key: $REN2HUB_API_KEY" \\`,
      `  -H "anthropic-version: 2023-06-01" \\`,
      `  -d '${JSON.stringify({ model: modelName, max_tokens: 1024, messages: [{ role: 'user', content: 'Hello' }] })}'`,
    ].join('\n')
  }
  if (path.includes(':generateContent') || path.includes('/v1beta/models/')) {
    return [
      `curl "${url}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -H "x-goog-api-key: $REN2HUB_API_KEY" \\`,
      `  -d '${JSON.stringify({ contents: [{ parts: [{ text: 'Hello' }] }] })}'`,
    ].join('\n')
  }
  let body: Record<string, unknown>
  if (path.includes('/v1/responses') || path.includes('/v1/embeddings')) {
    body = { model: modelName, input: 'Hello' }
  } else if (path.includes('/v1/images/')) {
    body = {
      model: modelName,
      prompt: 'A minimalist ink sketch of a desert ledger',
    }
  } else if (path.includes('/v1/audio/speech')) {
    body = { model: modelName, input: 'Hello', voice: 'alloy' }
  } else if (path.includes('/rerank')) {
    body = {
      model: modelName,
      query: 'Hello',
      documents: ['Document A', 'Document B'],
    }
  } else {
    body = {
      model: modelName,
      messages: [{ role: 'user', content: 'Hello' }],
    }
  }
  return [
    `curl ${url} \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -H "Authorization: Bearer $REN2HUB_API_KEY" \\`,
    `  -d '${JSON.stringify(body)}'`,
  ].join('\n')
})

async function copyText(text: string, successKey: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(t(successKey))
  } catch {
    toast.error(t('copyFailed'))
  }
}

const requestRules = computed(() => {
  const rules = selectedPricing.value?.billingDetails?.request_rules
  if (!Array.isArray(rules)) return []
  return rules.filter(
    (rule): rule is { condition: string; multiplier: string } =>
      Boolean(rule) &&
      typeof rule === 'object' &&
      typeof rule.condition === 'string' &&
      typeof rule.multiplier === 'string'
  )
})
const usageFields = computed(() =>
  Object.entries(selectedPricing.value?.usageSchema ?? {}).map(
    ([name, field]) => {
      const description = field.description
      const unit = field.unitLabel
      const localized = (value: string | Record<string, string> | undefined) =>
        typeof value === 'string'
          ? value
          : (value?.[locale.value] ??
            value?.[locale.value.split('-')[0]!] ??
            value?.en ??
            '')
      return {
        name,
        description: localized(description),
        unit: localized(unit) || field.unit || '',
        values: field.enum?.join(' · ') ?? '',
      }
    }
  )
)
</script>

<template>
  <ConsoleModal
    :open="model !== null"
    :title="model?.name ?? ''"
    size="xl"
    @close="emit('close')"
  >
    <template #header="{ titleId }">
      <div v-if="model" class="market-detail-header">
        <VendorLogo
          :vendor="
            !model.vendor || model.vendor === '__platform__'
              ? t('platform')
              : model.vendor
          "
          :size="40"
        />
        <div class="market-detail-title">
          <h2 :id="titleId" ref="titleElement">{{ model.name }}</h2>
          <p class="market-detail-subtitle">
            <span>
              {{
                !model.vendor || model.vendor === '__platform__'
                  ? t('platform')
                  : model.vendor
              }}
              · {{ t(`types.${model.type}`) }}
            </span>
            <span
              v-for="tag in model.tags.slice(0, 4)"
              :key="tag"
              class="market-detail-tag"
              >{{ tag }}</span
            >
          </p>
        </div>
        <div class="market-detail-actions">
          <CopyModelButton :name="model.name" class="market-detail-icon" />
          <button
            type="button"
            class="market-detail-icon focus-ring"
            :aria-label="t('close')"
            :title="t('close')"
            @click="emit('close')"
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              aria-hidden="true"
            >
              <path d="m6 6 12 12M18 6 6 18" />
            </svg>
          </button>
        </div>
      </div>
    </template>

    <div v-if="model" class="market-detail" data-market-detail>
      <details v-if="titleClipped" :key="model.id" class="market-full-id">
        <summary class="focus-ring" tabindex="0">
          {{ t('fullModelId') }}
        </summary>
        <code>{{ model.name }}</code>
      </details>

      <div class="market-detail-workbench">
        <aside class="market-detail-quotes">
          <div class="market-detail-section-heading">
            <h3>{{ t('detailsTitle') }}</h3>
          </div>
          <div class="market-detail-quote-list">
            <div
              v-for="quote in quoteRows"
              :key="`${quote.kind}:${quote.id}`"
              class="market-detail-offer"
              :data-market-baseline="quote.kind === 'baseline' ? '' : undefined"
              :class="{
                selected:
                  selection?.kind === quote.kind && selection.id === quote.id,
                'market-detail-reference': quote.kind === 'baseline',
              }"
            >
              <button
                type="button"
                class="market-detail-select"
                :data-offer-id="quote.kind === 'offer' ? quote.id : undefined"
                :aria-pressed="
                  selection?.kind === quote.kind && selection.id === quote.id
                "
                :aria-label="`${quote.kind === 'baseline' ? t('viewBenchmark') : t('openOffer', { name: model.name, channel: quote.name })} · ${pricingVariantLabel(quote.pricing, t('defaultVariant'))}`"
                :aria-describedby="`${descriptionId(quote.kind, quote.id)}-meta ${descriptionId(quote.kind, quote.id)}-price ${descriptionId(quote.kind, quote.id)}-note`"
                @click="selectQuote(quote)"
              />
              <span
                :id="`${descriptionId(quote.kind, quote.id)}-meta`"
                class="market-detail-channel"
              >
                <span class="market-detail-channel-top">
                  <span class="market-detail-channel-name"
                    >{{ quote.name }}
                    <span class="market-detail-ratio">{{
                      formatMarketRatio(quote.ratio)
                    }}</span>
                  </span>
                  <span
                    class="market-detail-selected"
                    :class="{
                      'is-hidden':
                        selection?.kind !== quote.kind ||
                        selection.id !== quote.id,
                    }"
                  >
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      aria-hidden="true"
                    >
                      <path d="m5 12 4 4L19 6" />
                    </svg>
                    {{ t('selected') }}
                  </span>
                </span>
                <span class="market-detail-offer-meta"
                  >{{
                    pricingVariantLabel(quote.pricing, t('defaultVariant'))
                  }}
                  · {{ t(`billingModes.${quote.pricing.billingMode}`) }}</span
                >
              </span>
              <ModelPriceDisplay
                :id="`${descriptionId(quote.kind, quote.id)}-price`"
                :pricing="quote.pricing"
                layout="rows"
              />
              <span
                v-if="quote.kind === 'baseline'"
                :id="`${descriptionId(quote.kind, quote.id)}-note`"
                class="market-detail-reference-note"
              >
                {{ t('reference') }}
              </span>
              <CatalogHealthBadge
                v-else
                :id="`${descriptionId(quote.kind, quote.id)}-note`"
                :health="quote.health"
                compact
                class="market-detail-health-note"
              />
            </div>
            <p v-if="!model.offers.length" class="market-detail-no-offers">
              {{ t('noSupply') }}
            </p>
          </div>
          <div v-if="selectedPricing" class="market-detail-jump">
            <button
              type="button"
              class="focus-ring"
              @click="viewPricingDetails"
            >
              {{ t('viewPricingDetails') }}
            </button>
          </div>
          <p class="sr-only" aria-live="polite" aria-atomic="true">
            {{ selectionAnnouncement }}
          </p>
        </aside>

        <div class="market-detail-inspector">
          <div
            class="market-detail-tabs"
            role="tablist"
            :aria-label="t('detailTabLabel')"
          >
            <button
              :id="`${tabPrefix}-tab-quote`"
              type="button"
              role="tab"
              class="market-detail-tab focus-ring"
              :aria-selected="activeTab === 'quote'"
              :aria-controls="`${tabPrefix}-panel-quote`"
              @click="activeTab = 'quote'"
            >
              {{ t('detailTabs.quote') }}
            </button>
            <button
              :id="`${tabPrefix}-tab-integrate`"
              type="button"
              role="tab"
              class="market-detail-tab focus-ring"
              :aria-selected="activeTab === 'integrate'"
              :aria-controls="`${tabPrefix}-panel-integrate`"
              @click="activeTab = 'integrate'"
            >
              {{ t('detailTabs.integrate') }}
            </button>
          </div>

          <section
            v-if="activeTab === 'quote' && selectedPricing"
            :id="`${tabPrefix}-panel-quote`"
            role="tabpanel"
            :aria-labelledby="`${tabPrefix}-tab-quote`"
            class="market-detail-price-panel"
          >
            <h3 ref="pricingHeading" tabindex="-1">
              {{ selectedName }} ·
              {{ t(selectedOffer ? 'pricingDetails' : 'pricingReference') }}
            </h3>
            <p class="market-detail-formula">
              {{
                selectedOffer
                  ? `${t(selectedPricing.billingMode === 'dynamic' ? 'dynamicFormula' : 'formula')} · ${formatMarketRatio(selectedOffer.ratio)}`
                  : t('benchmarkNote')
              }}
            </p>
            <p
              v-if="selectedPricing.pluginKey || selectedPricing.pluginName"
              class="market-detail-variant"
            >
              {{ t('plugin') }} ·
              {{ pricingVariantLabel(selectedPricing, t('defaultVariant')) }}
            </p>
            <ModelPriceDisplay :pricing="selectedPricing" mode="full" />
            <div
              v-if="selectedEndpoints.length"
              class="market-detail-endpoints"
            >
              <h4>{{ t('endpoints') }}</h4>
              <code v-for="endpoint in selectedEndpoints" :key="endpoint.type"
                ><span v-if="endpoint.method" class="market-endpoint-method">{{
                  endpoint.method
                }}</span
                >{{ endpoint.path || endpoint.type }}</code
              >
            </div>
            <div
              v-if="
                selectedPricing.billingDetails?.image_count ||
                selectedPricing.billingDetails?.audio_text_branches ||
                requestRules.length
              "
              class="market-detail-rule-notes"
            >
              <p v-if="selectedPricing.billingDetails?.image_count">
                {{ t('imageCountRule') }}
              </p>
              <p v-if="selectedPricing.billingDetails?.audio_text_branches">
                {{ t('audioTextRule') }}
              </p>
              <h4 v-if="requestRules.length">{{ t('requestRules') }}</h4>
              <dl v-if="requestRules.length">
                <div v-for="(rule, index) in requestRules" :key="index">
                  <dt>
                    <code>{{ rule.condition }}</code>
                  </dt>
                  <dd>{{ formatMarketRatio(rule.multiplier) }}</dd>
                </div>
              </dl>
            </div>
            <details
              v-if="usageFields.length"
              class="market-detail-extra-rules"
            >
              <summary class="focus-ring" tabindex="0">
                {{ t('usageFields') }}
              </summary>
              <dl>
                <div v-for="field in usageFields" :key="field.name">
                  <dt>
                    <code>{{ field.name }}</code
                    ><span v-if="field.unit"> · {{ field.unit }}</span>
                  </dt>
                  <dd>
                    {{ field.description
                    }}<span v-if="field.values"> · {{ field.values }}</span>
                  </dd>
                </div>
              </dl>
            </details>
            <CatalogHealthBadge
              v-if="selectedOffer"
              :health="selectedOffer.health"
              detail
            />
            <ModelStatistics
              v-if="selectedOffer && performance"
              :performance="performance"
              :series="detailSeries"
              :series-loading="detailSeriesLoading"
            />
          </section>

          <section
            v-else-if="activeTab === 'integrate'"
            :id="`${tabPrefix}-panel-integrate`"
            role="tabpanel"
            :aria-labelledby="`${tabPrefix}-tab-integrate`"
            class="market-detail-integrate-panel"
          >
            <div class="market-integrate-block">
              <div class="market-integrate-head">
                <h4>{{ t('baseUrl') }}</h4>
                <button
                  type="button"
                  class="market-inline-copy focus-ring"
                  @click="copyText(normalizedBaseUrl, 'copiedEndpoint')"
                >
                  {{ t('copyEndpoint') }}
                </button>
              </div>
              <code class="market-integrate-url">{{ normalizedBaseUrl }}</code>
            </div>

            <div class="market-integrate-block">
              <h4>{{ t('endpoints') }}</h4>
              <ul v-if="integrateEndpoints.length" class="market-endpoint-list">
                <li
                  v-for="endpoint in integrateEndpoints"
                  :key="endpoint.type"
                  class="market-endpoint-row"
                >
                  <code
                    ><span
                      v-if="endpoint.method"
                      class="market-endpoint-method"
                      >{{ endpoint.method }}</span
                    >{{ endpoint.path || endpoint.type }}</code
                  >
                  <button
                    type="button"
                    class="market-inline-copy focus-ring"
                    :aria-label="`${t('copyEndpoint')} ${endpoint.path || endpoint.type}`"
                    @click="
                      copyText(endpoint.path || endpoint.type, 'copiedEndpoint')
                    "
                  >
                    {{ t('copyEndpoint') }}
                  </button>
                </li>
              </ul>
              <p v-else class="market-detail-muted">{{ t('unknown') }}</p>
            </div>

            <div class="market-integrate-block">
              <div class="market-integrate-head">
                <h4>{{ t('curlExample') }}</h4>
                <button
                  type="button"
                  class="market-inline-copy focus-ring"
                  @click="copyText(curlSnippet, 'copiedCurl')"
                >
                  {{ t('copyCurl') }}
                </button>
              </div>
              <pre class="market-curl-code"><code>{{ curlSnippet }}</code></pre>
            </div>

            <div class="market-integrate-block">
              <h4>{{ t('modelInfo') }}</h4>
              <p class="market-detail-muted">
                {{ model.description || t('noDescription') }}
              </p>
            </div>
          </section>

          <p class="market-detail-routing">
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="9" />
              <path d="M12 11v6m0-10v.01" /></svg
            >{{ t('supplyNote') }}
          </p>
        </div>
      </div>
    </div>
  </ConsoleModal>
</template>

<style scoped>
.market-detail-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
  text-align: left;
}
.market-detail-title {
  flex: 1;
  min-width: 0;
}
.market-detail-title h2 {
  color: var(--text-primary);
  font: 600 1rem/1.45 var(--font-mono);
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  overflow: hidden;
}
.market-detail-subtitle {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  margin-top: 0.25rem;
}
.market-detail-tag {
  padding: 0.05rem 0.4rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-secondary);
  font-size: 0.65rem;
}
.market-detail-actions {
  display: flex;
  flex: none;
  align-items: center;
  gap: 0.2rem;
}
.market-detail-icon {
  display: grid;
  place-items: center;
  width: 2.5rem;
  height: 2.5rem;
  color: var(--text-secondary);
  border-radius: var(--shape-control);
}
.market-detail-icon:hover {
  background: var(--state-hover-layer);
}
.market-detail-icon svg {
  width: 1rem;
  height: 1rem;
}
.market-detail {
  display: grid;
  gap: 1rem;
  min-width: 0;
}
.market-detail-workbench {
  display: grid;
  grid-template-columns: minmax(15.5rem, 19rem) minmax(0, 1fr);
  gap: 1.1rem;
  align-items: start;
  min-width: 0;
}
.market-detail h3 {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-primary);
}
.market-detail-section-heading {
  margin-bottom: 0.55rem;
}
.market-detail-formula {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  line-height: 1.7;
  margin-top: 0.3rem;
}
.market-detail-quote-list {
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  overflow: hidden;
  background: var(--surface-solid);
}
.market-detail-offer {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.3rem;
  width: 100%;
  text-align: left;
  padding: 0.62rem 0.8rem;
  border-top: 1px dashed var(--border-subtle);
  transition: background-color 0.15s;
}
.market-detail-offer:first-child {
  border-top: 0;
}
.market-detail-offer:hover {
  background: var(--state-hover-layer);
}
.market-detail-offer.selected {
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent-text);
}
.market-detail-select {
  position: absolute;
  inset: 3px;
  z-index: 1;
  cursor: pointer;
  border-radius: var(--shape-control);
}
.market-detail-select:focus-visible,
.market-detail-jump button:focus-visible,
.market-detail-price-panel h3:focus-visible {
  outline: 2px solid var(--text-primary);
  outline-offset: -1px;
}
.market-detail-selected {
  display: inline-flex;
  align-items: center;
  flex: none;
  gap: 0.2rem;
  color: var(--accent-text);
  font-size: 0.6875rem;
  font-weight: 600;
}
.market-detail-selected.is-hidden {
  visibility: hidden;
}
.market-detail-jump {
  display: flex;
  justify-content: flex-end;
  margin-top: 0.3rem;
}
.market-detail-jump button {
  min-height: 44px;
  padding: 0.4rem 0.5rem;
  font-size: 0.75rem;
  color: var(--accent-text);
}
.market-full-id {
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-full-id summary {
  cursor: pointer;
}
.market-full-id code {
  display: block;
  margin-top: 0.45rem;
  font-family: var(--font-mono);
  overflow-wrap: anywhere;
}
.market-detail-channel {
  display: block;
  min-width: 0;
}
.market-detail-channel-top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.4rem;
}
.market-detail-channel-name {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 0.3rem;
  color: var(--text-primary);
  font-size: 0.8125rem;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.market-detail-ratio {
  font: 0.6875rem var(--font-mono);
  color: var(--accent-text);
  white-space: nowrap;
}
.market-detail-health-note {
  margin-top: 0.35rem;
  position: relative;
  pointer-events: none;
}
.market-detail-offer-meta {
  display: block;
  color: var(--text-tertiary);
  font-size: 0.625rem;
  margin-top: 0.2rem;
  overflow-wrap: anywhere;
}
.market-detail-reference-note,
.market-detail-statistics-note {
  color: var(--text-tertiary);
  font-size: 0.6875rem;
}
.market-detail-reference-note {
  color: var(--accent-text);
}
.market-detail-no-offers {
  padding: 1rem;
  color: var(--text-tertiary);
  font-size: 0.8125rem;
}
.market-detail-inspector {
  display: grid;
  gap: 0.85rem;
  min-width: 0;
}
.market-detail-tabs {
  display: inline-flex;
  gap: 0.25rem;
  padding: 0.22rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  justify-self: start;
}
.market-detail-tab {
  padding: 0.38rem 0.9rem;
  border-radius: calc(var(--shape-control) - 2px);
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-weight: 500;
  cursor: pointer;
  transition:
    background-color 0.12s,
    color 0.12s;
}
.market-detail-tab:hover {
  color: var(--text-primary);
}
.market-detail-tab[aria-selected='true'] {
  background: var(--surface-solid);
  color: var(--text-primary);
  font-weight: 600;
  box-shadow: var(--card-shadow);
}
.market-detail-price-panel,
.market-detail-integrate-panel {
  padding: 1rem 1.15rem;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
  min-width: 0;
}
.market-detail-integrate-panel {
  display: grid;
  gap: 1rem;
}
.market-integrate-block {
  display: grid;
  gap: 0.4rem;
  min-width: 0;
}
.market-integrate-block h4 {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--text-secondary);
}
.market-integrate-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.market-inline-copy {
  padding: 0.2rem 0.5rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-secondary);
  font-size: 0.6875rem;
  cursor: pointer;
  flex: none;
}
.market-inline-copy:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.market-integrate-url {
  display: block;
  padding: 0.5rem 0.65rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-primary);
  font: 0.75rem/1.5 var(--font-mono);
  overflow-wrap: anywhere;
}
.market-endpoint-list {
  display: grid;
  gap: 0.4rem;
}
.market-endpoint-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  padding: 0.45rem 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  min-width: 0;
}
.market-endpoint-row code {
  min-width: 0;
  overflow-wrap: anywhere;
  font: 0.75rem/1.5 var(--font-mono);
  color: var(--text-primary);
}
.market-endpoint-method {
  color: var(--accent-text);
  font-weight: 600;
  margin-right: 0.35rem;
}
.market-curl-code {
  margin: 0;
  padding: 0.75rem 0.85rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-primary);
  font: 0.75rem/1.65 var(--font-mono);
  overflow-x: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.market-detail-muted {
  color: var(--text-secondary);
  font-size: 0.75rem;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.market-detail-variant {
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-top: 0.5rem;
}
.market-detail-endpoints {
  min-width: 0;
}
.market-detail-endpoints h4 {
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin: 0.7rem 0 0.3rem;
}
.market-detail-endpoints code {
  display: inline-block;
  max-width: 100%;
  overflow-wrap: anywhere;
  padding: 0.15rem 0.4rem;
  margin: 0.25rem 0.3rem 0.25rem 0;
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  background: var(--surface-muted);
  font: 0.6875rem/1.65 var(--font-mono);
}
.market-detail-extra-rules {
  margin-top: 0.7rem;
  padding: 0.6rem 0;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-detail-extra-rules summary {
  cursor: pointer;
}
.market-detail-extra-rules dl {
  display: grid;
  gap: 0.6rem;
  margin-top: 0.6rem;
}
.market-detail-extra-rules dt {
  color: var(--text-primary);
}
.market-detail-extra-rules dd {
  margin-top: 0.2rem;
  color: var(--text-tertiary);
  overflow-wrap: anywhere;
}
.market-detail-rule-notes {
  color: var(--text-secondary);
  font-size: 0.75rem;
  line-height: 1.75;
  margin-top: 0.75rem;
}
.market-detail-rule-notes h4 {
  font-weight: 600;
  margin-top: 0.6rem;
}
.market-detail-rule-notes dl > div {
  display: flex;
  justify-content: space-between;
  gap: 0.8rem;
  padding: 0.35rem 0;
  border-bottom: 1px dashed var(--border-subtle);
}
.market-detail-rule-notes dt {
  min-width: 0;
  overflow-wrap: anywhere;
}
.market-detail-rule-notes dd {
  flex: none;
  font-family: var(--font-mono);
}
.market-detail-rule-notes code {
  font-size: 0.6875rem;
}
.market-detail-routing {
  display: flex;
  align-items: flex-start;
  gap: 0.45rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.65;
}
.market-detail-routing svg {
  flex: none;
  margin-top: 0.18rem;
}
html.dark .market-detail-offer {
  border-top-style: solid;
}
html.dark .market-detail-rule-notes dl > div {
  border-bottom-style: solid;
}
@media (max-width: 780px) {
  .market-detail-workbench {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 650px) {
  .market-detail-header {
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .market-detail-header > :first-child {
    display: none;
  }
  .market-detail-title h2 {
    font-size: 0.875rem;
  }
  .market-detail-actions {
    align-self: flex-start;
  }
  .market-detail-icon {
    width: 2.75rem;
    height: 2.75rem;
  }
  .market-detail-offer {
    padding: 0.75rem;
  }
  .market-detail-price-panel,
  .market-detail-integrate-panel {
    padding: 0.85rem;
  }
}
</style>
