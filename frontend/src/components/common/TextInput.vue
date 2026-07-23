<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue'

// Keep consumer `class`/`style` on the sizing wrapper, but forward everything
// else (e.g. `id`, `aria-*`) to the real <input> so label/focus targeting works.
defineOptions({ inheritAttrs: false })

const model = defineModel<string>({ default: '' })

withDefaults(
  defineProps<{
    type?: string
    placeholder?: string
    autocomplete?: string
    readonly?: boolean
  }>(),
  { type: 'text', placeholder: '', autocomplete: 'off', readonly: false }
)

const attrs = useAttrs()
const fallbackId = useId()
const inputId = computed(() => String(attrs.id ?? fallbackId))
const wrapperStyle = computed(() => attrs.style)
const inputAttrs = computed(() => {
  const rest = { ...attrs }
  delete rest.class
  delete rest.style
  delete rest.id
  return rest
})
</script>

<template>
  <div class="relative" :class="attrs.class" :style="wrapperStyle">
    <div
      v-if="$slots.icon"
      class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-tertiary)]"
    >
      <slot name="icon" />
    </div>
    <input
      :id="inputId"
      v-model="model"
      :type="type"
      :placeholder="placeholder"
      :autocomplete="autocomplete"
      :readonly="readonly"
      v-bind="inputAttrs"
      class="h-11 w-full rounded-xl border border-[var(--border-subtle)] pr-4 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
      :class="[
        $slots.icon ? 'pl-10' : 'pl-4',
        readonly
          ? 'cursor-not-allowed bg-[var(--surface-muted)] text-[var(--text-secondary)]'
          : 'bg-[var(--surface-solid)]',
      ]"
    />
  </div>
</template>
