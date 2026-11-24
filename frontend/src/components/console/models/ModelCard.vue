<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
  watch,
} from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketModel, ModelPerformanceState } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { formatMarketRatio } from '@/utils/modelMarket'
import { pricingVariantLabel } from './pricingVariantLabel'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceDisplay from './ModelPriceDisplay.vue'
import ModelStatistics from './ModelStatistics.vue'

const MAX_INLINE_CHANNELS = 3

const props = defineProps<{
  model: MarketModel
  performance: ModelPerformanceState
}>()
const emit = defineEmits<{ detail: [modelId: string, offerId?: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const cardId = useId()
const benchmarkId = `${cardId}-benchmark`
const quoteId = `${cardId}-quote`
const overflowMenuId = `${cardId}-overflow`

const selectedOfferId = ref<string | null>(null)
const overflowOpen = ref(false)
const railEl = ref<HTMLElement | null>(null)
const cardEl = ref<HTMLElement | null>(null)
const fitCount = ref(MAX_INLINE_CHANNELS)

watch(
  () => props.model.offers,
  (offers) => {
    if (
      selectedOfferId.value &&
      !offers.some((offer) => offer.id === selectedOfferId.value)
    ) {
      selectedOfferId.value = null
    }
    overflowOpen.value = false
  }
)

const activeOffer = computed(
  () =>
    props.model.offers.find((offer) => offer.id === selectedOfferId.value) ??
    props.model.offers[0]
)
const reference = computed(
  () => activeOffer.value?.baseline ?? props.model.baselines[0]
)
const channelCount = computed(
  () => new Set(props.model.offers.map((offer) => offer.channelId)).size
)
const visibleTags = computed(() => props.model.tags.slice(0, 3))
const hiddenTagCount = computed(() =>
  Math.max(0, props.model.tags.length - visibleTags.value.length)
)

const railOffers = computed(() => {
  const offers = props.model.offers
  if (offers.length <= 1) return []
  const maxShown = Math.min(MAX_INLINE_CHANNELS, fitCount.value, offers.length)
  if (!activeOffer.value) return offers
  const activeIndex = offers.findIndex(
    (item) => item.id === activeOffer.value?.id
  )
  if (activeIndex < maxShown) return offers
  // Keep the active channel chip visible when selected from the overflow menu.
  const reordered = offers.filter((item) => item.id !== activeOffer.value?.id)
  reordered.splice(Math.max(0, maxShown - 1), 0, activeOffer.value)
  return reordered
})

const inlineOffers = computed(() => {
  const limit = Math.min(
    MAX_INLINE_CHANNELS,
    fitCount.value,
    railOffers.value.length
  )
  return railOffers.value.slice(0, Math.max(1, limit))
})

const overflowOffers = computed(() =>
  railOffers.value.slice(inlineOffers.value.length)
)

function measureRail() {
  const rail = railEl.value
  const total = props.model.offers.length
  if (!rail || total <= 1) {
    fitCount.value = MAX_INLINE_CHANNELS
    return
  }
  const available = rail.clientWidth
  if (available <= 0) {
    fitCount.value = Math.min(MAX_INLINE_CHANNELS, total)
    return
  }
  const probeNodes = rail.querySelectorAll<HTMLElement>('[data-rail-probe]')
  if (!probeNodes.length) {
    fitCount.value = Math.min(MAX_INLINE_CHANNELS, total)
    return
  }
  const gap = 6
  const overflowReserve = 46
  let used = 0
  let count = 0
  const maxCandidate = Math.min(MAX_INLINE_CHANNELS, probeNodes.length)
  for (let index = 0; index < maxCandidate; index++) {
    const chipWidth = Math.ceil(
      probeNodes[index]!.getBoundingClientRect().width
    )
    const willHaveOverflow = total > index + 1
    const needed =
      used +
      (count > 0 ? gap : 0) +
      chipWidth +
      (willHaveOverflow ? gap + overflowReserve : 0)
    if (count > 0 && needed > available) break
    used += (count > 0 ? gap : 0) + chipWidth
    count++
  }
  fitCount.value = Math.max(1, count)
}

function selectChannel(offerId: string) {
  selectedOfferId.value = offerId
  overflowOpen.value = false
}

function handleDocumentPointer(event: MouseEvent) {
  if (!overflowOpen.value) return
  if (cardEl.value && !cardEl.value.contains(event.target as Node)) {
    overflowOpen.value = false
  }
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && overflowOpen.value) {
    overflowOpen.value = false
  }
}

let observer: ResizeObserver | null = null
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(() => measureRail())
    if (railEl.value) observer.observe(railEl.value)
  }
  document.addEventListener('pointerdown', handleDocumentPointer)
  document.addEventListener('keydown', handleDocumentKeydown)
  void nextTick(measureRail)
})
watch(
  [railEl, () => props.model.offers],
  ([el]) => {
    observer?.disconnect()
    if (el) observer?.observe(el)
    void nextTick(measureRail)
  },
  { flush: 'post' }
)
onBeforeUnmount(() => {
  observer?.disconnect()
  document.removeEventListener('pointerdown', handleDocumentPointer)
  document.removeEventListener('keydown', handleDocumentKeydown)
})
</script>

