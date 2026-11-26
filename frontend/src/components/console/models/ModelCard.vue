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
import type {
  MarketModel,
  MarketOffer,
  ModelPerformanceState,
} from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { formatMarketRatio } from '@/utils/modelMarket'
import { resolveOfferHealth } from './catalogHealth'
import { pricingVariantLabel } from './pricingVariantLabel'
import CatalogHealthBadge from './CatalogHealthBadge.vue'
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
const quoteId = `${cardId}-quote`
const statsId = `${cardId}-stats`
const overflowMenuId = `${cardId}-overflow`
const toastId = `${cardId}-health-toast`

const selectedOfferId = ref<string | null>(null)
const overflowOpen = ref(false)
const cycleSpin = ref(false)
let spinTimer: ReturnType<typeof setTimeout> | null = null
const railEl = ref<HTMLElement | null>(null)
const cardEl = ref<HTMLElement | null>(null)
const toastEl = ref<HTMLElement | null>(null)
const fitCount = ref(MAX_INLINE_CHANNELS)

interface HealthToastState {
  offer: MarketOffer
  anchor: HTMLElement
  left: number
  top: number
  placement: 'top' | 'bottom'
}
const healthToast = ref<HealthToastState | null>(null)

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
    healthToast.value = null
  }
)

const activeOffer = computed(
  () =>
    props.model.offers.find((offer) => offer.id === selectedOfferId.value) ??
    props.model.offers[0]
)

/** One representative offer per distinct channel for round-robin cycling. */
const distinctChannelOffers = computed(() => {
  const seen = new Set<number>()
  const list: MarketOffer[] = []
  for (const offer of props.model.offers) {
    if (!seen.has(offer.channelId)) {
      seen.add(offer.channelId)
      list.push(offer)
    }
  }
  return list
})
const channelCount = computed(() => distinctChannelOffers.value.length)
const hasMultipleChannels = computed(() => channelCount.value > 1)

const visibleTags = computed(() => props.model.tags.slice(0, 3))
const hiddenTagCount = computed(() =>
  Math.max(0, props.model.tags.length - visibleTags.value.length)
)

function offerHealth(offer: MarketOffer) {
  return resolveOfferHealth(offer.health, t)
}

const activeToastHealth = computed(() =>
  healthToast.value ? offerHealth(healthToast.value.offer) : null
)

function computeToastPosition(anchor: HTMLElement, toastWidth = 240) {
  const rect = anchor.getBoundingClientRect()
  const vw = typeof window !== 'undefined' ? window.innerWidth : 1280
  const pad = 12
  const half = Math.min(toastWidth / 2, Math.max(0, vw / 2 - pad))
  const rawCenter = rect.left + rect.width / 2
  const left = Math.min(Math.max(rawCenter, pad + half), vw - pad - half)
  const placement: 'top' | 'bottom' = rect.top < 88 ? 'bottom' : 'top'
  const top = placement === 'top' ? rect.top - 8 : rect.bottom + 8
  return { left, top, placement }
}

function showChannelToast(offer: MarketOffer, event: Event) {
  const anchor = event.currentTarget as HTMLElement | null
  if (!anchor) return
  const pos = computeToastPosition(anchor)
  healthToast.value = { offer, anchor, ...pos }
  void nextTick(() => {
    if (!healthToast.value || healthToast.value.anchor !== anchor) return
    const width = toastEl.value?.offsetWidth || 240
    const refined = computeToastPosition(anchor, width)
    healthToast.value = { offer, anchor, ...refined }
  })
}

function hideChannelToast(event?: Event) {
  if (
    event &&
    healthToast.value &&
    event.currentTarget !== healthToast.value.anchor
  ) {
    return
  }
  healthToast.value = null
}

