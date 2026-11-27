<script setup lang="ts">
import {
  ArrowDown,
  ArrowUp,
  Database,
  Gauge,
  Info,
  PenLine,
  Zap,
} from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'

import type { LogItem } from '@/types/console'
import { formatNumber } from '@/utils/format'

import { formatLogCacheHitRate, getLogUsageSummary } from './logUsage'
import { getLogMetadata } from './logMetadata'

type PopoverPlacement = 'above' | 'below'

interface PopoverPosition {
  left: number
  top: number
  placement: PopoverPlacement
}

interface CacheTooltipPosition {
  x: number
  y: number
}

const props = withDefaults(
  defineProps<{
    log: LogItem
    mobile?: boolean
  }>(),
  { mobile: false }
)

const { t } = useI18n()

const detailsOpen = ref(false)
const triggerRef = ref<HTMLButtonElement | null>(null)
const cacheBarRef = ref<HTMLButtonElement | null>(null)
const popoverRef = ref<HTMLElement | null>(null)
const popoverId = `log-usage-popover-${useId()}`
const popoverTitleId = `log-usage-title-${useId()}`
const cacheTooltipId = `log-usage-cache-tooltip-${useId()}`
const cacheTooltip = ref<CacheTooltipPosition | null>(null)
const popoverPosition = ref<PopoverPosition>({
  left: 12,
  top: 12,
  placement: 'below',
})
const usage = computed(() => getLogUsageSummary(props.log))
const metadata = computed(() => getLogMetadata(props.log))
const cacheSummaryAvailable = computed(
  () =>
    usage.value.cacheReadTokens !== null ||
    usage.value.cacheWriteTokens !== null ||
    usage.value.cacheWrite12hTokens !== null
)
const cacheHitRateLabel = computed(() =>
  formatLogCacheHitRate(usage.value.cacheHitRate)
)
const cacheFillPercent = computed(() => {
  const rate = usage.value.cacheHitRate
  if (rate === null || !Number.isFinite(rate) || rate <= 0) return 0
  return Math.min(100, Math.max(rate, 0))
})
const hasCacheRead = computed(
  () => usage.value.cacheReadTokens !== null && usage.value.cacheReadTokens > 0
)
const cacheBarAriaLabel = computed(() =>
  cacheSummaryAvailable.value
    ? t('logs.cacheBarAria', { rate: cacheHitRateLabel.value })
    : t('logs.cacheUnavailableAria')
)
const popoverStyle = computed(() => ({
  left: `${popoverPosition.value.left}px`,
  top: `${popoverPosition.value.top}px`,
}))
const cacheTooltipStyle = computed(() =>
  cacheTooltip.value
    ? {
        left: `${cacheTooltip.value.x}px`,
        top: `${cacheTooltip.value.y}px`,
      }
    : undefined
)

function formatTokenValue(value: number | null): string {
  return value === null ? '—' : formatNumber(value)
}

function hideCacheTooltip(): void {
  cacheTooltip.value = null
}

function showCacheTooltip(event: Event): void {
  if (detailsOpen.value) return
  const target = event.currentTarget
  if (!(target instanceof HTMLElement)) return
  const rect = target.getBoundingClientRect()
  const horizontalInset = 128
  cacheTooltip.value = {
    x: Math.min(
      Math.max(rect.left + rect.width / 2, horizontalInset),
      window.innerWidth - horizontalInset
    ),
    y: Math.max(8, rect.top - 8),
  }
}

function dismissDetails(): void {
  detailsOpen.value = false
  hideCacheTooltip()
}

function updatePopoverPosition(): void {
  const trigger = triggerRef.value
  if (!trigger) return

  const rect = trigger.getBoundingClientRect()
  const viewportMargin = 12
  const popoverWidth = 256
  const popoverHeight = popoverRef.value?.offsetHeight ?? 208
  const maxLeft = Math.max(
    viewportMargin,
    window.innerWidth - popoverWidth - viewportMargin
  )
  const left = Math.min(
    Math.max(rect.right - popoverWidth, viewportMargin),
    maxLeft
  )
  const placement: PopoverPlacement =
    rect.top >= popoverHeight + viewportMargin ? 'above' : 'below'

  popoverPosition.value = {
    left,
    top: placement === 'above' ? rect.top - 8 : rect.bottom + 8,
    placement,
  }
}