<template>
  <article
    ref="cardEl"
    class="market-model-card"
    :class="{ 'is-menu-open': overflowOpen }"
    data-model-card
    :data-model-name="model.name"
  >
    <button
      class="market-card-hit focus-ring"
      type="button"
      :aria-label="t('openDetail', { name: model.name })"
      :aria-describedby="`${benchmarkId} ${quoteId}`"
      @click="emit('detail', model.id, activeOffer?.id)"
    />
    <div class="market-card-heading">
      <ModelIdentity
        :name="model.name"
        :vendor="model.vendor"
        :type-label="t(`types.${model.type}`)"
      />
      <div v-if="visibleTags.length" class="market-card-tags" data-model-tags>
        <span v-for="tag in visibleTags" :key="tag" class="market-card-tag">{{
          tag
        }}</span>
        <span v-if="hiddenTagCount > 0" class="market-card-tag is-count"
          >+{{ hiddenTagCount }}</span
        >
      </div>
    </div>

    <div class="market-card-quotes">
      <div
        v-if="activeOffer"
        :id="quoteId"
        class="market-quote-stage"
        data-channel-quote
      >
        <div class="market-quote-heading">
          <span class="market-channel-name"
            >{{ activeOffer.channelName }}
            <span class="market-ratio">{{
              formatMarketRatio(activeOffer.ratio)
            }}</span></span
          >
          <span class="market-billing-label">{{
            t(`billingModes.${activeOffer.pricing.billingMode}`)
          }}</span>
        </div>
        <p
          v-if="model.baselines.length > 1 || activeOffer.pricing.pluginName"
          class="market-variant-label"
        >
          {{ pricingVariantLabel(activeOffer.pricing, t('defaultVariant')) }}
        </p>
        <ModelPriceDisplay
          :id="`${quoteId}-price`"
          :pricing="activeOffer.pricing"
        />
      </div>
      <div v-else :id="quoteId" class="market-quote-stage market-no-offers">
        {{ t('noSupply') }}
      </div>

      <div
        v-if="model.offers.length > 1"
        ref="railEl"
        class="market-channel-rail"
        data-channel-rail
      >
        <!-- Off-flow measurement probes keep the visible rail strictly single-line. -->
        <div class="market-rail-probes" aria-hidden="true">
          <span
            v-for="offer in railOffers.slice(0, MAX_INLINE_CHANNELS)"
            :key="`probe:${offer.id}`"
            class="market-rail-chip"
            data-rail-probe
          >
            <span class="market-rail-chip-name">{{ offer.channelName }}</span>
            <span class="market-rail-chip-ratio">{{
              formatMarketRatio(offer.ratio)
            }}</span>
          </span>
        </div>
        <button
          v-for="offer in inlineOffers"
          :key="offer.id"
          type="button"
          class="market-rail-chip focus-ring"
          :class="{ active: offer.id === activeOffer?.id }"
          :aria-pressed="offer.id === activeOffer?.id"
          :aria-label="t('previewChannel', { channel: offer.channelName })"
          :data-channel-chip="offer.channelId"
          @click.stop="selectChannel(offer.id)"
        >
          <span class="market-rail-chip-name">{{ offer.channelName }}</span>
          <span class="market-rail-chip-ratio">{{
            formatMarketRatio(offer.ratio)
          }}</span>
        </button>
        <div v-if="overflowOffers.length" class="market-rail-overflow-wrap">
          <button
            type="button"
            class="market-rail-overflow focus-ring"
            data-channel-overflow
            aria-haspopup="menu"
            :aria-expanded="overflowOpen"
            :aria-controls="overflowMenuId"
            :aria-label="
              t('moreChannelsLabel', { count: overflowOffers.length })
            "
            @click.stop="overflowOpen = !overflowOpen"
          >
            +{{ overflowOffers.length }}
          </button>
          <div
            v-if="overflowOpen"
            :id="overflowMenuId"
            class="market-rail-popover"
            role="menu"
            data-channel-overflow-menu
            @click.stop
          >
            <button
              v-for="offer in overflowOffers"
              :key="offer.id"
              type="button"
              role="menuitem"
              class="market-rail-popover-item focus-ring"
              :data-channel-overflow-item="offer.channelId"
              @click="selectChannel(offer.id)"
            >
              <span class="market-rail-popover-name">{{
                offer.channelName
              }}</span>
              <span class="market-rail-chip-ratio">{{
                formatMarketRatio(offer.ratio)
              }}</span>
            </button>
          </div>
        </div>
      </div>

      <div
        :id="benchmarkId"
        class="market-baseline-strip"
        data-platform-reference
        data-market-baseline
      >
        <div class="market-baseline-heading">
          <span
            >{{ t('benchmark') }}
            <span class="market-baseline-ratio">{{
              formatMarketRatio('1')
            }}</span></span
          >
          <span class="market-reference-label">{{ t('reference') }}</span>
        </div>
        <p
          v-if="
            reference && (model.baselines.length > 1 || reference.pluginName)
          "
          class="market-variant-label"
        >
          {{ pricingVariantLabel(reference, t('defaultVariant')) }}
        </p>
        <ModelPriceDisplay :id="`${benchmarkId}-price`" :pricing="reference" />
      </div>

      <!-- Model-wide metrics sit outside the channel lane they do not describe. -->
      <ModelStatistics
        compact
        :performance="performance"
        class="market-card-statistics"
      />
    </div>

    <div class="market-card-footer">
      <span>
        {{ t('offerCount', { count: channelCount }) }}
        <template v-if="model.baselines.length > 1">
          · {{ t('variantCount', { count: model.baselines.length }) }}
        </template>
      </span>
      <span class="market-card-details"
        >{{ t('allQuotes')
        }}<svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"
        >
          <path d="M5 12h14m-5-5 5 5-5 5" /></svg
      ></span>
    </div>
  </article>