function cycleChannel() {
  const candidates = distinctChannelOffers.value
  if (candidates.length <= 1 || !activeOffer.value) return
  const currentIndex = candidates.findIndex(
    (item) => item.channelId === activeOffer.value?.channelId
  )
  const nextOffer = candidates[(currentIndex + 1) % candidates.length]!
  selectedOfferId.value = nextOffer.id
  overflowOpen.value = false
  if (healthToast.value) {
    healthToast.value = { ...healthToast.value, offer: nextOffer }
  }
  cycleSpin.value = false
  void nextTick(() => {
    cycleSpin.value = true
    if (spinTimer) clearTimeout(spinTimer)
    spinTimer = setTimeout(() => {
      cycleSpin.value = false
    }, 300)
  })
}

const railOffers = computed(() => {
  const offers = props.model.offers
  if (offers.length <= 1) return []
  const maxShown = Math.min(MAX_INLINE_CHANNELS, fitCount.value, offers.length)
  if (!activeOffer.value) return offers
  const activeIndex = offers.findIndex(
    (item) => item.id === activeOffer.value?.id
  )
  if (activeIndex < maxShown) return offers
  // Keep the active channel chip visible when selected from overflow or cycled.
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
  healthToast.value = null
}

function handleDocumentPointer(event: MouseEvent) {
  if (!overflowOpen.value) return
  if (cardEl.value && !cardEl.value.contains(event.target as Node)) {
    overflowOpen.value = false
  }
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    if (overflowOpen.value) overflowOpen.value = false
    if (healthToast.value) healthToast.value = null
  }
}

function handleScroll() {
  if (healthToast.value) healthToast.value = null
}

