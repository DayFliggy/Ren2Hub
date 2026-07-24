<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue'

defineOptions({ inheritAttrs: false })

const model = defineModel<number | null>({ default: null })

const props = withDefaults(
  defineProps<{
    placeholder?: string
    min?: number
    max?: number
  }>(),
  { placeholder: '', min: undefined, max: undefined }
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

function onInput(e: Event) {
  const input = e.target as HTMLInputElement
  const allowNegative = Number.isFinite(props.min) && Number(props.min) < 0
  const raw = input.value
    .replace(allowNegative ? /[^0-9.-]/g : /[^0-9.]/g, '')
    .replace(/(?!^)-/g, '')
    .replace(/(\..*)\./g, '$1')
  input.value = raw

  if (raw === '' || raw === '-' || raw === '.' || raw === '-.') {
    model.value = null
    return
  }

  const parsed = Number(raw)
  if (!Number.isFinite(parsed)) {
    model.value = null
    return
  }

  const minimum = Number.isFinite(props.min) ? props.min : undefined
  const maximum = Number.isFinite(props.max) ? props.max : undefined
  let bounded = parsed
  if (minimum !== undefined) bounded = Math.max(minimum, bounded)
  if (maximum !== undefined) bounded = Math.min(maximum, bounded)
  if (bounded !== parsed) input.value = String(bounded)
  model.value = bounded
}
</script>

<template>
  <div class="relative" :class="attrs.class" :style="wrapperStyle">
    <span
      class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-sm text-[var(--text-tertiary)]"
      >$</span
    >
    <input
      :id="inputId"
      :value="model ?? ''"
      inputmode="decimal"
      :min="min"
      :max="max"
      :placeholder="placeholder"
      v-bind="inputAttrs"
      class="h-11 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] pl-8 pr-4 text-sm font-semibold text-[var(--text-primary)] placeholder:font-normal placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
      @input="onInput"
    />
  </div>
</template>
