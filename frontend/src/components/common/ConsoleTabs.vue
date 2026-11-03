<script setup lang="ts">
export interface TabItem {
  key: string
  label: string
}

const model = defineModel<string>({ default: '' })

const props = withDefaults(
  defineProps<{
    items: TabItem[]
    panelId: string
    density?: 'default' | 'compact'
  }>(),
  { density: 'default' }
)

function onKeydown(e: KeyboardEvent, index: number) {
  let next: number
  if (e.key === 'ArrowRight') {
    next = index === props.items.length - 1 ? 0 : index + 1
  } else if (e.key === 'ArrowLeft') {
    next = index === 0 ? props.items.length - 1 : index - 1
  } else if (e.key === 'Home') {
    next = 0
  } else if (e.key === 'End') {
    next = props.items.length - 1
  } else {
    return
  }

  const item = props.items[next]
  if (!item) return
  e.preventDefault()
  model.value = item.key
  const tabs = (
    e.currentTarget as HTMLElement
  ).parentElement?.querySelectorAll<HTMLElement>('[role="tab"]')
  tabs?.[next]?.focus()
}
</script>

<template>
  <div
    class="console-tabs flex items-center border-b border-[var(--border-subtle)]"
    :class="density === 'compact' ? 'gap-4' : 'gap-7'"
    role="tablist"
    data-handdrawn="tabs"
  >
    <button
      v-for="(item, i) in items"
      :id="`${panelId}-tab-${item.key}`"
      :key="item.key"
      type="button"
      role="tab"
      :aria-controls="panelId"
      :aria-selected="model === item.key"
      :tabindex="model === item.key ? 0 : -1"
      class="relative inline-flex shrink-0 items-center whitespace-nowrap font-medium transition-colors lg:min-h-10"
      :class="[
        density === 'compact'
          ? 'min-h-11 pb-2 text-xs'
          : 'min-h-11 pb-3 text-sm',
        model === item.key
          ? 'text-[var(--text-primary)]'
          : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]',
      ]"
      @click="model = item.key"
      @keydown="onKeydown($event, i)"
    >
      <!-- brush-highlight on active label -->
      <span
        :class="model === item.key ? 'brush-highlight' : ''"
        style="position: relative; z-index: 0"
        >{{ item.label }}</span
      >

      <!-- Active indicator: tapered brush-stroke bar (wider center, thin ends) -->
      <span
        v-if="model === item.key"
        class="active-bar absolute inset-x-0 bottom-0"
        aria-hidden="true"
      />
    </button>
  </div>
</template>

<style scoped>
.console-tabs {
  max-width: 100%;
  overflow-x: auto;
  overscroll-behavior-inline: contain;
  scrollbar-width: none;
}
.console-tabs::-webkit-scrollbar {
  display: none;
}

.console-tabs [role='tab']:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: -2px;
}

/* Day: tapered brush-stroke bar (clip). Night: full-width gold bar + glow. */
.active-bar {
  height: 2.5px;
  background: var(--accent);
  border-radius: 9999px;
  clip-path: var(--tab-bar-clip, none);
  box-shadow: var(--tab-bar-glow, none);
}

/* B1 Studio Dark Mode Tabs */
html.dark .console-tabs {
  scrollbar-width: thin;
  width: fit-content;
  border: 1px solid var(--outline-variant);
  border-radius: 12px;
  background: var(--studio-control-track, var(--surface-container-low));
  padding: 3px;
  gap: 3px;
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.22);
}

html.dark .console-tabs [role='tab'] {
  height: auto;
  min-height: 40px;
  padding: 6px 15px;
  border-radius: 8px;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--text-tertiary);
  transition:
    color 0.15s ease,
    background-color 0.15s ease;
}

@media (max-width: 1023px), (pointer: coarse) {
  .console-tabs [role='tab'] {
    min-height: 44px;
    min-width: 44px;
  }
  html.dark .console-tabs [role='tab'] {
    min-height: 44px;
    padding: 6px 12px;
  }
}

html.dark .console-tabs [role='tab']:hover:not([aria-selected='true']) {
  color: var(--text-secondary);
  background: rgba(255, 255, 255, 0.035);
}

html.dark .console-tabs [role='tab'][aria-selected='true'] {
  background: var(--studio-control-thumb, var(--surface-container-highest));
  color: var(--text-primary);
  border: 1px solid rgba(255, 255, 255, 0.07);
  box-shadow: var(
    --studio-control-thumb-shadow,
    inset 0 1px 0 rgba(255, 255, 255, 0.1),
    0 2px 6px rgba(4, 7, 14, 0.32)
  );
}

html.dark .console-tabs .active-bar,
html.dark .console-tabs :deep(.brush-highlight::before),
html.dark .console-tabs :deep(.brush-highlight::after) {
  display: none !important;
}
</style>
