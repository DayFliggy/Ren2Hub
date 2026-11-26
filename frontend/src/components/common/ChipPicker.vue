<script setup lang="ts">
import { onClickOutside } from '@vueuse/core'
import { computed, nextTick, onBeforeUnmount, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'

import { useFloatingPanel } from '@/composables/useFloatingPanel'

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    options: string[]
    placeholder?: string
    max?: number
    /** Enter adds the typed text as a chip of its own. */
    creatable?: boolean
    disabled?: boolean
  }>(),
  { placeholder: '', max: 0, creatable: false, disabled: false }
)

const model = defineModel<string[]>({ default: () => [] })

const keyword = ref('')
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const field = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const inputId = useId()
let blurTimer: number | undefined

const filtered = computed(() => {
  const kw = keyword.value.toLowerCase()
  return props.options
    .filter((o) => !model.value.includes(o))
    .filter((o) => (kw ? o.toLowerCase().includes(kw) : true))
    .slice(0, 8)
})

function add(option: string) {
  if (props.disabled) return
  if (props.max > 0 && model.value.length >= props.max) return
  model.value = [...model.value, option]
  keyword.value = ''
}

function remove(option: string) {
  if (props.disabled) return
  model.value = model.value.filter((o) => o !== option)
}

function close() {
  if (blurTimer) window.clearTimeout(blurTimer)
  blurTimer = undefined
  open.value = false
}

// Touch scrolling or tapping elsewhere may not blur the input, so outside taps
// close the panel directly; blur still covers keyboard Tab navigation.
onClickOutside(root, close, { ignore: [panel] })

const showPanel = computed(
  () => open.value && !props.disabled && filtered.value.length > 0
)
const { style: panelStyle, scope: panelScope } = useFloatingPanel(
  field,
  panel,
  showPanel,
  { width: 'exact', gap: 6 }
)

/** Closes once focus has left both the input and the teleported options. */
function onBlur() {
  if (blurTimer) window.clearTimeout(blurTimer)
  blurTimer = window.setTimeout(() => {
    blurTimer = undefined
    const active = document.activeElement
    if (root.value?.contains(active) || panel.value?.contains(active)) return
    open.value = false
  }, 150)
}

function optionButtons(): HTMLButtonElement[] {
  return [...(panel.value?.querySelectorAll('button') ?? [])]
}

/** The options sit at the end of <body>, so the input hands focus over. */
function onInputKeydown(event: KeyboardEvent) {
  if (props.creatable && event.key === 'Enter' && !event.isComposing) {
    const typed = keyword.value.trim()
    if (!typed) return
    event.preventDefault()
    if (model.value.includes(typed)) keyword.value = ''
    else add(typed)
    return
  }
  const enters =
    event.key === 'ArrowDown' || (event.key === 'Tab' && !event.shiftKey)
  if (!enters || !showPanel.value || event.isComposing) return
  event.preventDefault()
  optionButtons()[0]?.focus()
}

function returnToInput(keepOpen: boolean) {
  input.value?.focus()
  // Focusing the input reopens the panel; Escape and Tab close it again.
  if (!keepOpen) close()
}

async function onOptionKeydown(event: KeyboardEvent, index: number) {
  const buttons = optionButtons()
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    buttons[(index + step + buttons.length) % buttons.length]?.focus()
  } else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    add(filtered.value[index]!)
    await nextTick()
    const next = optionButtons()
    if (next.length) next[Math.min(index, next.length - 1)]!.focus()
    else returnToInput(false)
  } else if (event.key === 'Escape' || event.key === 'Tab') {
    // Escape must not also close an enclosing dialog.
    event.preventDefault()
    event.stopPropagation()
    returnToInput(event.key === 'Tab' && event.shiftKey)
  }
}

function onFocus() {
  if (blurTimer) window.clearTimeout(blurTimer)
  blurTimer = undefined
  open.value = true
}

onBeforeUnmount(() => {
  if (blurTimer) window.clearTimeout(blurTimer)
})
</script>

<template>
  <div ref="root" class="relative">
    <div ref="field" class="pencil-control" data-handdrawn="control">
      <input
        :id="inputId"
        ref="input"
        v-model="keyword"
        type="text"
        name="chip-picker-search"
        :aria-label="placeholder"
        :placeholder="placeholder"
        :disabled="disabled"
        class="h-11 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--studio-control-track,var(--surface-solid))] px-4 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
        @focus="onFocus"
        @click="onFocus"
        @blur="onBlur"
        @keydown="onInputKeydown"
      />
    </div>

    <Teleport to="body">
      <div
        v-if="showPanel"
        ref="panel"
        class="subtle-scroll max-h-56 overflow-y-auto rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] py-1"
        :style="panelStyle"
        data-handdrawn="menu"
        data-floating-panel
        :data-handdrawn-scope="panelScope"
        @focusout="onBlur"
      >
        <button
          v-for="(option, index) in filtered"
          :key="option"
          type="button"
          class="block w-full px-4 py-2 text-left text-sm text-[var(--text-primary)] transition-colors hover:bg-[var(--state-hover-layer)]"
          tabindex="-1"
          @pointerdown.prevent="add(option)"
          @keydown="onOptionKeydown($event, index)"
        >
          {{ option }}
        </button>
      </div>
    </Teleport>

    <div v-if="model.length > 0" class="mt-2.5 flex flex-wrap gap-2">
      <span
        v-for="option in model"
        :key="option"
        class="relative inline-flex items-center rounded-lg bg-[var(--surface-muted)] py-1.5 pl-3 pr-7 text-xs font-medium text-[var(--text-primary)]"
        data-handdrawn="chip"
      >
        {{ option }}
        <button
          type="button"
          class="chip-picker-remove absolute -right-1.5 -top-1.5 flex items-center justify-center rounded-full bg-[var(--text-primary)] text-[var(--surface-solid)] focus-ring"
          style="height: 18px; width: 18px"
          :aria-label="t('common.removeItem', { item: option })"
          :disabled="disabled"
          @click="remove(option)"
        >
          <svg
            width="10"
            height="10"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="3"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </span>
    </div>
  </div>
</template>

<style scoped>
/* Keep the compact 18px badge while giving touch/narrow viewports a 44px hit box. */
.chip-picker-remove::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  min-width: 44px;
  min-height: 44px;
  transform: translate(-50%, -50%);
}
@media (min-width: 1024px) and (pointer: fine) {
  .chip-picker-remove::after {
    min-width: 0;
    min-height: 0;
  }
}
</style>
