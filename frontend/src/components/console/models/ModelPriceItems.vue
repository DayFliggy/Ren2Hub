<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PricingOffer } from '@/api/liveContracts'
import { formatTokenPrice } from '@/utils/format'
import { formatDecimal, formatDecimalUSD } from '@/utils/decimal'

defineProps<{ offers: PricingOffer[] }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-3 text-xs" data-model-charge-items>
    <section
      v-for="(offer, offerIndex) in offers"
      :key="offer.offer_id"
      class="space-y-3"
    >
      <p class="text-[var(--text-tertiary)]" data-model-price-scope>
        {{ t('billing.priceScope') }}:
        {{ offer.price_version.scope_kind ?? 'global_model' }} · v{{
          offer.price_version.id
        }}
      </p>
      <p class="break-all" data-model-offer>
        {{ t('billing.offerLabel', { index: offerIndex + 1 }) }} ·
        {{ t('billing.pricedModel') }}: {{ offer.priced_model }}
      </p>
      <p
        v-for="adjustment in offer.adjustments"
        :key="adjustment.kind"
        class="text-[var(--text-secondary)]"
        data-model-adjustment
      >
        {{ t('billing.channelAdjustment') }}: ×
        {{ formatDecimal(adjustment.multiplier) }}
      </p>
      <p v-if="offer.conditions.service_tiers?.length" class="break-all">
        {{ t('billing.supportedServiceTiers') }}:
        {{ offer.conditions.service_tiers.join(', ') }}
      </p>
      <p
        v-if="offer.availability.status !== 'available'"
        class="text-[var(--status-warning-text)]"
      >
        {{ t('billing.priceUnavailable') }}: {{ offer.availability.reason }}
      </p>
      <p v-if="offer.price_version.mode === 'free'">
        {{ t('models.billing.free') }}
      </p>
      <p v-if="offer.price_version.metering" class="break-all">
        {{ t('billing.metering') }}:
        {{
          t(`billing.meteringMethods.${offer.price_version.metering.method}`)
        }}
      </p>
      <details v-if="offer.price_version.prices.length" class="space-y-2">
        <summary class="cursor-pointer focus-ring">
          {{ t('billing.basePrices') }}
        </summary>
        <dl class="space-y-2">
          <div v-for="line in offer.price_version.prices" :key="line.quantity">
            <dt>{{ line.quantity }} / {{ line.unit }}</dt>
            <dd class="font-mono">
              {{
                line.tiers
                  ? line.tiers
                      .map(
                        (tier) =>
                          `${tier.up_to ?? '∞'}: ${formatDecimalUSD(tier.unit_price)}`
                      )
                      .join(' / ')
                  : formatDecimalUSD(line.price)
              }}
            </dd>
          </div>
        </dl>
      </details>
      <details v-if="offer.price_version.mode === 'expression'">
        <summary class="cursor-pointer focus-ring">
          {{ t('billing.expression') }}
        </summary>
        <pre class="mt-2 whitespace-pre-wrap break-all font-mono">{{
          offer.price_version.expression
        }}</pre>
      </details>
      <dl class="space-y-2">
        <div
          v-if="offer.display_flat_fee && offer.display_flat_fee !== '0'"
          class="flex justify-between gap-3"
        >
          <dt>{{ t('billing.flatFee') }}</dt>
          <dd class="font-mono">
            {{ formatDecimalUSD(offer.display_flat_fee) }}
          </dd>
        </div>
        <div
          v-for="line in offer.display_prices"
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
