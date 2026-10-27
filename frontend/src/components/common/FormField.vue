<script setup lang="ts">
import { useId } from 'vue'

withDefaults(
  defineProps<{
    label?: string
    hint?: string
    /** Compound content (option buttons, comboboxes, pickers). A wrapping
        <label> would forward clicks on its gaps or popup rows to the first
        button, so it becomes a named group instead. */
    group?: boolean
  }>(),
  { label: '', hint: '', group: false }
)

const labelId = useId()
</script>

<template>
  <component
    :is="group ? 'div' : 'label'"
    class="block"
    :role="group ? 'group' : undefined"
    :aria-labelledby="group && label ? labelId : undefined"
  >
    <span
      v-if="label"
      :id="labelId"
      class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
    >
      {{ label }}
    </span>
    <slot />
    <span
      v-if="hint"
      class="mt-1.5 block text-xs text-[var(--text-tertiary)]"
      >{{ hint }}</span
    >
  </component>
</template>
