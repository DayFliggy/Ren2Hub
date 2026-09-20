<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PriceVersion } from '@/api/modelPricing'
import { formatTokenPrice } from '@/utils/format'

defineProps<{ pricing: PriceVersion }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-3 text-xs" data-model-charge-items>
    <p v-if="pricing.mode === 'free'">{{ t('models.billing.free') }}</p>
    <details v-if="pricing.mode === 'expression'">
      <summary class="cursor-pointer focus-ring">
        {{ t('billing.expression') }}
      </summary>
      <pre class="mt-2 whitespace-pre-wrap break-all font-mono">{{
        pricing.expression
      }}</pre>
    </details>
    <dl class="space-y-2">
      <div
        v-for="line in pricing.prices"
        :key="line.quantity"
        :data-detail-cache-read="line.quantity === 'cr' ? '' : undefined"
        :data-detail-cache-write="line.quantity === 'cc' ? '' : undefined"
        class="flex flex-wrap justify-between gap-x-3 gap-y-1"
      >
        <dt class="break-all text-[var(--text-secondary)]">
          {{
            line.quantity.startsWith('tool:')
              ? line.quantity
              : t(`billing.quantities.${line.quantity}`)
          }}
        </dt>
        <dd class="break-all text-right">
          <span class="font-mono">{{ formatTokenPrice(line.price) }}</span>
          <span class="ml-1 text-[var(--text-tertiary)]">{{
            t(`billing.units.${line.unit}`)
          }}</span>
        </dd>
      </div>
    </dl>
  </div>
</template>
