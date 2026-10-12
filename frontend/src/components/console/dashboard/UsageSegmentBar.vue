<script setup lang="ts">
import { computed, nextTick, ref, useId, watch, type CSSProperties } from 'vue'

const props = withDefaults(
  defineProps<{
    title: string
    segments: Array<{
      key: string
      label: string
      value: number
      displayValue: string
      intensity: number
    }>
    color: string
    emptyLabel: string
    summary?: string
    layout?: 'ranked' | 'rhythm'
    /** Ranked rows kept in the layout even when fewer days are active. */
    reservedRows?: number
    /**
     * From the sm breakpoint the year view shows the bars as one band, so the
     * ranked rows give way to the summary; narrower cards stack the details.
     */
    compact?: boolean
  }>(),
  { summary: '', layout: 'ranked', reservedRows: 0, compact: false }
)

const root = ref<HTMLElement | null>(null)
const focusKey = ref('')
const tooltipId = useId()
const tooltip = ref<{
  key: string
  label: string
  left: number
  top: number
} | null>(null)

const visibleSegments = computed(() =>
  props.segments.filter((segment) => segment.value > 0)
)
/**
 * Ranked bars show each active day's share. The rhythm keeps all seven days in
 * equal slots, shaded by value, so each sits above its weekday label.
 */
const trackSegments = computed(() => {
  if (props.layout === 'ranked' || !visibleSegments.value.length) {
    return visibleSegments.value
  }
  return props.segments
})
const total = computed(() =>
  visibleSegments.value.reduce((sum, segment) => sum + segment.value, 0)
)
const placeholderRows = computed(() =>
  Math.max(0, props.reservedRows - props.segments.length)
)

function segmentStyle(segment: (typeof props.segments)[number]): CSSProperties {
  const background =
    segment.value > 0
      ? `color-mix(in srgb, ${props.color} ${segment.intensity}%, var(--surface-solid))`
      : 'transparent'
  if (props.layout === 'rhythm') return { flex: '1 1 0%', background }
  return {
    width: `${total.value ? (segment.value / total.value) * 100 : 0}%`,
    background,
  }
}

function showTooltip(
  event: FocusEvent | MouseEvent,
  segment: (typeof props.segments)[number]
) {
  if (!root.value) return
  const target = event.currentTarget as HTMLElement
  const targetRect = target.getBoundingClientRect()
  const rootRect = root.value.getBoundingClientRect()
  tooltip.value = {
    key: segment.key,
    label: `${segment.label} · ${segment.displayValue}`,
    left: Math.min(
      rootRect.width - 72,
      Math.max(72, targetRect.left - rootRect.left + targetRect.width / 2)
    ),
    top: targetRect.top - rootRect.top - 6,
  }
}

function hideTooltip(key: string) {
  if (tooltip.value?.key === key) tooltip.value = null
}

function onSegmentFocus(
  event: FocusEvent,
  segment: (typeof props.segments)[number]
) {
  focusKey.value = segment.key
  showTooltip(event, segment)
}

async function focusSegment(key: string) {
  const target = root.value?.querySelector<HTMLButtonElement>(
    `[data-usage-segment="${key}"]`
  )
  if (!target) return
  focusKey.value = key
  await nextTick()
  target.focus()
}

function onKeydown(event: KeyboardEvent, key: string) {
  const segments = trackSegments.value
  const current = segments.findIndex((segment) => segment.key === key)
  if (current < 0) return

  let next: number
  if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
    next = (current - 1 + segments.length) % segments.length
  } else if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
    next = (current + 1) % segments.length
  } else if (event.key === 'Home') {
    next = 0
  } else if (event.key === 'End') {
    next = segments.length - 1
  } else {
    return
  }
  event.preventDefault()
  const target = segments[next]
  if (target) void focusSegment(target.key)
}

watch(
  trackSegments,
  (segments) => {
    if (!segments.some((segment) => segment.key === focusKey.value)) {
      focusKey.value = segments[0]?.key ?? ''
    }
    tooltip.value = null
  },
  { immediate: true }
)
</script>

