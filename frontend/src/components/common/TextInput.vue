<script setup lang="ts">
import { computed, ref, useAttrs, useId } from 'vue'

// Keep consumer `class`/`style` on the sizing wrapper, but forward everything
// else (e.g. `id`, `aria-*`) to the real <input> so label/focus targeting works.
defineOptions({ inheritAttrs: false })

const model = defineModel<string>({ default: '' })
const composing = ref(false)
const compositionValue = ref('')
const inputValue = computed(() =>
  composing.value ? compositionValue.value : model.value
)

// Native v-model coerces number inputs before emitting. Keep this component's
// string contract and decimal precision while retaining composition buffering.
function onInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  if (composing.value) compositionValue.value = value
  else model.value = value
}

function startComposition(event: CompositionEvent) {
  compositionValue.value = (event.target as HTMLInputElement).value
  composing.value = true
}

function finishComposition(event: Event) {
  if (!composing.value) return
  composing.value = false
  onInput(event)
}

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
  <div
    class="pencil-underline relative"
    :class="attrs.class"
    :style="wrapperStyle"
    data-handdrawn="underline"
  >
    <div
      v-if="$slots.icon"
      class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-tertiary)]"
    >
      <slot name="icon" />
    </div>
    <input
      :id="inputId"
      :value="inputValue"
      :type="type"
      :placeholder="placeholder"
      :autocomplete="autocomplete"
      :readonly="readonly"
      v-bind="inputAttrs"
      class="h-11 w-full border-0 border-b-[1.5px] bg-transparent pr-4 text-base leading-[1.375rem] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-[border-color,background-color,box-shadow] focus:outline-hidden focus:border-[var(--accent)] sm:text-[0.9375rem]"
      style="border-color: var(--border-default)"
      :class="[
        $slots.icon ? 'pl-10' : 'pl-4',
        readonly ? 'cursor-not-allowed opacity-60' : '',
      ]"
      @input="onInput"
      @compositionstart="startComposition"
      @compositionend="finishComposition"
      @change="finishComposition"
      @blur="finishComposition"
    />
  </div>
</template>
