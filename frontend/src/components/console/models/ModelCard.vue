<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { MarketGroup, MarketModel } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceItems from './ModelPriceItems.vue'
defineProps<{ model: MarketModel; group?: MarketGroup }>()
const emit = defineEmits<{ detail: [modelId: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
</script>

<template>
  <article
    class="market-model-card"
    data-model-card
    :data-model-name="model.name"
  >
    <ModelIdentity
      :name="model.name"
      :vendor="model.vendor"
      :type-label="model.types.map((type) => t(`types.${type}`)).join(' · ')"
      clickable
      @detail="emit('detail', model.id)"
    />
    <p class="market-description">
      {{ model.description || t('noDescription') }}
    </p>
    <div class="market-tags">
      <span v-for="tag in model.tags" :key="tag">{{ tag }}</span>
    </div>
    <div class="market-prices">
      <p class="market-price-title">
        {{ group ? t('groupPrice') : t('benchmark')
        }}<span v-if="group"> · {{ group.name }}</span>
      </p>
      <p v-if="group?.ratio === 'auto'" class="market-note">
        {{ t('autoPrice') }}
      </p>
      <template v-else>
        <p v-if="group" class="market-note">
          {{ t('groupRatio', { ratio: group.ratio }) }}
        </p>
        <ModelPriceItems
          :pricing="model.pricing"
          :group-ratio="typeof group?.ratio === 'number' ? group.ratio : 1"
        />
      </template>
    </div>
    <div class="market-card-footer">
      <span>{{ t('groupCount', { count: model.groups.length }) }}</span>
      <button
        class="focus-ring"
        type="button"
        :aria-label="t('openDetail', { name: model.name })"
        @click="emit('detail', model.id)"
      >
        {{ t('details') }} →
      </button>
    </div>
  </article>
</template>

<style scoped>
.market-model-card {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 1rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  background: var(--surface-solid);
  box-shadow: var(--card-shadow);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.market-model-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--card-shadow-hover);
}
.market-description {
  font-size: 0.8125rem;
  line-height: 1.7;
  color: var(--text-secondary);
  margin-block: 0.8rem;
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.market-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.market-tags span {
  padding: 0.15rem 0.4rem;
  border-radius: var(--shape-control);
  background: var(--surface-muted);
  overflow-wrap: anywhere;
}
.market-prices {
  flex: 1;
  border-top: 1px dashed var(--border-subtle);
  padding-top: 0.8rem;
  margin-top: 0.8rem;
}
.market-price-title {
  font-size: 0.75rem;
  font-weight: 600;
  margin-bottom: 0.6rem;
  overflow-wrap: anywhere;
}
.market-note {
  color: var(--text-secondary);
  font-size: 0.75rem;
  margin-bottom: 0.6rem;
}
.market-card-footer {
  display: flex;
  justify-content: space-between;
  gap: 0.5rem;
  flex-wrap: wrap;
  padding-top: 0.8rem;
  margin-top: 0.8rem;
  border-top: 1px dashed var(--border-subtle);
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
.market-card-footer button {
  color: var(--accent-text);
  min-height: 2rem;
}
@media (prefers-reduced-motion: reduce) {
  .market-model-card {
    transition: none;
  }
}
</style>