let observer: ResizeObserver | null = null
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(() => measureRail())
    if (railEl.value) observer.observe(railEl.value)
  }
  document.addEventListener('pointerdown', handleDocumentPointer)
  document.addEventListener('keydown', handleDocumentKeydown)
  window.addEventListener('scroll', handleScroll, true)
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
  if (spinTimer) clearTimeout(spinTimer)
  document.removeEventListener('pointerdown', handleDocumentPointer)
  document.removeEventListener('keydown', handleDocumentKeydown)
  window.removeEventListener('scroll', handleScroll, true)
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
      :aria-describedby="`${quoteId} ${statsId}`"
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
          <span
            class="market-channel-name market-channel-hover-anchor"
            @mouseenter="showChannelToast(activeOffer, $event)"
            @mouseleave="hideChannelToast($event)"
            @click="emit('detail', model.id, activeOffer.id)"
            ><span class="market-channel-title">{{
              activeOffer.channelName
            }}</span>
            <span class="market-ratio">{{
              formatMarketRatio(activeOffer.ratio)
            }}</span></span
          >
          <div class="market-quote-meta">
            <span class="market-billing-label">{{
              t(`billingModes.${activeOffer.pricing.billingMode}`)
            }}</span>
            <span
              class="market-health-badge-anchor"
              @mouseenter="showChannelToast(activeOffer, $event)"
              @mouseleave="hideChannelToast($event)"
              @click="emit('detail', model.id, activeOffer.id)"
            >
              <CatalogHealthBadge :health="activeOffer.health" badge />
            </span>
            <button
              v-if="hasMultipleChannels"
              type="button"
              class="market-cycle-channel focus-ring touch-target"
              :class="{ 'is-spinning': cycleSpin }"
              data-cycle-channel
              :aria-label="t('cycleChannel')"
              :title="t('cycleChannel')"
              @click.stop="cycleChannel"
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
                <path d="M3 3v5h5" />
                <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" />
                <path d="M16 16h5v5" />
              </svg>
            </button>
          </div>
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
            <span
              class="market-rail-dot"
              :class="`is-${offerHealth(offer).tone}`"
            />
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
          :aria-describedby="
            healthToast?.offer.id === offer.id ? toastId : undefined
          "
          :data-channel-chip="offer.channelId"
          @click.stop="selectChannel(offer.id)"
          @mouseenter="showChannelToast(offer, $event)"
          @mouseleave="hideChannelToast($event)"
          @focus="showChannelToast(offer, $event)"
          @blur="hideChannelToast($event)"
        >
          <span
            class="market-rail-dot"
            :class="`is-${offerHealth(offer).tone}`"
            aria-hidden="true"
          />
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
              @mouseenter="showChannelToast(offer, $event)"
              @mouseleave="hideChannelToast($event)"
              @focus="showChannelToast(offer, $event)"
              @blur="hideChannelToast($event)"
            >
              <span class="market-rail-popover-main">
                <span
                  class="market-rail-dot"
                  :class="`is-${offerHealth(offer).tone}`"
                  aria-hidden="true"
                />
                <span class="market-rail-popover-name">{{
                  offer.channelName
                }}</span>
              </span>
              <span class="market-rail-chip-ratio">{{
                formatMarketRatio(offer.ratio)
              }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- Model-wide metrics sit outside the channel lane and anchor to the card base. -->
      <ModelStatistics
        :id="statsId"
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

    <Teleport to="body">
      <div
        v-if="healthToast && activeToastHealth"
        :id="toastId"
        ref="toastEl"
        role="tooltip"
        class="market-health-toast"
        :class="[`is-${activeToastHealth.tone}`, `is-${healthToast.placement}`]"
        :style="{
          left: `${healthToast.left}px`,
          top: `${healthToast.top}px`,
        }"
        data-channel-health-toast
        :data-toast-state="activeToastHealth.state"
      >
        <div class="market-health-toast-head">
          <span class="market-health-toast-channel">{{
            healthToast.offer.channelName
          }}</span>
          <span class="market-health-toast-ratio">{{
            formatMarketRatio(healthToast.offer.ratio)
          }}</span>
        </div>
        <div class="market-health-toast-status">
          <span
            class="market-rail-dot"
            :class="`is-${activeToastHealth.tone}`"
            aria-hidden="true"
          />
          <span class="market-health-toast-primary">{{
            activeToastHealth.primary
          }}</span>
        </div>
        <p
          v-if="activeToastHealth.secondary"
          class="market-health-toast-secondary"
        >
          {{ activeToastHealth.secondary }}
        </p>
      </div>
    </Teleport>
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
  padding: 0.75rem 0.85rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  min-width: 0;
}
.market-quote-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.35rem 0.5rem;
  flex-wrap: wrap;
  margin-bottom: 0.45rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.market-channel-hover-anchor {
  position: relative;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.25rem;
  min-width: 0;
  pointer-events: auto;
  cursor: pointer;
}
.market-channel-name {
  color: var(--text-primary);
  font-weight: 500;
}
.market-channel-title {
  min-width: 0;
  overflow-wrap: anywhere;
}
.market-ratio {
  display: inline-block;
  padding: 0.05rem 0.35rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  font: 0.6875rem/1.45 var(--font-mono);
  color: var(--accent-text);
  background: var(--accent-soft);
}
.market-quote-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  flex: none;
  margin-left: auto;
}
.market-billing-label {
  color: var(--text-tertiary);
  font-size: 0.625rem;
  white-space: nowrap;
}
.market-health-badge-anchor {
  position: relative;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  pointer-events: auto;
  cursor: pointer;
}
.market-cycle-channel {
  position: relative;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.375rem;
  height: 1.375rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-solid);
  color: var(--text-tertiary);
  pointer-events: auto;
  cursor: pointer;
  transition:
    border-color 0.14s,
    background-color 0.14s,
    color 0.14s;
}
.market-cycle-channel:hover {
  border-color: var(--border-strong);
  background: var(--state-hover-layer);
  color: var(--text-primary);
}
.market-cycle-channel svg {
  transition: transform 0.28s var(--ease-spring-snappy, ease);
}
.market-cycle-channel.is-spinning svg {
  transform: rotate(180deg);
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
.market-rail-dot {
  width: 6px;
  height: 6px;
  border-radius: 9999px;
  flex: none;
}
.market-rail-dot.is-good {
  background: var(--status-success);
}
.market-rail-dot.is-warn {
  background: var(--status-warning);
}
.market-rail-dot.is-bad {
  background: var(--status-danger);
}
.market-rail-dot.is-neutral {
  border: 1.5px solid var(--load-unchecked);
  background: transparent;
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
  border: 1px solid var(--overlay-border, var(--border-default));
  border-radius: var(--shape-control);
  background: var(--surface-overlay, var(--surface-solid));
  box-shadow: var(--overlay-shadow, var(--card-shadow-hover));
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
.market-rail-popover-main {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  min-width: 0;
}
.market-rail-popover-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.market-card-statistics {
  margin-top: auto;
  padding-bottom: 0.1rem;
}
.market-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.65rem 1rem;
  margin-top: 0.5rem;
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
  .market-rail-chip,
  .market-cycle-channel,
  .market-cycle-channel svg {
    transition: none;
  }
  .market-model-card:hover,
  .market-cycle-channel.is-spinning svg {
    transform: none;
  }
}

html.dark .market-quote-stage {
  background: var(--surface-container-low);
}
html.dark .market-card-footer {
  background: var(--surface-container-low);
  border-top: 1px solid var(--border-subtle);
}
</style>

<style>
/* Teleported floating mini-toast lives under <body>, so rules stay unscoped
   while tightly prefixed with .market-health-toast. */
.market-health-toast {
  position: fixed;
  z-index: var(--z-tooltip, 300);
  pointer-events: none;
  min-width: 10.5rem;
  max-width: min(17rem, calc(100vw - 1.5rem));
  padding: 0.45rem 0.65rem 0.48rem 0.72rem;
  border: 1px solid var(--overlay-border, var(--border-default));
  border-radius: var(--shape-control, 10px);
  background: var(--surface-overlay, var(--surface-solid));
  box-shadow: var(--overlay-shadow, var(--card-shadow-hover));
  color: var(--text-primary);
  display: grid;
  gap: 0.22rem;
  overflow: hidden;
}
html.dark .market-health-toast {
  --text-tertiary: var(--text-tertiary-overlay);
  --text-tertiary-base: var(--text-tertiary-overlay);
  background: var(--surface-container-highest);
  border-color: var(--outline);
  box-shadow: var(--elevation-5);
}
.market-health-toast.is-top {
  transform: translate(-50%, -100%);
}
.market-health-toast.is-bottom {
  transform: translate(-50%, 0);
}
.market-health-toast::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: var(--load-unchecked);
}
.market-health-toast.is-good::before {
  background: var(--status-success);
}
.market-health-toast.is-warn::before {
  background: var(--status-warning);
}
.market-health-toast.is-bad::before {
  background: var(--status-danger);
}
.market-health-toast-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.market-health-toast-channel {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  color: var(--text-secondary);
}
.market-health-toast-ratio {
  flex: none;
  font-family: var(--font-mono);
  font-size: 0.625rem;
  color: var(--accent-text);
}
.market-health-toast-status {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  font-weight: 600;
  line-height: 1.35;
}
.market-health-toast.is-good .market-health-toast-primary {
  color: var(--status-success-text);
}
.market-health-toast.is-warn .market-health-toast-primary {
  color: var(--status-warning-text);
}
.market-health-toast.is-bad .market-health-toast-primary {
  color: var(--status-danger-text);
}
.market-health-toast.is-neutral .market-health-toast-primary {
  color: var(--text-primary);
}
.market-health-toast-secondary {
  font-size: 0.6875rem;
  line-height: 1.35;
  color: var(--status-warning-text);
  padding-left: 0.75rem;
}
.market-health-toast .market-rail-dot {
  width: 6px;
  height: 6px;
  border-radius: 9999px;
  flex: none;
}
.market-health-toast .market-rail-dot.is-good {
  background: var(--status-success);
}
.market-health-toast .market-rail-dot.is-warn {
  background: var(--status-warning);
}
.market-health-toast .market-rail-dot.is-bad {
  background: var(--status-danger);
}
.market-health-toast .market-rail-dot.is-neutral {
  border: 1.5px solid var(--load-unchecked);
  background: transparent;
}
</style>
