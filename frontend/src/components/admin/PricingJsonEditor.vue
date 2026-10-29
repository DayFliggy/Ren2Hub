<script setup lang="ts">
import { PRICING_KEYS, type PricingKey } from '@/api/pricingConfig'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
// One raw JSON text per saved pricing option; the page parses them on use.
const props = defineProps<{ modelValue: Record<PricingKey, string> }>()
const emit = defineEmits<{
  'update:modelValue': [value: Record<PricingKey, string>]
}>()
function change(key: PricingKey, event: Event) {
  const value = (event.target as HTMLTextAreaElement).value
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
</script>
<template>
  <ConsoleCard :padded="true">
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      <label v-for="key in PRICING_KEYS" :key="key" class="block">
        <span
          class="mb-1.5 block text-xs font-mono font-medium text-[var(--text-secondary)]"
          >{{ key }}</span
        >
        <textarea
          :value="modelValue[key]"
          rows="7"
          spellcheck="false"
          class="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs text-[var(--text-primary)] transition-colors focus:border-[var(--accent)] focus-ring outline-hidden"
          @input="change(key, $event)"
        />
      </label>
    </div>
  </ConsoleCard>
</template>
