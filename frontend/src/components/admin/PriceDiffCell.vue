<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PriceDiffField } from '@/features/pricing/diff'
import { modelPricingMessages } from '@/i18n/modelPricing'
defineProps<{ fields: PriceDiffField[]; side: 'current' | 'candidate' }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
</script>
<template>
  <dl class="price-diff">
    <div
      v-for="field in fields"
      :key="field.key"
      :data-price-change="field.change"
    >
      <dt>
        {{ field.key }}
        <span v-if="field.change !== 'unchanged'" class="price-diff-label">{{
          t(`diff${field.change}`)
        }}</span>
      </dt>
      <dd>
        <span v-if="field[side] === null" class="price-diff-absent">{{
          t('diffUnset')
        }}</span>
        <code v-else
          ><span
            v-for="(segment, index) in field[side]"
            :key="index"
            :data-diff="
              segment.changed
                ? side === 'current'
                  ? 'removed'
                  : 'added'
                : undefined
            "
            >{{ segment.text }}</span
          ><span v-if="!field[side]?.length">""</span></code
        >
      </dd>
    </div>
  </dl>
</template>
<style scoped>
.price-diff {
  display: grid;
  gap: 0.6rem;
  min-width: 12rem;
  max-width: 22rem;
  overflow-wrap: anywhere;
}
.price-diff dt {
  font-size: 0.7rem;
  color: var(--text-secondary);
}
.price-diff dd {
  margin: 0.2rem 0 0;
}
.price-diff code {
  white-space: pre-wrap;
  font-size: 0.75rem;
}
.price-diff-label {
  display: inline-block;
  padding-inline: 0.3rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.25rem;
}
.price-diff-absent {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  font-style: italic;
}
[data-diff='removed'] {
  background: var(--status-danger-soft);
  color: var(--status-danger-text);
  text-decoration: line-through;
}
[data-diff='added'] {
  background: var(--status-success-soft);
  color: var(--status-success-text);
  text-decoration: underline;
  text-underline-offset: 2px;
}
</style>
