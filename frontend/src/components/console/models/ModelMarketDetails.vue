<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MarketModel } from '@/types/modelMarket'
import { modelMarketMessages } from '@/i18n/modelMarket'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ModelIdentity from './ModelIdentity.vue'
import ModelPriceItems from './ModelPriceItems.vue'
const props = defineProps<{
  model: MarketModel | null
  initialGroup?: string
}>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
const selected = ref('')
watch(
  () => [props.model, props.initialGroup],
  () => {
    selected.value = props.model?.groups.some(
      (group) => group.name === props.initialGroup
    )
      ? props.initialGroup!
      : ''
  },
  { immediate: true }
)
const group = computed(() =>
  props.model?.groups.find((item) => item.name === selected.value)
)
</script>
<template>
  <ConsoleModal
    :open="!!model"
    :title="t('detailsTitle')"
    size="xl"
    @close="emit('close')"
  >
    <div v-if="model" class="market-details" data-model-market-details>
      <ModelIdentity :name="model.name" :vendor="model.vendor" />
      <section>
        <h3>{{ t('description') }}</h3>
        <p class="description">{{ model.description || t('noDescription') }}</p>
        <p v-if="model.tags.length">
          {{ t('tags') }}: {{ model.tags.join(' · ') }}
        </p>
      </section>
      <section>
        <h3>{{ t('endpoints') }}</h3>
        <ul v-if="model.endpoints.length" class="endpoints">
          <li v-for="endpoint in model.endpoints" :key="endpoint.type">
            <strong>{{ endpoint.type }}</strong
            ><code>{{
              endpoint.path
                ? `${endpoint.method} ${endpoint.path}`
                : t('unknown')
            }}</code>
          </li>
        </ul>
        <p v-else>{{ t('unknown') }}</p>
        <p class="note">{{ t('capabilitiesUnavailable') }}</p>
      </section>
      <section>
        <h3>{{ t('pricingDetails') }}</h3>
        <label class="block mb-4"
          >{{ t('groups')
          }}<select
            v-model="selected"
            class="console-input mt-2 w-full"
            :aria-label="t('groups')"
          >
            <option value="">{{ t('benchmark') }}</option>
            <option
              v-for="item in model.groups"
              :key="item.name"
              :value="item.name"
            >
              {{ item.name }} · {{ item.description }}
            </option>
          </select></label
        >
        <p v-if="group?.ratio === 'auto'">{{ t('autoPrice') }}</p>
        <template v-else
          ><p v-if="group" class="note">
            {{ t('groupRatio', { ratio: group.ratio }) }}
          </p>
          <ModelPriceItems
            :pricing="model.pricing"
            :group-ratio="typeof group?.ratio === 'number' ? group.ratio : 1"
        /></template>
      </section>
      <p class="note">{{ t('priceNote') }}</p>
      <p class="note">{{ t('requestNote') }}</p>
    </div>
    <template #footer
      ><ConsoleButton variant="secondary" @click="emit('close')">{{
        t('close')
      }}</ConsoleButton></template
    >
  </ConsoleModal>
</template>
<style scoped>
.market-details {
  display: grid;
  gap: 1.25rem;
  overflow-wrap: anywhere;
}
section {
  border-top: 1px dashed var(--border-subtle);
  padding-top: 1rem;
}
h3 {
  font-size: 0.8125rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
}
p,
label {
  color: var(--text-secondary);
  font-size: 0.8125rem;
  line-height: 1.7;
}
.description {
  white-space: pre-wrap;
}
.note {
  color: var(--text-tertiary);
  font-size: 0.75rem;
  margin-top: 0.5rem;
}
.endpoints {
  display: grid;
  gap: 0.5rem;
  font-size: 0.75rem;
}
.endpoints li {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1rem;
  justify-content: space-between;
}
code {
  white-space: pre-wrap;
  color: var(--text-secondary);
}
</style>
