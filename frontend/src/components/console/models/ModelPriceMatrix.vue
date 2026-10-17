<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { MarketGroup, MarketModel } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceItems from './ModelPriceItems.vue'
defineProps<{ models: MarketModel[]; groups: MarketGroup[] }>()
const emit = defineEmits<{ detail: [modelId: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
</script>
<template>
  <section data-price-matrix>
    <p class="mb-2 text-sm text-[var(--text-secondary)]">
      {{ t('compareGroups') }}
    </p>
    <p class="mb-4 text-xs text-[var(--text-tertiary)]">{{ t('tableHint') }}</p>
    <div
      class="market-matrix-scroll focus-ring"
      role="region"
      :aria-label="t('tableRegion')"
      tabindex="0"
    >
      <table>
        <caption class="sr-only">
          {{
            t('tableCaption')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col" class="model-column">{{ t('tableModel') }}</th>
            <th scope="col">{{ t('benchmark') }}</th>
            <th v-for="group in groups" :key="group.name" scope="col">
              {{ group.name
              }}<span v-if="group.ratio !== 'auto'" class="group-ratio">{{
                t('groupRatio', { ratio: group.ratio })
              }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="model in models" :key="model.id">
            <th scope="row" class="model-column">
              <ModelIdentity
                :name="model.name"
                :vendor="model.vendor"
                compact
                clickable
                @detail="emit('detail', model.id)"
              />
            </th>
            <td data-base-price>
              <ModelPriceItems :pricing="model.pricing" />
            </td>
            <td
              v-for="group in groups"
              :key="group.name"
              :data-group-price="group.name"
            >
              <p
                v-if="!model.groups.some((item) => item.name === group.name)"
                class="market-unavailable"
              >
                {{ t('noGroupModel') }}
              </p>
              <p v-else-if="group.ratio === 'auto'" class="market-unavailable">
                {{ t('autoPrice') }}
              </p>
              <ModelPriceItems
                v-else
                :pricing="model.pricing"
                :group-ratio="group.ratio"
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
<style scoped>
.market-matrix-scroll {
  overflow-x: auto;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-card);
  isolation: isolate;
}
table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  text-align: left;
}
th,
td {
  min-width: 13rem;
  max-width: 20rem;
  padding: 1rem;
  vertical-align: top;
  border-bottom: 1px solid var(--border-subtle);
  border-right: 1px solid var(--border-subtle);
  background: var(--surface-solid);
  overflow-wrap: anywhere;
}
thead th {
  font-size: 0.8125rem;
  background: var(--surface-muted);
}
.model-column {
  position: sticky;
  left: 0;
  z-index: 1;
  min-width: 16rem;
  max-width: 20rem;
}
tbody .model-column {
  font-weight: normal;
}
tr:last-child td,
tr:last-child th {
  border-bottom: 0;
}
.group-ratio {
  display: block;
  margin-top: 0.25rem;
  color: var(--text-tertiary);
  font-size: 0.6875rem;
}
.market-unavailable {
  color: var(--text-tertiary);
  font-size: 0.75rem;
  font-weight: normal;
}
@media (max-width: 640px) {
  .model-column {
    min-width: 11rem;
    max-width: 11rem;
  }
  th,
  td {
    min-width: 12rem;
    padding: 0.75rem;
  }
}
</style>