<template>
  <section
    ref="root"
    class="relative min-w-0"
    :data-usage-segment-bar="layout"
    :data-usage-segment-compact="compact || undefined"
  >
    <div
      class="flex min-w-0 items-center justify-between gap-2"
      data-usage-segment-title
    >
      <h3
        class="truncate text-[0.6875rem] font-medium text-[var(--text-tertiary)]"
      >
        {{ title }}
      </h3>
      <!-- A compact ranked summary stands in for the rows it hides from sm. -->
      <span
        v-if="summary || !visibleSegments.length"
        class="truncate text-right text-[0.5625rem] tabular-nums text-[var(--text-tertiary)]"
        :class="{
          'max-sm:hidden':
            compact && layout === 'ranked' && visibleSegments.length,
        }"
        data-usage-segment-summary
      >
        {{ visibleSegments.length ? summary : emptyLabel }}
      </span>
    </div>

    <!-- Track, rows and labels stay in place without data so heights match. -->
    <div
      class="mt-1 flex h-2 overflow-hidden rounded-full bg-[var(--surface-muted)]"
      role="group"
      :aria-label="title"
    >
      <button
        v-for="segment in trackSegments"
        :key="segment.key"
        type="button"
        class="h-full min-w-px border-r border-[var(--surface-solid)] transition-[filter] last:border-r-0 hover:brightness-110 focus-ring"
        :style="segmentStyle(segment)"
        :tabindex="focusKey === segment.key ? 0 : -1"
        :aria-label="`${segment.label} ${segment.displayValue}`"
        :aria-describedby="tooltip?.key === segment.key ? tooltipId : undefined"
        :data-usage-segment="segment.key"
        :data-usage-segment-share="
          total ? (segment.value / total).toFixed(6) : '0'
        "
        @focus="onSegmentFocus($event, segment)"
        @blur="hideTooltip(segment.key)"
        @mouseenter="showTooltip($event, segment)"
        @mouseleave="hideTooltip(segment.key)"
        @keydown="onKeydown($event, segment.key)"
      />
    </div>

    <div
      v-if="layout === 'ranked'"
      class="mt-1 space-y-0.5"
      :class="{ 'sm:hidden': compact }"
      data-usage-segment-details
    >
      <div
        v-for="segment in segments"
        :key="segment.key"
        class="grid grid-cols-[0.45rem_minmax(0,1fr)_auto] items-center gap-1.5 text-[0.5625rem]"
      >
        <span
          class="size-1.5 rounded-sm"
          :style="{
            background: `color-mix(in srgb, ${color} ${segment.intensity}%, var(--surface-solid))`,
          }"
          aria-hidden="true"
        />
        <span class="truncate text-[var(--text-secondary)]">
          {{ segment.label }}
        </span>
        <span class="font-mono tabular-nums text-[var(--text-primary)]">
          {{ segment.displayValue }}
        </span>
      </div>
      <div
        v-for="index in placeholderRows"
        :key="`placeholder-${index}`"
        class="grid grid-cols-[0.45rem_minmax(0,1fr)_auto] items-center gap-1.5 text-[0.5625rem] text-[var(--text-tertiary)]"
        aria-hidden="true"
        data-usage-segment-placeholder
      >
        <span class="size-1.5 rounded-sm bg-[var(--surface-muted)]" />
        <span>--</span>
      </div>
    </div>

    <!-- The year band keeps its labels inside the shared stage height. -->
    <div
      v-else
      class="grid grid-cols-7 text-center text-[0.5625rem] text-[var(--text-tertiary)]"
      :class="compact ? 'mt-1 sm:mt-0.5 sm:leading-none' : 'mt-1'"
      data-usage-segment-labels
      aria-hidden="true"
    >
      <span v-for="segment in segments" :key="segment.key" class="truncate">
        {{ segment.label }}
      </span>
    </div>

    <div
      v-if="tooltip"
      :id="tooltipId"
      class="pointer-events-none absolute z-20 -translate-x-1/2 -translate-y-full whitespace-nowrap rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-overlay)] px-2 py-1 text-[0.625rem] text-[var(--text-primary)] shadow-[var(--overlay-shadow)]"
      :style="{ left: `${tooltip.left}px`, top: `${tooltip.top}px` }"
      role="tooltip"
    >
      {{ tooltip.label }}
    </div>
  </section>
</template>
