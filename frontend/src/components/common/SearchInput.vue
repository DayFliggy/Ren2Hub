<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue'

defineOptions({ inheritAttrs: false })

const model = defineModel<string>({ default: '' })

withDefaults(
  defineProps<{
    placeholder?: string
    /** 'compact' matches the 32px toolbar buttons from the sm breakpoint and
        keeps the 44px touch target below it. */
    size?: 'md' | 'compact'
  }>(),
  { placeholder: '', size: 'md' }
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
  <div
    class="pencil-underline relative"
    :class="attrs.class"
    :style="wrapperStyle"
    data-handdrawn="underline"
  >
    <svg
      class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-tertiary)]"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
    <input
      :id="inputId"
      v-model="model"
      type="search"
      :placeholder="placeholder"
      v-bind="inputAttrs"
      class="w-full border-0 border-b-[1.5px] bg-transparent pl-10 pr-4 text-base leading-5 text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-[border-color,background-color,box-shadow] focus:outline-hidden focus:border-[var(--accent)] hover:bg-[var(--state-hover-layer)] sm:text-sm"
      :class="size === 'compact' ? 'h-11 sm:h-8' : 'h-11'"
      style="border-color: var(--border-default)"
    />
  </div>
</template>
