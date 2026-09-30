<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { catalogPrices } from '@/api/publicCatalog'
import type { PricingModelContract } from '@/api/liveContracts'
import { formatDecimalUSD } from '@/utils/decimal'
import { modelPricingMessages } from '@/i18n/modelPricing'
const props = withDefaults(
  defineProps<{ pricing: PricingModelContract; unit?: 'M' | 'K' }>(),
  { unit: 'M' }
)
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const lines = computed(() => catalogPrices(props.pricing, props.unit))
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
    <p v-if="pricing.billing_mode === 'unconfigured'">
      {{ t('priceUnavailable') }}
    </p>
    <template v-else-if="pricing.billing_mode === 'tiered_expr'"
      ><span>{{ t('expression') }}</span>
      <pre>{{ pricing.billing_expr }}</pre>
    </template>
    <dl v-else>
      <div
        v-for="line in lines"
        :key="line.key"
        :data-detail-cache-read="line.key === 'cacheRead' ? '' : undefined"
        :data-detail-cache-write="line.key === 'cacheWrite' ? '' : undefined"
      >
        <dt>{{ t(labels[line.key] ?? line.key) }}</dt>
        <dd>
          {{ formatDecimalUSD(line.value) }}
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
      <pre v-else>{{ variant.billing_expr }}</pre>
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
