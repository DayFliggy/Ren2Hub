<script setup lang="ts">
const model = defineModel<boolean>({ default: false })

withDefaults(
  defineProps<{
    disabled?: boolean
    label?: string
  }>(),
  { disabled: false, label: '' }
)
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="model"
    :aria-label="label"
    :disabled="disabled"
    class="relative h-6 w-11 shrink-0 rounded-full transition-colors duration-200 focus-ring disabled:cursor-not-allowed disabled:opacity-50"
    :style="{ background: model ? 'var(--accent)' : 'var(--border-default)' }"
    @click="model = !model"
  >
    <span
      class="absolute top-0.5 h-5 w-5 rounded-full bg-[var(--surface-solid)] shadow transition-all duration-200"
      :style="{ left: model ? 'calc(100% - 22px)' : '2px' }"
    />
  </button>
</template>
