<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketModel } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import ModelIdentity from './ModelIdentity.vue'
import ModelCapabilities from './ModelCapabilities.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'
import ModelStatistics from './ModelStatistics.vue'

const props = defineProps<{ model: MarketModel }>()
const emit = defineEmits<{ detail: [modelId: string, offerId?: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const representative = computed(() => props.model.offers[0])
const reference = computed(
  () => representative.value?.baseline ?? props.model.baselines[0]
)
const channelCount = computed(
  () => new Set(props.model.offers.map((offer) => offer.channelId)).size
)
</script>

<template>
  <article
    class="market-model-card"
    data-model-card
    :data-model-name="model.name"
  >
    <button
      class="market-card-hit focus-ring"
      type="button"
      :aria-label="t('openDetail', { name: model.name })"
      @click="emit('detail', model.id, representative?.id)"
    />
    <div class="market-card-heading">
      <ModelIdentity
        :name="model.name"
        :vendor="model.vendor"
        :type-label="t(`types.${model.type}`)"
      />
    </div>
    <div class="market-card-capabilities">
      <ModelCapabilities :capabilities="model.capabilities" />
    </div>
    <div class="market-card-quotes">
      <div
        class="market-quote-lane market-benchmark"
        data-platform-reference
        data-market-baseline
      >
        <div class="market-quote-heading">
          <span
            >{{ t('benchmark') }} <span class="market-ratio">1.0×</span></span
          >
          <span class="market-reference-label">{{ t('reference') }}</span>
        </div>
        <p
          v-if="
            reference && (model.baselines.length > 1 || reference.pluginName)
          "
          class="market-variant-label"
        >
          {{ pricingVariantLabel(reference, t('defaultVariant')) }}
        </p>
        <ModelPriceDisplay :pricing="reference" />
      </div>
      <div v-if="representative" class="market-quote-lane" data-channel-quote>
        <div class="market-quote-heading">
          <span class="market-channel-name"
            >{{ representative.channelName }}
            <span class="market-ratio">{{ representative.ratio }}×</span></span
          >
          <span class="market-billing-label">{{
            t(`billingModes.${representative.pricing.billingMode}`)
          }}</span>
        </div>
        <p
          v-if="model.baselines.length > 1 || representative.pricing.pluginName"
          class="market-variant-label"
        >
          {{ pricingVariantLabel(representative.pricing, t('defaultVariant')) }}
        </p>
        <ModelPriceDisplay :pricing="representative.pricing" />
        <ModelStatistics compact />
      </div>
      <div v-else class="market-quote-lane market-no-offers">
        {{ t('noSupply') }}
      </div>
    </div>
    <div class="market-card-footer">
      <span>{{ t('offerCount', { count: channelCount }) }}</span>
      <span class="market-card-details"
        >{{ t('allQuotes')
        }}<svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"
        >
          <path d="M5 12h14m-5-5 5 5-5 5" /></svg
      ></span>
    </div>
  </article>
</template>

<style scoped>
.market-model-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  overflow: hidden;
  box-shadow: var(--card-shadow);
  transition:
    border-color 0.15s,
    box-shadow 0.15s,
    transform 0.15s;
}
.market-model-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--card-shadow-hover);
  transform: translateY(-2px);
}
.market-card-hit {
  position: absolute;
  inset: 0;
  z-index: 1;
  width: 100%;
  height: 100%;
  border-radius: inherit;
}
.market-card-hit:focus-visible {
  outline-offset: -3px;
}
.market-card-heading {
  padding: 1.1rem 1rem 0;
}
.market-card-capabilities {
  padding: 0.7rem 1rem 0.8rem;
}
.market-card-quotes {
  padding: 0 1rem;
  flex: 1;
}
.market-quote-lane {
  border-top: 1px dashed var(--border-default);
  padding: 0.7rem 0;
  min-width: 0;
}
.market-benchmark {
  padding: 0.6rem 0;
}
.market-quote-heading {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.35rem 0.6rem;
  flex-wrap: wrap;
  margin-bottom: 0.45rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-quote-heading > span:first-child {
  min-width: 0;
  overflow-wrap: anywhere;
}
.market-ratio {
  display: inline-block;
  padding: 0.05rem 0.35rem;
  margin-left: 0.2rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  font: 0.6875rem/1.5 var(--font-mono);
  color: var(--accent-text);
  background: var(--accent-soft);
}
.market-reference-label,
.market-billing-label {
  color: var(--text-tertiary);
  font-size: 0.625rem;
}
.market-variant-label {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  margin-bottom: 0.3rem;
}
.market-no-offers {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  padding-block: 1rem;
}
.market-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.7rem 1rem;
  margin-top: 0.2rem;
  background: var(--surface-muted);
  border-top: 1px dashed var(--border-subtle);
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.market-card-details {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-secondary);
}
.market-card-heading,
.market-card-capabilities,
.market-card-quotes,
.market-card-footer {
  pointer-events: none;
}
.market-card-heading :deep(.market-copy) {
  pointer-events: auto;
}
@media (prefers-reduced-motion: reduce) {
  .market-model-card {
    transition: none;
  }
  .market-model-card:hover {
    transform: none;
  }
}
</style>
