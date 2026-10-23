<script setup lang="ts">
import { computed } from 'vue'
import ReferencePrices from './ReferencePrices.vue'
import { useI18n } from 'vue-i18n'
import type { MarketPricing, MarketPriceLine } from '@/types/modelMarket'
import { formatUnitPriceUSD } from '@/utils/decimal'
import { isAudioEndpoint } from '@/utils/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'

const props = withDefaults(
  defineProps<{
    pricing?: MarketPricing
    mode?: 'summary' | 'comparison' | 'full'
    layout?: 'inline' | 'rows'
  }>(),
  { mode: 'summary', layout: 'inline', pricing: undefined }
)
const { t, te } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const summaryKeys = computed(() => {
  const endpoints = props.pricing?.endpointTypes ?? []
  const audioOnly = endpoints.length > 0 && endpoints.every(isAudioEndpoint)
  return audioOnly
    ? ['audioInput', 'audioOutput', 'request']
    : ['input', 'output', 'request']
})
const primary = computed(
  () =>
    props.pricing?.priceLines.filter((line) =>
      summaryKeys.value.includes(line.key)
    ) ?? []
)
const extra = computed(
  () =>
    props.pricing?.priceLines.filter(
      (line) => !summaryKeys.value.includes(line.key)
    ) ?? []
)
const lines = computed(() =>
  props.mode === 'full' || !primary.value.length
    ? (props.pricing?.priceLines ?? [])
    : primary.value
)
function label(line: MarketPriceLine) {
  const key =
    line.unit === 'per_image'
      ? 'imageRequest'
      : line.key === 'cacheWrite' &&
          props.pricing?.cacheWriteMode === 'claude_ttl'
        ? 'cacheWrite5m'
        : line.key
  return te(`priceLabels.${key}`) ? t(`priceLabels.${key}`) : line.key
}
</script>

<template>
  <div
    class="market-price"
    :class="[`market-price--${mode}`, `market-price--${layout}`]"
    data-market-prices
  >
    <span
      v-if="!pricing || pricing.billingMode === 'unconfigured'"
      class="market-price-empty"
      >{{
        pricing?.priceReason && te(`priceReasons.${pricing.priceReason}`)
          ? t(`priceReasons.${pricing.priceReason}`)
          : t('priceStates.unavailable')
      }}</span
    >
    <template v-else-if="pricing.billingMode === 'dynamic'">
      <ReferencePrices
        :prices="pricing.referencePrices ?? []"
        :details="mode === 'full'"
      />
      <pre v-if="mode === 'full'" class="market-expression">{{
        pricing.expression
      }}</pre>
      <p v-else class="market-price-hint">{{ t('expressionHint') }}</p>
    </template>
    <template v-else>
      <span v-if="pricing.priceStatus" class="market-price-hint">{{
        t(`priceStates.${pricing.priceStatus}`)
      }}</span>
      <dl class="market-price-lines">
        <div
          v-for="line in lines"
          :key="line.key"
          :data-price-dimension="line.key"
        >
          <dt>{{ label(line) }}</dt>
          <dd>
            <strong>{{ formatUnitPriceUSD(line.amount) }}</strong
            ><span>{{ t(`units.${line.unit}`) }}</span>
          </dd>
        </div>
      </dl>
      <dl
        v-if="mode === 'comparison' && primary.length && extra.length"
        class="market-price-extra"
      >
        <div
          v-for="line in extra"
          :key="line.key"
          :data-price-dimension="line.key"
        >
          <dt>{{ label(line) }}</dt>
          <dd>
            <strong>{{ formatUnitPriceUSD(line.amount) }}</strong>
            <span>{{ t(`units.${line.unit}`) }}</span>
          </dd>
        </div>
      </dl>
    </template>
  </div>
</template>

<style scoped>
.market-price {
  min-width: 0;
  color: var(--text-primary);
}
.market-price-lines {
  display: flex;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 0.4rem 1rem;
}
.market-price-lines > div {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.4rem;
  min-width: 0;
}
.market-price dt {
  color: var(--text-secondary);
  font-size: 0.6875rem;
}
.market-price dd {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 0.25rem;
  min-width: 0;
}
.market-price dd strong {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 0.9375rem;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.market-price dd span {
  color: var(--text-tertiary);
  font-size: 0.625rem;
}
.market-price--summary.market-price--rows .market-price-lines {
  display: grid;
  gap: 0.3rem;
}
.market-price--comparison .market-price-lines {
  display: grid;
  gap: 0.3rem;
}
.market-price--comparison .market-price-lines > div {
  justify-content: space-between;
  gap: 0.2rem 0.5rem;
}
.market-price--comparison dd strong {
  font-size: 0.8125rem;
}
.market-price-extra {
  display: grid;
  gap: 0.35rem;
  padding-top: 0.55rem;
  margin-top: 0.6rem;
  border-top: 1px dashed var(--border-subtle);
}
.market-price-extra > div {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.market-price-extra dd strong {
  font-size: 0.6875rem;
  font-weight: 400;
}
.market-price--full .market-price-lines {
  display: grid;
  gap: 0;
}
.market-price--full .market-price-lines > div {
  display: flex;
  justify-content: space-between;
  padding: 0.75rem 0;
  border-bottom: 1px dashed var(--border-subtle);
}
.market-price--full .market-price-lines > div:last-child {
  border-bottom: 0;
}
.market-price--full dt {
  font-size: 0.8125rem;
}
.market-price--full dd strong {
  font-size: 0.9375rem;
}
.market-price--full dd span {
  font-size: 0.75rem;
}
.market-price-empty,
.market-expression-label {
  font-size: 0.8125rem;
  color: var(--text-secondary);
}
.market-expression-label {
  color: var(--accent-text);
}
.market-price-hint {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  line-height: 1.6;
  margin-top: 0.2rem;
}
.market-expression {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  padding: 1rem;
  margin-top: 0.75rem;
  background: var(--surface-muted);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  font: 0.75rem/1.8 var(--font-mono);
}
</style>
