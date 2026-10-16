<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelMarketMessages } from '@/i18n/modelMarket'
import VendorLogo from './VendorLogo.vue'
import CopyModelButton from './CopyModelButton.vue'

const props = withDefaults(
  defineProps<{
    name: string
    vendor: string
    typeLabel?: string
    compact?: boolean
    clickable?: boolean
  }>(),
  { typeLabel: '', compact: false, clickable: false }
)
const emit = defineEmits<{ detail: [] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const displayVendor = computed(() =>
  !props.vendor || props.vendor === '__platform__'
    ? t('platform')
    : props.vendor
)
</script>

<template>
  <div class="market-model-identity" :class="{ compact }">
    <VendorLogo :vendor="displayVendor" :size="compact ? 30 : 36" />
    <component
      :is="clickable ? 'button' : 'div'"
      class="market-model-name-group"
      :class="{ 'focus-ring': clickable }"
      :type="clickable ? 'button' : undefined"
      :aria-label="clickable ? t('openDetail', { name }) : undefined"
      @click="clickable && emit('detail')"
    >
      <span class="market-model-name" :title="name">{{ name }}</span>
      <span class="market-model-meta"
        >{{ displayVendor
        }}<span v-if="typeLabel"> · {{ typeLabel }}</span></span
      >
    </component>
    <CopyModelButton :name="name" />
  </div>
</template>

<style scoped>
.market-model-identity {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-width: 0;
}
.market-model-name-group {
  flex: 1;
  min-width: 0;
  text-align: left;
  border-radius: 0.25rem;
}
.market-model-name {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  font-size: 0.875rem;
  font-weight: 600;
  line-height: 1.6;
  color: var(--text-primary);
}
.market-model-meta {
  display: block;
  margin-top: 0.125rem;
  font-size: 0.6875rem;
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
button.market-model-name-group:hover .market-model-name {
  color: var(--accent-text);
}
.compact .market-model-name {
  font-size: 0.8125rem;
}
</style>
