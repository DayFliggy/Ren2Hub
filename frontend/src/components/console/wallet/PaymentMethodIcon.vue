<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { CreditCard, ExternalLink, Landmark } from 'lucide-vue-next'

import { paymentIcon } from './paymentIcon'

const props = withDefaults(
  defineProps<{ icon?: string; type?: string; fallback?: 'card' | 'link' }>(),
  { icon: '', type: '', fallback: 'card' }
)
// A configured image that fails to load falls back to the built-in icon.
const imageFailed = ref(false)
watch(
  () => props.icon,
  () => (imageFailed.value = false)
)
const resolved = computed(() =>
  paymentIcon(imageFailed.value ? { ...props, icon: '' } : props)
)
const generic = { card: CreditCard, bank: Landmark, link: ExternalLink }
</script>

<template>
  <!-- Decorative: the method or link name beside it carries the meaning. -->
  <img
    v-if="resolved.kind === 'image'"
    :src="resolved.src"
    alt=""
    aria-hidden="true"
    class="size-5 shrink-0 object-contain"
    loading="lazy"
    decoding="async"
    referrerpolicy="no-referrer"
    @error="imageFailed = true"
  />
  <svg
    v-else-if="resolved.kind === 'brand'"
    viewBox="0 0 24 24"
    class="payment-brand-mark size-5 shrink-0"
    :style="{
      '--brand-mark-light': resolved.mark.color,
      '--brand-mark-dark': resolved.mark.darkColor ?? resolved.mark.color,
    }"
    fill="currentColor"
    aria-hidden="true"
  >
    <path :d="resolved.mark.path" />
  </svg>
  <span
    v-else-if="resolved.kind === 'letter'"
    class="payment-brand-mark inline-flex size-5 shrink-0 items-center justify-center rounded-full text-[0.625rem] font-bold leading-none"
    :style="{
      '--brand-mark-light': resolved.color,
      '--brand-mark-dark': resolved.darkColor ?? resolved.color,
      boxShadow: 'inset 0 0 0 1.5px currentColor',
    }"
    aria-hidden="true"
  >
    {{ resolved.letter }}
  </span>
  <component
    :is="generic[resolved.name]"
    v-else
    class="size-4 shrink-0 text-[var(--text-tertiary)]"
    aria-hidden="true"
  />
</template>

<style scoped>
.payment-brand-mark {
  color: var(--brand-mark-light);
}

html.dark .payment-brand-mark {
  color: var(--brand-mark-dark);
}
</style>
