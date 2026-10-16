<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import { modelMarketMessages } from '@/i18n/modelMarket'

const props = defineProps<{ name: string }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const toast = useToast()
async function copyName() {
  try {
    await navigator.clipboard.writeText(props.name)
    toast.success(t('copied', { name: props.name }))
  } catch {
    toast.error(t('copyFailed'))
  }
}
</script>

<template>
  <button
    type="button"
    class="market-copy focus-ring"
    :aria-label="`${t('copy')} · ${name}`"
    :title="t('copy')"
    @click.stop="copyName"
  >
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <rect x="8" y="8" width="12" height="12" rx="2" />
      <path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3" />
    </svg>
  </button>
</template>

<style scoped>
.market-copy {
  position: relative;
  z-index: 2;
  display: grid;
  place-items: center;
  flex: none;
  width: 2rem;
  height: 2rem;
  border-radius: var(--shape-control);
  color: var(--text-tertiary);
}
.market-copy:hover {
  color: var(--text-primary);
  background: var(--state-hover-layer);
}
@media (pointer: coarse) {
  .market-copy {
    min-width: 2.75rem;
    min-height: 2.75rem;
  }
}
</style>
