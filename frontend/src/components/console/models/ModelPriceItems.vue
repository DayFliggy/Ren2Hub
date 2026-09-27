<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PriceVersion } from '@/api/modelPricing'
import { formatTokenPrice } from '@/utils/format'
import { formatDecimalUSD } from '@/utils/decimal'

defineProps<{ pricing: PriceVersion; versions?: PriceVersion[] }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-3 text-xs" data-model-charge-items>
    <section
      v-for="version in versions ?? [pricing]"
      :key="version.id"
      class="space-y-3"
    >
      <p class="text-[var(--text-tertiary)]" data-model-price-scope>
        {{ t('billing.priceScope') }}: {{ version.scope_kind ?? 'global_model'
        }}<span v-if="version.channel_id"> · {{ version.channel_id }}</span> ·
        v{{ version.id }}
      </p>
      <p v-if="version.mode === 'free'">{{ t('models.billing.free') }}</p>
      <details v-if="version.mode === 'expression'">
        <summary class="cursor-pointer focus-ring">
          {{ t('billing.expression') }}
        </summary>
        <pre class="mt-2 whitespace-pre-wrap break-all font-mono">{{
          version.expression
        }}</pre>
      </details>
      <dl class="space-y-2">
        <div
          v-if="version.flat_fee && version.flat_fee !== '0'"
          class="flex justify-between gap-3"
        >
          <dt>{{ t('billing.flatFee') }}</dt>
          <dd class="font-mono">{{ formatDecimalUSD(version.flat_fee) }}</dd>
        </div>
        <div
          v-for="line in version.prices"
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
            <template v-if="line.tiers?.length">
              <span class="block text-[var(--text-tertiary)]">{{
                t(`billing.tierModes.${line.tier_mode}`)
              }}</span>
              <span
                v-for="(tier, index) in line.tiers"
                :key="index"
                class="block font-mono"
                >{{ tier.up_to ? `≤${tier.up_to}` : '∞' }}:
                {{ formatTokenPrice(tier.unit_price) }}</span
              >
            </template>
            <span v-else class="font-mono">{{
              formatTokenPrice(line.price)
            }}</span>
            <span class="ml-1 text-[var(--text-tertiary)]">{{
              t(`billing.units.${line.unit}`)
            }}</span>
          </dd>
        </div>
      </dl>
    </section>
  </div>
</template>
