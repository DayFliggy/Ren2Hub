<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  publicPricingReferences,
  expressionReferencePrices,
} from '@/features/pricing/reference'
import ReferencePrices from './ReferencePrices.vue'
import { catalogPrices } from '@/api/publicCatalog'
import type { PricingModelContract } from '@/api/liveContracts'
import { formatUnitPriceUSD } from '@/utils/decimal'
import { modelPricingMessages } from '@/i18n/modelPricing'
const props = withDefaults(
  defineProps<{ pricing: PricingModelContract; unit?: 'M' | 'K' }>(),
  { unit: 'M' }
)
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const lines = computed(() => catalogPrices(props.pricing, props.unit))
const references = computed(() => publicPricingReferences(props.pricing))
const referencePlugin = computed(
  () =>
    props.pricing.billing_plugin_variants?.find(
      (variant) =>
        variant.billing_mode === 'tiered_expr' && variant.billing_expr
    )?.plugin_name || props.pricing.usage_reference?.plugin_name
)
const labels: Record<string, string> = {
  input: 'input',
  output: 'output',
  cacheRead: 'cache',
  cacheWrite: 'cacheWrite',
  image: 'image',
  audioInput: 'audio',
  audioOutput: 'audioOutput',
  request: 'fixed',
}
</script>
<template>
  <div class="model-price-items">
    <p v-if="!references.length && referencePlugin">{{ referencePlugin }}</p>
    <ReferencePrices
      v-if="
        pricing.billing_mode === 'tiered_expr' ||
        pricing.billing_plugin_variants?.length
      "
      :prices="references"
      :unit="unit"
      :details="true"
    />
    <p v-if="pricing.billing_mode === 'unconfigured'">
      {{ t('priceUnavailable') }}
    </p>
    <template v-else-if="pricing.billing_mode === 'tiered_expr'"
      ><span>{{ t('expression') }}</span>
      <pre>{{ pricing.billing_expr }}</pre>
    </template>
    <dl
      v-else-if="
        !pricing.billing_plugin_variants?.length || !pricing.pricing_values
      "
    >
      <div
        v-for="line in lines"
        :key="line.key"
        :data-detail-cache-read="line.key === 'cacheRead' ? '' : undefined"
        :data-detail-cache-write="line.key === 'cacheWrite' ? '' : undefined"
      >
        <dt>{{ t(labels[line.key] ?? line.key) }}</dt>
        <dd>
          {{ formatUnitPriceUSD(line.value) }}
          {{
            line.unit === 'per_call'
              ? t('perRequest')
              : unit === 'K'
                ? '/ 1K tokens'
                : t('perMillion')
          }}
        </dd>
      </div>
    </dl>
    <details
      v-for="variant in pricing.billing_plugin_variants ?? []"
      :key="variant.plugin_key"
    >
      <summary>{{ variant.plugin_name }}</summary>
      <p v-if="variant.billing_mode === 'unconfigured'">
        {{ t('priceUnavailable') }}
      </p>
      <p v-else-if="variant.billing_mode === 'ratio'">
        {{ t('inheritsModelPrice') }}
      </p>
      <template v-else
        ><ReferencePrices
          :prices="
            expressionReferencePrices(
              variant.billing_expr,
              true,
              variant.usage_reference,
              '1',
              variant.plugin_name
            )
          "
          :unit="unit"
          :details="true"
        />
        <pre>{{ variant.billing_expr }}</pre>
      </template>
    </details>
  </div>
</template>
<style scoped>
.model-price-items {
  min-width: 0;
  font-size: 0.8rem;
}
dl {
  display: grid;
  gap: 0.5rem;
}
dl div {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 0.5rem;
}
dt {
  color: var(--text-secondary);
}
dd {
  font-variant-numeric: tabular-nums;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin-top: 0.5rem;
  font-family: monospace;
  font-size: 0.75rem;
}
</style>
