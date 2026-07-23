<script setup lang="ts">
export interface TabItem {
  key: string
  label: string
}

const model = defineModel<string>({ default: '' })

const props = defineProps<{
  items: TabItem[]
}>()

// Left/Right arrows move selection (and focus) between tabs — the ARIA
// tabs keyboard pattern.
function onKeydown(e: KeyboardEvent, index: number) {
  if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
  e.preventDefault()
  const delta = e.key === 'ArrowRight' ? 1 : -1
  const next = (index + delta + props.items.length) % props.items.length
  model.value = props.items[next].key
  const tabs = (
    e.currentTarget as HTMLElement
  ).parentElement?.querySelectorAll<HTMLElement>('[role="tab"]')
  tabs?.[next]?.focus()
}
</script>

<template>
  <div
    class="flex items-center gap-7 border-b border-[var(--border-subtle)]"
    role="tablist"
  >
    <button
      v-for="(item, i) in items"
      :key="item.key"
      type="button"
      role="tab"
      :aria-selected="model === item.key"
      :tabindex="model === item.key ? 0 : -1"
      class="relative pb-3 text-sm font-medium transition-colors"
      :class="
        model === item.key
          ? 'text-[var(--text-primary)]'
          : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]'
      "
      @click="model = item.key"
      @keydown="onKeydown($event, i)"
    >
      {{ item.label }}
      <span
        v-if="model === item.key"
        class="absolute inset-x-0 -bottom-px h-0.5 rounded-full"
        style="background: var(--accent)"
      />
    </button>
  </div>
</template>
