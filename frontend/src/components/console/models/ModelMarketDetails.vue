<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketModel } from '@/types/modelMarket'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import ModelCapabilities from './ModelCapabilities.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'
import ModelStatistics from './ModelStatistics.vue'
import VendorLogo from './VendorLogo.vue'
import CopyModelButton from './CopyModelButton.vue'

const props = defineProps<{ model: MarketModel | null; offerId?: string }>()
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMarketMessages,
})
const selection = ref<{ kind: 'offer' | 'baseline'; id: string } | null>(null)
watch(
  [() => props.model, () => props.offerId],
  ([model, initialOfferId], [previousModel, previousOfferId]) => {
    if (!model) {
      selection.value = null
      return
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
          :size="42"
        />
        <div class="market-detail-title">
          <h2 :id="titleId">{{ model.name }}</h2>
          <p>
            {{
              !model.vendor || model.vendor === '__platform__'
                ? t('platform')
                : model.vendor
            }}
            · {{ t(`types.${model.type}`) }}
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
      <ModelCapabilities :capabilities="model.capabilities" expanded />
      <section class="market-detail-quotes">
        <div class="market-detail-section-heading">
          <h3>{{ t('detailsTitle') }}</h3>
          <p>{{ t('benchmarkHint') }}</p>
        </div>
        <div class="market-detail-quote-list">
          <div class="market-detail-offer-head" aria-hidden="true">
            <span>{{ t('detailChannel') }}</span
            ><span>{{ t('detailPrice') }}</span
            ><span>{{ t('detailStats') }}</span>
          </div>
          <button
            v-for="baseline in model.baselines"
            :key="baseline.id"
            type="button"
            class="market-detail-offer market-detail-reference focus-ring"
            data-market-baseline
            :class="{
              selected:
                selection?.kind === 'baseline' && selection.id === baseline.id,
            }"
            :aria-pressed="
              selection?.kind === 'baseline' && selection.id === baseline.id
            "
            :aria-label="`${t('viewBenchmark')} · ${pricingVariantLabel(baseline, t('defaultVariant'))}`"
            @click="selection = { kind: 'baseline', id: baseline.id }"
          >
            <span class="market-detail-channel"
              ><span class="market-detail-channel-name"
                >{{ t('benchmark') }}
                <span class="market-detail-ratio">1.0×</span></span
              ><span class="market-detail-offer-meta"
                >{{ pricingVariantLabel(baseline, t('defaultVariant')) }} ·
                {{ t(`billingModes.${baseline.billingMode}`) }}</span
              ></span
            >
            <ModelPriceDisplay :pricing="baseline" />
            <span class="market-detail-reference-note">{{
              t('reference')
            }}</span>
          </button>
          <button
            v-for="offer in model.offers"
            :key="offer.id"
            type="button"
            class="market-detail-offer focus-ring"
            :data-offer-id="offer.id"
            :class="{
              selected:
                selection?.kind === 'offer' && selection.id === offer.id,
            }"
            :aria-pressed="
              selection?.kind === 'offer' && selection.id === offer.id
            "
            :aria-label="`${t('openOffer', { name: model.name, channel: offer.channelName })} · ${pricingVariantLabel(offer.pricing, t('defaultVariant'))}`"
            @click="selection = { kind: 'offer', id: offer.id }"
          >
            <span class="market-detail-channel"
              ><span class="market-detail-channel-name"
                >{{ offer.channelName }}
                <span class="market-detail-ratio"
                  >{{ offer.ratio }}×</span
                ></span
              ><span class="market-detail-offer-meta"
                >{{ pricingVariantLabel(offer.pricing, t('defaultVariant')) }} ·
                {{ t(`billingModes.${offer.pricing.billingMode}`) }}</span
              ></span
            >
            <ModelPriceDisplay :pricing="offer.pricing" />
            <span class="market-detail-statistics-note">{{
              t('noStatistics')
            }}</span>
          </button>
          <p v-if="!model.offers.length" class="market-detail-no-offers">
            {{ t('noSupply') }}
          </p>
        </div>
      </section>
      <section v-if="selectedPricing" class="market-detail-price-panel">
        <h3>
          {{ selectedName }} ·
          {{ t(selectedOffer ? 'pricingDetails' : 'pricingReference') }}
        </h3>
        <p class="market-detail-formula">
          {{
            selectedOffer
              ? `${t(selectedPricing.billingMode === 'dynamic' ? 'dynamicFormula' : 'formula')} · ${selectedOffer.ratio}×`
              : t('benchmarkNote')
          }}
        </p>
        <p class="market-detail-variant">
          {{ t('plugin') }} ·
          {{ pricingVariantLabel(selectedPricing, t('defaultVariant')) }}
        </p>
        <ModelPriceDisplay :pricing="selectedPricing" mode="full" />
        <div v-if="selectedEndpoints.length" class="market-detail-endpoints">
          <h4>{{ t('endpoints') }}</h4>
          <code v-for="endpoint in selectedEndpoints" :key="endpoint.type"
            ><span v-if="endpoint.method">{{ endpoint.method }} </span
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
              <dd>{{ rule.multiplier }}×</dd>
            </div>
          </dl>
        </div>
        <details v-if="usageFields.length" class="market-detail-extra-rules">
          <summary class="focus-ring">{{ t('usageFields') }}</summary>
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
        <ModelStatistics v-if="selectedOffer" />
      </section>
      <div class="market-detail-bottom">
        <section>
          <h3>{{ t('modelInfo') }}</h3>
          <p>{{ model.description || t('noDescription') }}</p>
        </section>
        <section class="market-detail-endpoints">
          <h3>{{ t('endpoints') }}</h3>
          <code v-for="endpoint in model.endpoints" :key="endpoint.type"
            ><span v-if="endpoint.method">{{ endpoint.method }} </span
            >{{ endpoint.path || endpoint.type }}</code
          >
          <p v-if="!model.endpointTypes.length">{{ t('unknown') }}</p>
        </section>
      </div>
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
  font: 600 1rem/1.5 var(--font-mono);
  overflow-wrap: anywhere;
}
.market-detail-title p {
  color: var(--text-tertiary);
  font-size: 0.75rem;
  margin-top: 0.25rem;
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
  gap: 1.4rem;
  min-width: 0;
}
.market-detail h3 {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-primary);
}
.market-detail-section-heading {
  margin-bottom: 0.7rem;
}
.market-detail-section-heading p,
.market-detail-formula {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  line-height: 1.8;
  margin-top: 0.35rem;
}
.market-detail-quote-list {
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  overflow: hidden;
}
.market-detail-offer-head,
.market-detail-offer {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(0, 1.2fr) minmax(0, 0.65fr);
  gap: 0.7rem;
  align-items: center;
  padding: 0.85rem 1rem;
}
.market-detail-offer-head {
  background: var(--surface-table-header);
  color: var(--text-tertiary);
  font-size: 0.625rem;
}
.market-detail-offer {
  width: 100%;
  text-align: left;
  border-top: 1px dashed var(--border-subtle);
  transition: background-color 0.15s;
}
.market-detail-offer:hover {
  background: var(--state-hover-layer);
}
.market-detail-offer.selected {
  background: var(--accent-soft);
  box-shadow: inset 2px 0 var(--accent);
}
.market-detail-offer:focus-visible {
  outline-offset: -3px;
}
.market-detail-channel {
  display: block;
  min-width: 0;
}
.market-detail-channel-name {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 0.3rem;
  color: var(--text-primary);
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.market-detail-ratio {
  font: 0.6875rem var(--font-mono);
  color: var(--accent-text);
  white-space: nowrap;
}
.market-detail-offer-meta {
  display: block;
  color: var(--text-tertiary);
  font-size: 0.625rem;
  margin-top: 0.3rem;
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
.market-detail-price-panel {
  padding: 1rem 1.2rem;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
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
  margin: 0.3rem 0.3rem 0.3rem 0;
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  background: var(--surface-muted);
  font: 0.6875rem/1.7 var(--font-mono);
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
  line-height: 1.8;
  margin-top: 0.75rem;
}
.market-detail-rule-notes h4 {
  font-weight: 600;
  margin-top: 0.65rem;
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
.market-detail-bottom {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  gap: 1.2rem;
}
.market-detail-bottom p {
  color: var(--text-secondary);
  font-size: 0.75rem;
  line-height: 1.8;
  margin-top: 0.5rem;
  overflow-wrap: anywhere;
}
.market-detail-routing {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  border-top: 1px dashed var(--border-subtle);
  padding-top: 0.8rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.8;
}
.market-detail-routing svg {
  flex: none;
  margin-top: 0.2rem;
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
  .market-detail-offer-head {
    display: none;
  }
  .market-detail-offer {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.5rem;
    padding: 0.8rem;
  }
  .market-detail-channel {
    display: flex;
    justify-content: space-between;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .market-detail-price-panel {
    padding: 0.8rem;
  }
  .market-detail-bottom {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
