<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketChannelShelf } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { formatMarketRatio } from '@/utils/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import CatalogHealthBadge from './CatalogHealthBadge.vue'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'

defineProps<{ shelves: MarketChannelShelf[] }>()
const emit = defineEmits<{ detail: [modelId: string, offerId?: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const shelvesId = useId()
function offerDomId(kind: 'meta' | 'price' | 'health', offerId: string) {
  return `${shelvesId}-${kind}-${encodeURIComponent(offerId)}`
}
function shelfAnchorId(channelId: number) {
  return `${shelvesId}-shelf-${channelId}`
}
function jumpToShelf(channelId: number) {
  const target = document.getElementById(shelfAnchorId(channelId))
  if (!target) return
  const reduced =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  target.scrollIntoView({
    block: 'start',
    behavior: reduced ? 'auto' : 'smooth',
  })
}
</script>

<template>
  <div class="market-shelves" data-market-shelves>
    <nav
      v-if="shelves.length > 1"
      class="market-shelf-jump subtle-scroll"
      :aria-label="t('shelfJumpLabel')"
      data-shelf-jump
    >
      <button
        v-for="shelf in shelves"
        :key="shelf.channel.id"
        type="button"
        class="market-shelf-jump-pill focus-ring"
        @click="jumpToShelf(shelf.channel.id)"
      >
        <span class="market-shelf-jump-name">{{ shelf.channel.name }}</span>
        <span class="market-shelf-jump-ratio">{{
          formatMarketRatio(shelf.channel.ratio)
        }}</span>
        <span class="market-shelf-jump-count">{{ shelf.items.length }}</span>
      </button>
    </nav>

    <section
      v-for="shelf in shelves"
      :id="shelfAnchorId(shelf.channel.id)"
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
              {{ t('shelfCount', { count: shelf.items.length }) }}
            </p>
          </div>
        </div>
        <div class="market-shelf-statistics">
          <CatalogHealthBadge
            kind="channel"
            :channel-summary="shelf.channel.health"
            :filtered-count="shelf.items.length"
            compact
          />
        </div>
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
                :aria-describedby="`${shelvesId}-ratio-${shelf.channel.id} ${offerDomId('meta', offer.id)} ${offerDomId('price', offer.id)} ${offerDomId('health', offer.id)}`"
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
              <CatalogHealthBadge
                :id="offerDomId('health', offer.id)"
                :health="offer.health"
                compact
                class="market-shelf-offer-health"
              />
            </div>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.market-shelves {
  display: grid;
  gap: 1.25rem;
}
.market-shelf-jump {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  overflow-x: auto;
  padding: 0.25rem 0 0.35rem;
  min-width: 0;
}
.market-shelf-jump-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  flex: none;
  padding: 0.4rem 0.7rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
  color: var(--text-primary);
  font-size: 0.75rem;
  cursor: pointer;
  transition:
    border-color 0.15s,
    background-color 0.15s;
}
.market-shelf-jump-pill:hover {
  border-color: var(--border-strong);
  background: var(--state-hover-layer);
}
.market-shelf-jump-name {
  font-weight: 500;
}
.market-shelf-jump-ratio {
  padding: 0.05rem 0.3rem;
  border-radius: var(--shape-control);
  background: var(--accent-soft);
  color: var(--accent-text);
  font: 0.6875rem/1.4 var(--font-mono);
}
.market-shelf-jump-count {
  color: var(--text-tertiary);
  font: 0.6875rem/1.4 var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.market-shelf {
  min-width: 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  background: var(--surface-solid);
  overflow: hidden;
  box-shadow: var(--card-shadow);
  scroll-margin-top: 1rem;
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
  font-weight: 600;
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
  margin-top: 0.25rem;
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
  padding: 1rem 0;
  border-bottom: 1px dashed var(--border-subtle);
}
.market-shelf-offers {
  display: flex;
  flex-direction: column;
  flex: 1;
  margin-top: 0.75rem;
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
.market-shelf-offer :deep(.market-price),
.market-shelf-offer-health {
  position: relative;
  pointer-events: none;
}
.market-shelf-offer-health {
  margin-top: 0.45rem;
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

html.dark .market-shelf-header {
  border-bottom: 1px solid var(--border-subtle);
}
html.dark .market-shelf-item {
  border-bottom-style: solid;
}
html.dark .market-shelf-offer {
  border-top: 1px solid var(--border-subtle);
}
html.dark .market-shelf-statistics {
  border-top-style: solid;
}
@media (prefers-reduced-motion: reduce) {
  .market-shelf-jump-pill,
  .market-shelf-offer {
    transition: none;
  }
}
</style>
