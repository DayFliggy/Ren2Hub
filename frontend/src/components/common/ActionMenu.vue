<script setup lang="ts">
/**
 * Overflow ("⋯") action menu. The panel is teleported and fixed-positioned so
 * scrolling table regions cannot clip it; any scroll or resize closes it.
 */
import { nextTick, ref, useId, watch } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import { MoreHorizontal } from 'lucide-vue-next'
import type { Component } from 'vue'

export interface ActionMenuItem {
  key: string
  label: string
  icon?: Component
  tone?: 'default' | 'danger'
  disabled?: boolean
  /** Draw a divider above this item. */
  separated?: boolean
}

const props = withDefaults(
  defineProps<{
    items: ActionMenuItem[]
    label: string
    variant?: 'ghost' | 'outline'
    disabled?: boolean
  }>(),
  { variant: 'ghost', disabled: false }
)
const emit = defineEmits<{ select: [key: string] }>()

const menuId = useId()
const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const position = ref({ top: 0, left: 0 })

function menuButtons(): HTMLButtonElement[] {
  return [
    ...(panel.value?.querySelectorAll<HTMLButtonElement>(
      '[role="menuitem"]:not([disabled])'
    ) ?? []),
  ]
}

async function show() {
  if (props.disabled || !trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  position.value = { top: rect.bottom + 6, left: rect.left }
  open.value = true
  await nextTick()
  const box = panel.value?.getBoundingClientRect()
  if (box) {
    // Align to the trigger's right edge, inside the viewport; open upward
    // when there is no room below.
    const left = Math.max(
      8,
      Math.min(rect.right - box.width, innerWidth - box.width - 8)
    )
    const top =
      rect.bottom + 6 + box.height > innerHeight - 8
        ? Math.max(8, rect.top - box.height - 6)
        : rect.bottom + 6
    position.value = { top, left }
  }
  menuButtons()[0]?.focus({ preventScroll: true })
}

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}

function choose(item: ActionMenuItem) {
  if (props.disabled || item.disabled) return
  close(true)
  emit('select', item.key)
}

function onKeydown(event: KeyboardEvent) {
  const list = menuButtons()
  const index = list.indexOf(document.activeElement as HTMLButtonElement)
  if (event.key === 'Escape' || event.key === 'Tab') {
    event.preventDefault()
    close(true)
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    list[(index + step + list.length) % list.length]?.focus()
  } else if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    list[event.key === 'Home' ? 0 : list.length - 1]?.focus()
  }
}

function dismiss() {
  close()
}

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) close()
  }
)
onClickOutside(panel, dismiss, { ignore: [trigger] })
useEventListener(window, 'resize', dismiss)
useEventListener(window, 'scroll', dismiss, { capture: true, passive: true })
</script>

<template>
  <span class="inline-flex">
    <button
      ref="trigger"
      type="button"
      :aria-label="label"
      :title="label"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-controls="open ? menuId : undefined"
      :disabled="disabled"
      class="inline-flex items-center justify-center text-[var(--text-tertiary)] transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-40 enabled:hover:text-[var(--text-primary)]"
      :class="
        variant === 'outline'
          ? 'pencil-control h-9 w-9 rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)] enabled:hover:bg-[var(--surface-hover)]'
          : 'h-11 w-11 rounded-lg enabled:hover:bg-[var(--state-hover-layer)] sm:h-8 sm:w-8'
      "
      data-handdrawn="control"
      @click="open ? close() : show()"
    >
      <MoreHorizontal :size="17" aria-hidden="true" />
    </button>
    <Teleport to="body">
      <div
        v-if="open"
        :id="menuId"
        ref="panel"
        role="menu"
        :aria-label="label"
        class="fixed z-[300] min-w-44 rounded-lg border border-[var(--overlay-border)] bg-[var(--surface-overlay)] py-1.5"
        :style="{ top: `${position.top}px`, left: `${position.left}px` }"
        data-handdrawn="menu"
        @keydown="onKeydown"
      >
        <template v-for="item in items" :key="item.key">
          <div
            v-if="item.separated"
            class="my-1 border-t border-[var(--border-subtle)]"
            role="separator"
          />
          <button
            type="button"
            role="menuitem"
            :disabled="item.disabled"
            class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-40"
            :class="
              item.tone === 'danger'
                ? 'text-[var(--status-danger-text)] enabled:hover:bg-[var(--status-danger-soft)]'
                : 'text-[var(--text-primary)] enabled:hover:bg-[var(--surface-hover)]'
            "
            @click="choose(item)"
          >
            <component
              :is="item.icon"
              v-if="item.icon"
              :size="15"
              aria-hidden="true"
            />
            <span>{{ item.label }}</span>
          </button>
        </template>
      </div>
    </Teleport>
  </span>
</template>
