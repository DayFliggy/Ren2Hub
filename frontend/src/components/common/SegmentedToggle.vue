<script setup lang="ts">
/**
 * Compact segmented pill (2+ options), sharing the visual language of the
 * grid/list view toggle in ModelsView: a muted track with the active segment
 * lifted onto the solid surface. Used for the buy/sell switch and view mode.
 */
import { nextTick } from 'vue'

export interface SegmentOption {
  value: string
  label?: string
  /** accessible label + tooltip when the segment is icon-only */
  ariaLabel?: string
  /** lucide 24×24 path — renders an icon instead of / alongside the label */
  icon?: string
}

const model = defineModel<string>({ required: true })

const props = withDefaults(
  defineProps<{
    options: SegmentOption[]
    /** stable accessible name for the whole group */
    label: string
    size?: 'sm' | 'md'
  }>(),
  { size: 'md' }
)

async function onKeydown(event: KeyboardEvent, index: number) {
  const last = props.options.length - 1
  let next: number
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
    next = index === last ? 0 : index + 1
  } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
    next = index === 0 ? last : index - 1
  } else if (event.key === 'Home') {
    next = 0
  } else if (event.key === 'End') {
    next = last
  } else {
    return
  }

  const option = props.options[next]
  if (!option) return
  event.preventDefault()
  const group = (event.currentTarget as HTMLElement).parentElement
  model.value = option.value
  // A parent may refuse the change (e.g. an unsaved-draft guard). Focus
  // follows the value that is actually selected after the update settles.
  await nextTick()
  const selected = props.options.findIndex((item) => item.value === model.value)
  group?.querySelectorAll<HTMLElement>('[role="radio"]')[selected]?.focus()
}
</script>

<template>
  <div
    class="inline-flex min-w-0 max-w-full items-center gap-1 overflow-x-auto overscroll-x-contain rounded-xl bg-[var(--surface-muted)] p-1"
    role="radiogroup"
    :aria-label="label"
    data-handdrawn="segments"
  >
    <button
      v-for="(opt, index) in options"
      :key="opt.value"
      type="button"
      role="radio"
      :aria-checked="model === opt.value"
      :tabindex="model === opt.value ? 0 : -1"
      :aria-label="opt.ariaLabel"
      :title="opt.ariaLabel"
      class="inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap font-semibold transition-all focus-ring touch-snappy"
      :class="[
        size === 'sm'
          ? 'h-11 px-2.5 text-xs lg:h-8'
          : 'h-11 px-3.5 text-sm lg:h-10',
        opt.label
          ? ''
          : size === 'sm'
            ? 'w-11 px-0 lg:w-8'
            : 'w-11 px-0 lg:w-10',
        model === opt.value
          ? 'seg-active text-[var(--text-primary)]'
          : 'text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--state-hover-layer)] rounded-lg',
      ]"
      @click="model = opt.value"
      @keydown="onKeydown($event, index)"
    >
      <svg
        v-if="opt.icon"
        :width="size === 'sm' ? 15 : 16"
        :height="size === 'sm' ? 15 : 16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path :d="opt.icon" />
      </svg>
      <span v-if="opt.label">{{ opt.label }}</span>
    </button>
  </div>
</template>

<style scoped>
/* Active segment: day = outlined hand-drawn tile; night = filled + elevation */
.seg-active {
  background: var(--surface-solid);
  border-radius: var(--sketch-border-radius-sm);
  box-shadow: var(--elevation-1);
  border: 1px solid var(--border-default);
}

/* B1 Studio Dark Mode Segmented Controls */
html.dark [data-handdrawn='segments'] {
  padding: 3px;
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
  background: var(--studio-control-track, var(--surface-container-low));
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.22);
}

html.dark [data-handdrawn='segments'] button {
  border-radius: 8px;
  font-size: 0.8125rem;
  transition:
    color 0.15s ease,
    background-color 0.15s ease;
}

html.dark [data-handdrawn='segments'] button:hover:not(.seg-active) {
  color: var(--text-secondary);
  background: rgba(255, 255, 255, 0.035);
}

html.dark [data-handdrawn='segments'] .seg-active {
  background: var(--studio-control-thumb, var(--surface-container-highest));
  border: 1px solid rgba(255, 255, 255, 0.07);
  box-shadow: var(
    --studio-control-thumb-shadow,
    inset 0 1px 0 rgba(255, 255, 255, 0.1),
    0 2px 6px rgba(4, 7, 14, 0.32)
  );
  border-radius: 8px;
  color: var(--text-primary);
}

html.dark [data-handdrawn='segments'] :focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: -2px;
}
</style>