async function openDetails(): Promise<void> {
  hideCacheTooltip()
  detailsOpen.value = true
  await nextTick()
  updatePopoverPosition()
}

function toggleDetails(): void {
  if (detailsOpen.value) dismissDetails()
  else void openDetails()
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (!detailsOpen.value || !(event.target instanceof Node)) return
  if (triggerRef.value?.contains(event.target)) return
  if (cacheBarRef.value?.contains(event.target)) return
  if (popoverRef.value?.contains(event.target)) return
  dismissDetails()
}

function onWindowKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || !detailsOpen.value) return
  event.preventDefault()
  dismissDetails()
  triggerRef.value?.focus()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  window.addEventListener('keydown', onWindowKeydown)
  window.addEventListener('scroll', dismissDetails, true)
  window.addEventListener('resize', dismissDetails, { passive: true })
  window.visualViewport?.addEventListener('resize', dismissDetails, {
    passive: true,
  })
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  window.removeEventListener('keydown', onWindowKeydown)
  window.removeEventListener('scroll', dismissDetails, true)
  window.removeEventListener('resize', dismissDetails)
  window.visualViewport?.removeEventListener('resize', dismissDetails)
})
</script>

<template>
  <div
    v-if="usage.available"
    data-log-usage
    class="flex w-full min-w-0 items-center"
    :class="mobile ? 'gap-3' : 'gap-2'"
  >
    <div
      data-log-usage-content
      class="flex min-w-0 flex-1 flex-col items-start overflow-hidden"
      :class="mobile ? 'gap-1.5' : 'gap-1'"
    >
      <div
        data-log-usage-io
        class="flex min-w-0 items-center gap-2.5 font-mono text-sm font-semibold leading-none"
      >
        <span
          data-log-usage-input
          class="inline-flex items-center gap-0.5 whitespace-nowrap tabular-nums text-[var(--status-success-text)]"
          :aria-label="`${t('logs.inputTokens')}: ${formatNumber(usage.promptTokens)}`"
        >
          <ArrowDown :size="13" stroke-width="1.9" aria-hidden="true" />
          {{ formatNumber(usage.promptTokens) }}
        </span>
        <span
          data-log-usage-output
          class="inline-flex items-center gap-0.5 whitespace-nowrap tabular-nums text-[var(--accent-text)]"
          :aria-label="`${t('logs.outputTokens')}: ${formatNumber(usage.completionTokens)}`"
        >
          <ArrowUp :size="13" stroke-width="1.9" aria-hidden="true" />
          {{ formatNumber(usage.completionTokens) }}
        </span>
      </div>

      <div
        data-log-usage-cache
        class="flex w-full min-w-0 items-center gap-2 text-[0.6875rem] leading-none text-[var(--text-tertiary)]"
      >
        <button
          ref="cacheBarRef"
          type="button"
          data-log-usage-cache-bar
          :aria-label="cacheBarAriaLabel"
          :aria-describedby="cacheTooltip ? cacheTooltipId : undefined"
          class="group inline-flex min-h-4 items-center py-1 text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus-ring)]"
          :class="
            cacheSummaryAvailable
              ? mobile
                ? 'w-full max-w-[168px]'
                : 'w-full max-w-[120px]'
              : mobile
                ? 'w-20 shrink-0'
                : 'w-14 shrink-0'
          "
          @pointerenter="showCacheTooltip"
          @pointerleave="hideCacheTooltip"
          @focus="showCacheTooltip"
          @blur="hideCacheTooltip"
          @click="toggleDetails"
        >
          <span
            class="log-cache-track relative inline-flex h-2.5 w-full overflow-hidden rounded-[5px] border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] transition-colors group-hover:border-[var(--outline)]"
          >
            <span
              v-if="hasCacheRead"
              data-log-usage-cache-fill
              class="log-cache-fill h-full rounded-[4px] bg-[var(--accent)] transition-[width] duration-300"
              :style="{
                width: `${cacheFillPercent}%`,
                minWidth: '10px',
              }"
            />
          </span>
        </button>
        <span v-if="!cacheSummaryAvailable" class="whitespace-nowrap">{{
          t('logs.cacheUnavailable')
        }}</span>
      </div>
    </div>

    <button
      ref="triggerRef"
      type="button"
      data-log-usage-trigger
      :aria-controls="popoverId"
      :aria-expanded="detailsOpen"
      :aria-label="t('logs.viewUsageDetails')"
      class="inline-flex h-6 w-6 shrink-0 self-center items-center justify-center rounded-full text-[var(--text-tertiary)] transition-colors duration-100 hover:bg-[var(--surface-hover)] hover:text-[var(--text-secondary)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
      @click="toggleDetails"
    >
      <Info :size="15" stroke-width="1.9" aria-hidden="true" />
    </button>
  </div>

  <span v-else data-log-usage-empty class="text-xs text-[var(--text-tertiary)]">
    —
  </span>

  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-100 ease-out"
      enter-from-class="translate-y-1 scale-95 opacity-0"
      leave-active-class="transition duration-75 ease-in"
      leave-to-class="translate-y-1 scale-95 opacity-0"
    >
      <section
        v-if="detailsOpen"
        :id="popoverId"
        ref="popoverRef"
        data-log-usage-popover
        data-handdrawn="menu"
        role="dialog"
        aria-modal="false"
        :aria-labelledby="popoverTitleId"
        class="fixed z-[var(--z-popover)] w-64 rounded-lg border border-[var(--overlay-border)] bg-[var(--surface-overlay)] p-3 shadow-[var(--overlay-shadow)]"
        :class="
          popoverPosition.placement === 'above' ? '-translate-y-full' : ''
        "
        :style="popoverStyle"
      >
        <h4
          :id="popoverTitleId"
          class="text-sm font-semibold text-[var(--text-primary)]"
        >
          {{ t('logs.usageDetails') }}
        </h4>

        <dl class="mt-2.5 sibling-space-y-2 text-xs">
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <ArrowDown
                :size="14"
                class="text-[var(--status-success-text)]"
                aria-hidden="true"
              />
              {{ t('logs.inputTokens') }}
            </dt>
            <dd
              class="font-mono font-medium tabular-nums text-[var(--text-primary)]"
            >
              {{ formatTokenValue(usage.promptTokens) }}
            </dd>
          </div>
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <ArrowUp
                :size="14"
                class="text-[var(--accent)]"
                aria-hidden="true"
              />
              {{ t('logs.outputTokens') }}
            </dt>
            <dd
              class="font-mono font-medium tabular-nums text-[var(--text-primary)]"
            >
              {{ formatTokenValue(usage.completionTokens) }}
            </dd>
          </div>
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <PenLine
                :size="14"
                class="text-[var(--status-warning-text)]"
                aria-hidden="true"
              />
              {{
                t(
                  usage.cacheWrite12hTokens === null
                    ? 'logs.cacheCreation'
                    : 'logs.cacheCreation30m'
                )
              }}
            </dt>
            <dd
              class="flex items-center gap-1.5 font-mono font-medium tabular-nums text-[var(--text-primary)]"
            >
              {{
                formatTokenValue(
                  usage.cacheWrite12hTokens === null
                    ? usage.cacheWriteTokens
                    : usage.cacheWrite30mTokens
                )
              }}
              <span
                v-if="usage.cacheTtl"
                class="rounded-sm border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-1 py-0.5 text-[0.625rem] font-medium leading-none text-[var(--status-warning-text)]"
              >
                {{ usage.cacheTtl }}
              </span>
            </dd>
          </div>
          <div
            v-if="usage.cacheWrite12hTokens !== null"
            class="flex items-center justify-between gap-4"
            data-log-cache-write-12h
          >
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <PenLine
                :size="14"
                class="text-[var(--status-warning-text)]"
                aria-hidden="true"
              />
              {{ t('logs.cacheCreation12h') }}
            </dt>
            <dd
              class="font-mono font-medium tabular-nums text-[var(--text-primary)]"
            >
              {{ formatTokenValue(usage.cacheWrite12hTokens) }}
            </dd>
          </div>
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <Database
                :size="14"
                class="text-[var(--status-info-text)]"
                aria-hidden="true"
              />
              {{ t('logs.cacheReadTokens') }}
            </dt>
            <dd
              class="font-mono font-medium tabular-nums text-[var(--text-primary)]"
            >
              {{ formatTokenValue(usage.cacheReadTokens) }}
            </dd>
          </div>
          <div class="sibling-space-y-1.5">
            <div class="flex items-center justify-between gap-4">
              <dt
                data-log-cache-hit-rate
                class="flex items-center gap-1.5 text-[var(--text-tertiary)]"
              >
                <Gauge
                  :size="14"
                  class="text-[var(--status-info-text)]"
                  aria-hidden="true"
                />
                {{ t('logs.cacheHitRate') }}
              </dt>
              <dd
                class="font-mono font-medium tabular-nums text-[var(--text-primary)]"
              >
                {{ cacheHitRateLabel }}
              </dd>
            </div>
            <div
              class="h-2.5 w-full overflow-hidden rounded-[5px] border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)]"
              aria-hidden="true"
            >
              <div
                v-if="hasCacheRead"
                class="h-full rounded-[4px] bg-[var(--accent)]"
                :style="{ width: `${cacheFillPercent}%`, minWidth: '10px' }"
              />
            </div>
          </div>
          <div
            v-if="metadata.fastMode"
            class="flex items-center justify-between gap-4"
            data-log-fast-mode
          >
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <Zap
                :size="14"
                class="text-[var(--accent-text)]"
                aria-hidden="true"
              />
              {{ t('logs.fastMode') }}
            </dt>
            <dd class="font-medium text-[var(--accent-text)]">
              {{ t('logs.fastEnabled') }}
            </dd>
          </div>
          <div
            class="flex items-center justify-between gap-4 border-t border-[var(--border-subtle)] pt-2"
          >
            <dt class="font-medium text-[var(--text-secondary)]">
              {{ t('logs.totalTokens') }}
            </dt>
            <dd
              class="font-mono font-semibold tabular-nums text-[var(--accent-text)]"
            >
              {{ formatNumber(usage.totalTokens) }}
            </dd>
          </div>
        </dl>
      </section>
    </Transition>

    <Transition
      enter-active-class="transition duration-100 ease-out"
      enter-from-class="translate-y-1 scale-95 opacity-0"
      leave-active-class="transition duration-75 ease-in"
      leave-to-class="translate-y-1 scale-95 opacity-0"
    >
      <div
        v-if="cacheTooltip && !detailsOpen"
        :id="cacheTooltipId"
        data-log-usage-cache-tooltip
        role="tooltip"
        data-handdrawn="toast"
        class="pointer-events-none fixed z-[var(--z-tooltip)] min-w-44 -translate-x-1/2 -translate-y-full rounded-lg border border-[var(--overlay-border)] bg-[var(--surface-overlay)] px-3 py-2 text-xs shadow-[var(--overlay-shadow)]"
        :style="cacheTooltipStyle"
      >
        <dl v-if="cacheSummaryAvailable" class="sibling-space-y-1.5">
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <Database
                :size="12"
                class="text-[var(--status-info-text)]"
                aria-hidden="true"
              />
              {{ t('logs.cacheRead') }}
            </dt>
            <dd
              class="font-mono font-medium tabular-nums text-[var(--status-info-text)]"
            >
              {{ formatTokenValue(usage.cacheReadTokens) }}
            </dd>
          </div>
          <div class="flex items-center justify-between gap-4">
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <PenLine
                :size="12"
                class="text-[var(--status-warning-text)]"
                aria-hidden="true"
              />
              {{ t('logs.cacheWrite') }}
            </dt>
            <dd
              class="flex items-center gap-1 font-mono font-medium tabular-nums text-[var(--status-warning-text)]"
            >
              <span>{{ formatTokenValue(usage.cacheWriteTokens) }}</span>
              <span
                v-if="usage.cacheTtl"
                class="rounded-sm border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-1 py-0.5 text-[0.625rem] leading-none"
              >
                {{ usage.cacheTtl }}
              </span>
            </dd>
          </div>
          <div
            class="flex items-center justify-between gap-4 border-t border-[var(--border-subtle)] pt-1.5"
          >
            <dt class="flex items-center gap-1.5 text-[var(--text-tertiary)]">
              <Gauge
                :size="12"
                class="text-[var(--accent-text)]"
                aria-hidden="true"
              />
              {{ t('logs.cacheHitRate') }}
            </dt>
            <dd
              class="font-mono font-semibold tabular-nums text-[var(--accent-text)]"
            >
              {{ cacheHitRateLabel }}
            </dd>
          </div>
        </dl>
        <p v-else class="whitespace-nowrap text-[var(--text-tertiary)]">
          {{ t('logs.cacheUnavailableAria') }}
        </p>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
html.dark .log-cache-track {
  background: var(--surface-container-lowest);
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.35);
}

html.dark .log-cache-fill {
  box-shadow: 0 0 6px rgba(228, 192, 107, 0.25);
}
</style>
