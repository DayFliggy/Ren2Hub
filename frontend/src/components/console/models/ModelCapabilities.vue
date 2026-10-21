<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelCapabilities } from '@/api/modelCatalog'
import { modelMarketMessages } from '@/i18n/modelMarket'
import { formatContext } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    capabilities: ModelCapabilities | null
    expanded?: boolean
  }>(),
  { expanded: false }
)
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const items = computed(() => {
  const value = props.capabilities
  const result = [
    {
      key: 'context',
      value: value?.context_limit
        ? formatContext(value.context_limit)
        : t('unknown'),
    },
    {
      key: 'outputLimit',
      value: value?.output_limit
        ? formatContext(value.output_limit)
        : t('unknown'),
    },
    {
      key: 'tools',
      value:
        value?.tool_call == null
          ? t('unknown')
          : t(value.tool_call ? 'supported' : 'unsupported'),
    },
  ]
  if (props.expanded) {
    result.push(
      {
        key: 'inputModalities',
        value: value?.input_modalities?.join(' · ') || t('unknown'),
      },
      {
        key: 'outputModalities',
        value: value?.output_modalities?.join(' · ') || t('unknown'),
      },
      {
        key: 'reasoning',
        value: value?.reasoning_efforts?.join(' · ') || t('unknown'),
      }
    )
  }
  return result
})
</script>

<template>
  <dl class="market-capabilities" :class="{ expanded }">
    <div v-for="item in items" :key="item.key">
      <dt>{{ t(item.key) }}</dt>
      <dd>{{ item.value }}</dd>
    </div>
  </dl>
</template>

<style scoped>
.market-capabilities {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.8rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.6;
}
.market-capabilities > div {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.market-capabilities dd {
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.expanded {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.625rem;
}
.expanded > div {
  display: block;
  min-width: 0;
  padding: 0.75rem;
  background: var(--surface-muted);
  border-radius: var(--shape-control);
}
.expanded dd {
  overflow-wrap: anywhere;
  margin-top: 0.3rem;
  font-size: 0.8125rem;
}
@media (max-width: 600px) {
  .expanded {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
