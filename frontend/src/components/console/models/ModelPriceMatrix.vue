<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketChannel, MarketModel } from '@/types/modelMarket'
import { formatMarketRatio } from '@/utils/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'

const props = defineProps<{
  models: MarketModel[]
  channels: MarketChannel[]
}>()
const emit = defineEmits<{ detail: [modelId: string, offerId?: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const descriptionPrefix = useId()
const quoteId = (id: string, mobile = false) =>
  `${descriptionPrefix}-${mobile ? 'mobile' : 'table'}-${encodeURIComponent(id)}`
const hoveredChannel = ref<number | null>(null)
const focusedChannel = ref<number | null>(null)
const activeChannel = computed(
  () => hoveredChannel.value ?? focusedChannel.value
)
const rows = computed(() =>
  props.models.map((model) => ({
    model,
    variants: model.baselines.length ? model.baselines : [undefined],
    columns: props.channels.map((channel) => ({
      channel,
      offers: model.offers.filter((offer) => offer.channelId === channel.id),
    })),
  }))
)
watch(
  () => props.models,
  () => {
    hoveredChannel.value = null
    focusedChannel.value = null
  }
)
function onFocus(event: FocusEvent) {
  hoveredChannel.value = null
  const column = (event.target as HTMLElement).closest<HTMLElement>(
    '[data-channel-id]'
  )
  focusedChannel.value = column ? Number(column.dataset.channelId) : null
  if (!column) return
  const scroller = event.currentTarget as HTMLElement
  const target = event.target as HTMLElement
  const left = scroller
    .querySelector('.model-column')!
    .getBoundingClientRect().right
  const right = scroller
    .querySelector('.benchmark-column')!
    .getBoundingClientRect().left
  const bounds = target.getBoundingClientRect()
  // Native focus scrolling does not account for the two sticky columns.
  if (bounds.left < left) scroller.scrollLeft += bounds.left - left
  else if (bounds.right > right) scroller.scrollLeft += bounds.right - right
}
function onFocusOut(event: FocusEvent) {
  if (
    !(event.currentTarget as HTMLElement).contains(
      event.relatedTarget as Node | null
    )
  )
    focusedChannel.value = null
}
</script>

<template>
  <div class="market-comparison">
    <div
      class="market-matrix-scroll subtle-scroll focus-ring"
      tabindex="0"
      role="region"
      :aria-label="t('tableRegion')"
      data-market-matrix
      @mouseleave="hoveredChannel = null"
      @focusin="onFocus"
      @focusout="onFocusOut"
    >
      <table
        class="market-matrix"
        :style="{ minWidth: `${435 + channels.length * 205}px` }"
      >
        <caption class="sr-only">
          {{
            t('tableCaption')
          }}
        </caption>
        <thead>
          <tr class="market-matrix-groups">
            <th
              class="model-column"
              rowspan="2"
              scope="col"
              @mouseenter="hoveredChannel = null"
            >
              {{ t('tableModel') }}
            </th>
            <th
              v-if="channels.length"
              :colspan="channels.length"
              scope="colgroup"
            >
              {{ t('tableChannels') }}
            </th>
            <th
              class="benchmark-column"
              rowspan="2"
              scope="col"
              data-market-baseline
              @mouseenter="hoveredChannel = null"
            >
              <div class="matrix-baseline-title">
                {{ t('benchmark') }} <span>1.0×</span>
              </div>
              <p>{{ t('reference') }}</p>
            </th>
          </tr>
          <tr>
            <th
              v-for="channel in channels"
              :key="channel.id"
              class="channel-column"
              scope="col"
              :data-channel-id="channel.id"
              :class="{ 'col-hover': activeChannel === channel.id }"
              @mouseenter="hoveredChannel = channel.id"
            >
              <span class="matrix-channel-title"
                >{{ channel.name
                }}<span class="matrix-ratio">{{
                  formatMarketRatio(channel.ratio)
                }}</span></span
              >
            </th>
          </tr>
        </thead>
        <tbody v-for="(row, rowIndex) in rows" :key="row.model.id">
          <tr
            v-for="(baseline, variantIndex) in row.variants"
            :key="baseline?.id ?? row.model.id"
            :data-model-name="row.model.name"
            :data-baseline-id="baseline?.id"
          >
            <th
              v-if="variantIndex === 0"
              class="model-column"
              scope="rowgroup"
              :rowspan="row.variants.length"
              :class="{ 'last-model': rowIndex === rows.length - 1 }"
              @mouseenter="hoveredChannel = null"
            >
              <ModelIdentity
                :name="row.model.name"
                :vendor="row.model.vendor"
                :type-label="t(`types.${row.model.type}`)"
                compact
                clickable
                @detail="emit('detail', row.model.id)"
              />
            </th>
            <td
              v-for="column in row.columns"
              :key="column.channel.id"
              class="quote-cell"
              :data-channel-id="column.channel.id"
              :class="{ 'col-hover': activeChannel === column.channel.id }"
              @mouseenter="hoveredChannel = column.channel.id"
            >
              <div
                v-for="offer in column.offers.filter(
                  (item) => item.baseline.id === baseline?.id
                )"
                :key="offer.id"
                class="matrix-offer"
              >
                <button
                  type="button"
                  class="matrix-offer-action"
                  :data-offer-id="offer.id"
                  :aria-label="
                    t('openOffer', {
                      name: row.model.name,
                      channel: column.channel.name,
                    })
                  "
                  :aria-describedby="quoteId(offer.id)"
                  @click="emit('detail', row.model.id, offer.id)"
                />
                <div :id="quoteId(offer.id)">
                  <span class="sr-only">{{
                    formatMarketRatio(offer.ratio)
                  }}</span>
                  <span class="matrix-variant"
                    >{{
                      pricingVariantLabel(offer.pricing, t('defaultVariant'))
                    }}
                    <span>{{
                      t(`billingModes.${offer.pricing.billingMode}`)
                    }}</span></span
                  >
                  <ModelPriceDisplay
                    :pricing="offer.pricing"
                    mode="comparison"
                  />
                </div>
              </div>
              <span
                v-if="
                  !column.offers.some(
                    (item) => item.baseline.id === baseline?.id
                  )
                "
                class="matrix-no-supply"
                :aria-label="t('noSupply')"
                >—</span
              >
            </td>
            <td
              class="benchmark-column"
              data-market-baseline
              @mouseenter="hoveredChannel = null"
            >
              <div v-if="baseline" class="matrix-baseline-prices">
                <span class="matrix-variant"
                  >{{ pricingVariantLabel(baseline, t('defaultVariant')) }}
                  <span>{{
                    t(`billingModes.${baseline.billingMode}`)
                  }}</span></span
                >
                <ModelPriceDisplay :pricing="baseline" mode="comparison" />
              </div>
              <ModelPriceDisplay v-else />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="market-matrix-mobile" data-market-matrix-mobile>
      <article
        v-for="row in rows"
        :key="row.model.id"
        class="matrix-mobile-model"
        :data-model-name="row.model.name"
      >
        <div class="matrix-mobile-heading">
          <ModelIdentity
            :name="row.model.name"
            :vendor="row.model.vendor"
            :type-label="t(`types.${row.model.type}`)"
            compact
            clickable
            @detail="emit('detail', row.model.id)"
          />
        </div>
        <section
          class="matrix-mobile-baseline"
          data-market-baseline
          :aria-label="t('benchmark')"
        >
          <h3>
            {{ t('benchmark') }}<span>1.0× · {{ t('reference') }}</span>
          </h3>
          <div
            v-for="baseline in row.model.baselines"
            :key="baseline.id"
            class="matrix-baseline-prices"
          >
            <span class="matrix-variant"
              >{{ pricingVariantLabel(baseline, t('defaultVariant')) }}
              <span>{{ t(`billingModes.${baseline.billingMode}`) }}</span></span
            >
            <ModelPriceDisplay :pricing="baseline" mode="comparison" />
          </div>
          <ModelPriceDisplay v-if="!row.model.baselines.length" />
        </section>
        <div class="matrix-mobile-quotes">
          <template v-for="column in row.columns" :key="column.channel.id">
            <div
              v-for="offer in column.offers"
              :key="offer.id"
              class="matrix-mobile-offer"
            >
              <button
                type="button"
                class="matrix-offer-action"
                :data-offer-id="offer.id"
                :aria-label="
                  t('openOffer', {
                    name: row.model.name,
                    channel: column.channel.name,
                  })
                "
                :aria-describedby="quoteId(offer.id, true)"
                @click="emit('detail', row.model.id, offer.id)"
              />
              <div :id="quoteId(offer.id, true)">
                <span class="matrix-channel-title"
                  >{{ column.channel.name }}
                  <span class="matrix-ratio">{{
                    formatMarketRatio(offer.ratio)
                  }}</span></span
                >
                <span class="matrix-variant"
                  >{{ pricingVariantLabel(offer.pricing, t('defaultVariant')) }}
                  <span>{{
                    t(`billingModes.${offer.pricing.billingMode}`)
                  }}</span></span
                >
                <ModelPriceDisplay :pricing="offer.pricing" mode="comparison" />
              </div>
            </div>
          </template>
          <p v-if="!row.model.offers.length" class="matrix-mobile-empty">
            {{ t('noSupply') }}
          </p>
        </div>
      </article>
    </div>
    <p class="market-matrix-help">{{ t('tableHint') }}</p>
  </div>
</template>

<style scoped>
.market-matrix-scroll {
  overflow: auto;
  scroll-padding-inline: 230px 205px;
  isolation: isolate;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  background: var(--surface-solid);
  box-shadow: var(--card-shadow);
}
.market-matrix {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  text-align: left;
  font-size: 0.75rem;
}
.market-matrix th,
.market-matrix td {
  vertical-align: top;
  border-bottom: 1px var(--row-divider-style) var(--border-default);
  border-right: 1px solid var(--border-subtle);
}
.market-matrix th {
  font-weight: 500;
  padding: 0.8rem 0.9rem;
  background: var(--surface-table-header);
}
.market-matrix .model-column.last-model,
.market-matrix tbody:last-of-type tr:last-child th,
.market-matrix tbody:last-of-type tr:last-child td {
  border-bottom: 0;
}
.market-matrix .model-column {
  position: sticky;
  left: 0;
  z-index: 3;
  width: 230px;
  min-width: 230px;
  max-width: 230px;
  background: var(--surface-solid);
  box-shadow: 3px 0 8px var(--shadow-color);
}
.market-matrix thead .model-column {
  background: var(--surface-table-header);
}
.market-matrix .benchmark-column {
  position: sticky;
  right: 0;
  z-index: 3;
  width: 205px;
  min-width: 205px;
  max-width: 205px;
  padding: 0.8rem 0.9rem;
  background: var(--surface-table-header);
  border-left: 2px solid var(--border-strong);
  border-right: 0;
  box-shadow: -3px 0 10px var(--shadow-color);
}
.market-matrix-groups {
  color: var(--text-tertiary);
  font-size: 0.6875rem;
}
.market-matrix .channel-column {
  min-width: 205px;
  transition:
    background-color 0.12s,
    box-shadow 0.12s;
}
.matrix-channel-title {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
  color: var(--text-primary);
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.matrix-ratio {
  flex: none;
  padding: 0.08rem 0.3rem;
  background: var(--accent-soft);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  color: var(--accent-text);
  font: 0.6875rem/1.5 var(--font-mono);
}
.matrix-baseline-title {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  flex-wrap: wrap;
  color: var(--accent-text);
  font-size: 0.8125rem;
}
.matrix-baseline-title span {
  font: 0.6875rem var(--font-mono);
}
.benchmark-column > p {
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  margin-top: 0.4rem;
}
.market-matrix .quote-cell {
  padding: 0;
  min-width: 205px;
  transition: background-color 0.12s;
}
.market-matrix th.col-hover {
  background: color-mix(
    in srgb,
    var(--accent) 12%,
    var(--surface-table-header)
  );
  box-shadow: inset 0 2px 0 var(--accent);
}
.market-matrix td.col-hover {
  background: color-mix(in srgb, var(--accent) 6%, var(--surface-solid));
}
.matrix-offer,
.matrix-mobile-offer {
  position: relative;
}
.matrix-offer {
  display: block;
  width: 100%;
  text-align: left;
  padding: 0.8rem 0.9rem;
  min-height: 6rem;
}
.matrix-offer:hover,
.matrix-offer:has(:focus-visible) {
  background: var(--state-press-layer);
}
.matrix-offer-action {
  position: absolute;
  inset: 2px;
  z-index: 1;
  border-radius: var(--shape-control);
  cursor: pointer;
}
.matrix-offer-action:focus-visible {
  outline: 2px solid var(--accent-text);
  outline-offset: -2px;
}
.matrix-variant {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 0.25rem 0.5rem;
  color: var(--text-tertiary);
  font-size: 0.625rem;
  line-height: 1.6;
  margin-bottom: 0.5rem;
}
.matrix-no-supply {
  display: block;
  color: var(--text-tertiary);
  text-align: center;
  padding: 1.5rem;
}
.matrix-baseline-prices + .matrix-baseline-prices {
  margin-top: 0.8rem;
  padding-top: 0.8rem;
  border-top: 1px dashed var(--border-default);
}
.market-matrix-help {
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.7;
  margin-top: 0.75rem;
}
.market-matrix-mobile {
  display: none;
}
@container model-market (max-width: 720px) {
  .market-matrix-scroll,
  .market-matrix-help {
    display: none;
  }
  .market-matrix-mobile {
    display: grid;
    gap: 1rem;
  }
  .matrix-mobile-model {
    min-width: 0;
    border: 1px solid var(--border-subtle);
    border-radius: var(--shape-card);
    background: var(--surface-solid);
    box-shadow: var(--card-shadow);
  }
  .matrix-mobile-heading {
    padding: 1rem 1rem 0.7rem;
  }
  .matrix-mobile-baseline {
    margin: 0 1rem 0.5rem;
    padding: 0.8rem;
    background: var(--surface-table-header);
    border: 1px solid var(--border-default);
    border-radius: var(--shape-control);
  }
  .matrix-mobile-baseline h3 {
    display: flex;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 0.3rem;
    font-size: 0.75rem;
    color: var(--accent-text);
    margin-bottom: 0.6rem;
  }
  .matrix-mobile-baseline h3 span {
    font: 0.625rem/1.7 var(--font-mono);
  }
  .matrix-mobile-quotes {
    padding: 0 1rem 0.4rem;
  }
  .matrix-mobile-offer {
    display: block;
    width: 100%;
    text-align: left;
    padding: 0.75rem 0;
    border-bottom: 1px dashed var(--border-subtle);
  }
  .matrix-mobile-offer:last-child {
    border-bottom: 0;
  }
  .matrix-mobile-offer .matrix-channel-title {
    margin-bottom: 0.4rem;
  }
  .matrix-mobile-empty {
    font-size: 0.75rem;
    color: var(--text-tertiary);
    padding: 0.8rem 0;
  }
}

html.dark .matrix-baseline-prices + .matrix-baseline-prices {
  border-top: 1px solid var(--border-subtle);
}
@container model-market (max-width: 720px) {
  html.dark .matrix-mobile-offer {
    border-bottom-style: solid;
  }
}
</style>