</template>

<style scoped>
.market-model-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  box-shadow: var(--card-shadow);
  transition:
    border-color 0.15s,
    box-shadow 0.15s,
    transform 0.15s;
}
.market-model-card.is-menu-open {
  z-index: 12;
}
.market-model-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--card-shadow-hover);
  transform: translateY(-2px);
}
.market-card-hit {
  position: absolute;
  inset: 0;
  z-index: 1;
  width: 100%;
  height: 100%;
  border-radius: inherit;
}
.market-card-hit:focus-visible {
  outline-offset: -3px;
}
.market-card-heading {
  padding: 1rem 1rem 0.7rem;
}
.market-card-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
  margin-top: 0.55rem;
}
.market-card-tag {
  display: inline-flex;
  align-items: center;
  padding: 0.08rem 0.45rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-secondary);
  font-size: 0.65rem;
  line-height: 1.45;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.market-card-tag.is-count {
  font-family: var(--font-mono);
  color: var(--text-tertiary);
}
.market-card-quotes {
  padding: 0 1rem;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.market-quote-stage {
  padding: 0.7rem 0.8rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  min-width: 0;
}
.market-quote-heading,
.market-baseline-heading {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.35rem 0.6rem;
  flex-wrap: wrap;
  margin-bottom: 0.4rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-quote-heading > span:first-child,
.market-baseline-heading > span:first-child {
  min-width: 0;
  overflow-wrap: anywhere;
}
.market-channel-name {
  color: var(--text-primary);
  font-weight: 500;
}
.market-ratio {
  display: inline-block;
  padding: 0.05rem 0.35rem;
  margin-left: 0.25rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  font: 0.6875rem/1.45 var(--font-mono);
  color: var(--accent-text);
  background: var(--accent-soft);
}
.market-baseline-strip {
  padding: 0.55rem 0 0;
  border-top: 1px dashed var(--border-subtle);
  min-width: 0;
}
.market-baseline-heading {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  margin-bottom: 0.28rem;
}
.market-baseline-ratio {
  font-family: var(--font-mono);
  margin-left: 0.2rem;
  color: var(--text-secondary);
}
.market-baseline-strip :deep(.market-price dd strong) {
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--text-secondary);
}
.market-reference-label,
.market-billing-label {
  color: var(--text-tertiary);
  font-size: 0.625rem;
}
.market-variant-label {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  margin-bottom: 0.3rem;
}
.market-no-offers {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  padding-block: 0.9rem;
}
.market-channel-rail {
  position: relative;
  z-index: 2;
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 6px;
  min-width: 0;
  pointer-events: auto;
}
.market-rail-probes {
  position: absolute;
  visibility: hidden;
  pointer-events: none;
  display: flex;
  flex-wrap: nowrap;
  gap: 6px;
  top: 0;
  left: 0;
  height: 0;
  overflow: hidden;
}
.market-rail-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.28rem;
  flex: 0 1 auto;
  min-width: 0;
  max-width: 100%;
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
  color: var(--text-secondary);
  font-size: 0.6875rem;
  line-height: 1.35;
  white-space: nowrap;
  cursor: pointer;
  transition:
    border-color 0.12s,
    background-color 0.12s,
    color 0.12s;
}
.market-rail-chip:hover {
  border-color: var(--border-strong);
  background: var(--state-hover-layer);
  color: var(--text-primary);
}
.market-rail-chip.active {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent-text);
  font-weight: 600;
}
.market-rail-chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.market-rail-chip-ratio {
  flex: none;
  font-family: var(--font-mono);
  font-size: 0.625rem;
  opacity: 0.85;
}
.market-rail-overflow-wrap {
  position: relative;
  flex: none;
  margin-left: auto;
}
.market-rail-overflow {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 2rem;
  padding: 0.25rem 0.45rem;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  color: var(--text-secondary);
  font: 600 0.6875rem/1.35 var(--font-mono);
  cursor: pointer;
}
.market-rail-overflow:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.market-rail-popover {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 20;
  min-width: 11.5rem;
  max-width: min(16rem, calc(100vw - 2.5rem));
  padding: 0.3rem;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
  box-shadow: var(--card-shadow-hover);
  display: grid;
  gap: 2px;
}
.market-rail-popover-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  width: 100%;
  padding: 0.42rem 0.55rem;
  border-radius: calc(var(--shape-control) - 2px);
  color: var(--text-primary);
  font-size: 0.75rem;
  text-align: left;
  cursor: pointer;
}
.market-rail-popover-item:hover {
  background: var(--state-hover-layer);
}
.market-rail-popover-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.market-card-statistics {
  margin-bottom: 0.2rem;
}
.market-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.65rem 1rem;
  margin-top: 0.55rem;
  background: var(--surface-muted);
  border-top: 1px dashed var(--border-subtle);
  border-radius: 0 0 var(--shape-card) var(--shape-card);
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.market-card-details {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-secondary);
}
.market-card-heading,
.market-card-quotes,
.market-card-footer {
  pointer-events: none;
}
.market-card-heading :deep(.market-copy) {
  pointer-events: auto;
  position: relative;
  z-index: 2;
}
@media (prefers-reduced-motion: reduce) {
  .market-model-card,
  .market-rail-chip {
    transition: none;
  }
  .market-model-card:hover {
    transform: none;
  }
}

html.dark .market-baseline-strip {
  border-top: 1px solid var(--border-subtle);
}
html.dark .market-card-footer {
  border-top: 1px solid var(--border-subtle);
}
</style>
