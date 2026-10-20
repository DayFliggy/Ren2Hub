<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ReferencePrice } from '@/types/pricingReference'
import { formatUnitPriceUSD, shiftDecimal } from '@/utils/decimal'
import { pricingReferenceMessages } from '@/i18n/pricingReference'
withDefaults(
  defineProps<{
    prices: ReferencePrice[]
    unit?: 'M' | 'K'
    details?: boolean
  }>(),
  { unit: 'M', details: false }
)
const { t, te } = useI18n({
  useScope: 'local',
  messages: pricingReferenceMessages,
})
</script>
<template>
  <div class="reference-prices" data-reference-prices>
    <p v-if="!prices.length">{{ t('unavailable') }}</p>
    <div
      v-for="price in prices"
      :key="`${price.key}:${price.unit}`"
      class="reference-price"
      :data-price-dimension="price.key"
    >
      <p
        v-if="price.source !== 'configured' || price.pluginName"
        class="reference-label"
      >
        {{ t(price.source)
        }}<span v-if="price.pluginName"> · {{ price.pluginName }}</span
        ><span v-if="price.label"> · {{ price.label }}</span>
      </p>
      <div class="reference-amount">
        <span>{{
          te(`labels.${price.key}`) ? t(`labels.${price.key}`) : price.key
        }}</span
        ><strong>{{
          formatUnitPriceUSD(
            unit === 'K' && price.unit === 'per_million_tokens'
              ? shiftDecimal(price.amount, 3)
              : price.amount
          )
        }}</strong
        ><span>{{
          t(
            `units.${unit === 'K' && price.unit === 'per_million_tokens' ? 'per_thousand_tokens' : price.unit}`
          )
        }}</span>
      </div>
      <p v-if="price.condition" class="reference-condition">
        {{ t('condition') }}: {{ price.condition }}
      </p>
      <p v-if="price.requestRules" class="reference-condition">
        {{ t('rules') }}<span v-if="details">: {{ price.requestRules }}</span>
      </p>
      <dl
        v-if="details && price.facts"
        class="reference-facts"
        :aria-label="t('facts')"
      >
        <div v-for="(value, key) in price.facts" :key="key">
          <dt>{{ key }}</dt>
          <dd>{{ value }}</dd>
        </div>
      </dl>
    </div>
  </div>
</template>
<style scoped>
.reference-prices {
  min-width: 0;
  font-size: 0.8rem;
}
.reference-price + .reference-price {
  margin-top: 0.5rem;
}
.reference-label {
  color: var(--accent-text);
  overflow-wrap: anywhere;
}
.reference-amount {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
  align-items: baseline;
  font-variant-numeric: tabular-nums;
}
.reference-amount strong {
  min-width: 0;
  overflow-wrap: anywhere;
  margin-left: auto;
}
.reference-condition {
  color: var(--text-secondary);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.reference-facts {
  overflow-wrap: anywhere;
  margin-top: 0.5rem;
}
.reference-facts > div {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  justify-content: space-between;
}
</style>
