<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketChannelShelf } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { formatMarketRatio } from '@/utils/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import ModelIdentity from './ModelIdentity.vue'
import ModelCapabilities from './ModelCapabilities.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'
import ModelStatistics from './ModelStatistics.vue'

defineProps<{ shelves: MarketChannelShelf[] }>()
const emit = defineEmits<{ detail: [modelId: string, offerId?: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const shelvesId = useId()
function offerDomId(kind: 'meta' | 'price', offerId: string) {
  return `${shelvesId}-${kind}-${encodeURIComponent(offerId)}`
}
</script>

<template>
  <div class="market-shelves" data-market-shelves>
    <section
      v-for="shelf in shelves"
      :key="shelf.channel.id"
      class="market-shelf"
      :aria-label="shelf.channel.name"
    >
      <header class="market-shelf-header">
        <div class="market-shelf-identity">
          <span class="market-channel-symbol" aria-hidden="true">{{
            shelf.channel.name.slice(0, 2)
          }}</span>
          <div>
            <h2 class="display-title">
              {{ shelf.channel.name }}
              <span
                :id="`${shelvesId}-ratio-${shelf.channel.id}`"
                class="market-shelf-ratio"
                >{{ formatMarketRatio(shelf.channel.ratio) }}</span
              >
            </h2>
            <p>
              {{
                t('resultCount', {
                  models: shelf.items.length,
                  offers: shelf.items.reduce(
                    (count, item) => count + item.offers.length,
                    0
                  ),
                })
              }}
            </p>
          </div>
        </div>
        <div class="market-shelf-statistics"><ModelStatistics compact /></div>
      </header>
      <div class="market-shelf-items">
        <article
          v-for="item in shelf.items"
          :key="item.model.id"
          class="market-shelf-item"
          :data-model-name="item.model.name"
          data-model-card
        >
          <ModelIdentity
            :name="item.model.name"
            :vendor="item.model.vendor"
            :type-label="t(`types.${item.model.type}`)"
            compact
            clickable
            @detail="emit('detail', item.model.id, item.offers[0]?.id)"
          />
          <ModelCapabilities
            :capabilities="item.model.capabilities"
            class="market-shelf-capabilities"
          />
          <div class="market-shelf-offers">
            <div
              v-for="offer in item.offers"
              :key="offer.id"
              class="market-shelf-offer"
            >
              <button
                type="button"
                class="market-shelf-offer-hit focus-ring"
                :data-offer-id="offer.id"
                :aria-label="
                  t('openOffer', {
                    name: item.model.name,
                    channel: shelf.channel.name,
                  })
                "
                :aria-describedby="`${shelvesId}-ratio-${shelf.channel.id} ${offerDomId('meta', offer.id)} ${offerDomId('price', offer.id)}`"
                @click="emit('detail', item.model.id, offer.id)"
              />
              <div
                :id="offerDomId('meta', offer.id)"
                class="market-shelf-offer-heading"
              >
                <span>{{
                  pricingVariantLabel(offer.pricing, t('defaultVariant'))
                }}</span>
                <span>{{
                  t(`billingModes.${offer.pricing.billingMode}`)
                }}</span>
              </div>
              <ModelPriceDisplay
                :id="offerDomId('price', offer.id)"
                :pricing="offer.pricing"
              />
            </div>
            <ModelStatistics compact class="market-shelf-item-stats" />
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.market-shelves {
  display: grid;
  gap: 1.4rem;
}
.market-shelf {
  min-width: 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  background: var(--surface-solid);
  overflow: hidden;
  box-shadow: var(--card-shadow);
}
.market-shelf-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.8rem 1.5rem;
  padding: 1rem 1.25rem;
  background: var(--surface-muted);
  border-bottom: 1px dashed var(--border-default);
}
.market-shelf-identity {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
}
.market-shelf-identity > div {
  min-width: 0;
}
.market-channel-symbol {
  display: grid;
  place-items: center;
  flex: none;
  width: 2.4rem;
  height: 2.4rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--accent-soft);
  color: var(--accent-text);
  font-size: 0.75rem;
}
.market-shelf-identity h2 {
  font-size: 1rem;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.market-shelf-ratio {
  display: inline-block;
  margin-left: 0.35rem;
  padding: 0.1rem 0.35rem;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  color: var(--accent-text);
  font: 0.75rem/1.4 var(--font-mono);
}
.market-shelf-identity p {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  margin-top: 0.3rem;
}
.market-shelf-statistics :deep(.model-statistics-empty) {
  border: 0;
  padding: 0;
}
.market-shelf-items {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 0 1.25rem 0.4rem;
  column-gap: 1.4rem;
}
.market-shelf-item {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 1.05rem 0;
  border-bottom: 1px dashed var(--border-subtle);
}
.market-shelf-capabilities {
  margin: 0.7rem 0 0.8rem;
}
.market-shelf-offers {
  display: flex;
  flex-direction: column;
  flex: 1;
}
.market-shelf-offer {
  position: relative;
  display: block;
  width: 100%;
  min-width: 0;
  text-align: left;
  padding: 0.65rem 0.35rem;
  margin-inline: -0.35rem;
  border-top: 1px dashed var(--border-subtle);
  border-radius: var(--shape-control);
  transition: background-color 0.15s;
}
.market-shelf-offer:hover {
  background: var(--state-hover-layer);
}
.market-shelf-offer-hit {
  position: absolute;
  inset: 0;
  z-index: 1;
  width: 100%;
  height: 100%;
  border-radius: inherit;
}
.market-shelf-offer-hit:focus-visible {
  outline-offset: -2px;
}
.market-shelf-offer-heading {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.3rem 0.6rem;
  margin-bottom: 0.4rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
}
.market-shelf-offer-heading,
.market-shelf-offer :deep(.market-price) {
  position: relative;
  pointer-events: none;
}
.market-shelf-item-stats {
  margin-top: auto;
}
@container model-market (width > 1100px) {
  .market-shelf-item:nth-child(3n + 1):nth-last-child(-n + 3),
  .market-shelf-item:nth-child(3n + 1):nth-last-child(-n + 3)
    ~ .market-shelf-item {
    border-bottom: 0;
  }
}
@container model-market (width > 720px) and (width <= 1100px) {
  .market-shelf-items {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .market-shelf-item:nth-child(2n + 1):nth-last-child(-n + 2),
  .market-shelf-item:nth-child(2n + 1):nth-last-child(-n + 2)
    ~ .market-shelf-item {
    border-bottom: 0;
  }
}
@container model-market (max-width: 720px) {
  .market-shelves {
    gap: 1rem;
  }
  .market-shelf-items {
    grid-template-columns: minmax(0, 1fr);
    padding: 0 1rem;
  }
  .market-shelf-header {
    padding: 0.9rem 1rem;
  }
  .market-shelf-identity h2 {
    font-size: 0.9375rem;
  }
  .market-shelf-statistics {
    width: 100%;
    padding-top: 0.6rem;
    border-top: 1px dashed var(--border-subtle);
  }
  .market-shelf-item:last-child {
    border-bottom: 0;
  }
}
</style>
