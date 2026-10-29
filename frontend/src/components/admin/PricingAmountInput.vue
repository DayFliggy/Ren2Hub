<script setup lang="ts">
import { ref, watch } from 'vue'
import { divideDecimal, isDecimal, multiplyDecimal } from '@/utils/decimal'
// The parent owns the USD amount; this input owns only the text typed in the
// display currency. Switching currency therefore never rewrites a price, and
// an exact USD value survives any number of round trips.
const props = withDefaults(
  defineProps<{ modelValue: string; rate?: string }>(),
  { rate: '1' }
)
const emit = defineEmits<{ 'update:modelValue': [usd: string] }>()
const text = ref('')
let written: string | undefined
function display(usd: string, rate: string) {
  return rate === '1' || !isDecimal(usd) ? usd : multiplyDecimal(usd, rate)
}
watch(
  () => [props.modelValue, props.rate] as const,
  ([usd, rate], previous) => {
    // Our own write keeps the typed text, which may differ from the rounded
    // USD amount rendered back in the display currency.
    if (previous && usd === written && rate === previous[1]) return
    text.value = display(usd, rate)
  },
  { immediate: true }
)
function input(event: Event) {
  text.value = (event.target as HTMLInputElement).value
  // An invalid amount is passed on unchanged for the parent to reject.
  written =
    props.rate === '1' || !isDecimal(text.value)
      ? text.value
      : divideDecimal(text.value, props.rate)
  emit('update:modelValue', written)
}
</script>
<template>
  <input :value="text" inputmode="decimal" @input="input" />
</template>
