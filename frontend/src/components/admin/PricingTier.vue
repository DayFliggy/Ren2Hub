<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { TOKEN_VARIABLES } from '@/features/pricing/lib/billing-expression/types'
import {
  createEmptyVisualCondition,
  visualNodeId,
  type VisualPricingNode,
} from '@/features/pricing/lib/billing-expression/visual'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import PricingCondition from './PricingCondition.vue'
const model = defineModel<VisualPricingNode>({ required: true })
// The whole document is multiplied by image_count: fixed prices are per image.
defineProps<{ imageCount?: boolean }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
// len is the tier-condition context length; it is never priced on its own.
const priceVariables = TOKEN_VARIABLES.filter((variable) => variable !== 'len')
function branch() {
  model.value = {
    id: visualNodeId(),
    kind: 'branch',
    condition: createEmptyVisualCondition(),
    yes: model.value,
    no: {
      id: visualNodeId(),
      kind: 'tier',
      label: '',
      billingUnit: 'token',
      fixedPrice: '',
      prices: [
        { variable: 'p', value: '' },
        { variable: 'c', value: '' },
      ],
    },
  }
}
</script>
<template>
  <div class="price-tier">
    <template v-if="model.kind === 'tier'">
      <div class="price-tier-heading">
        <label class="admin-field"
          ><span>{{ t('tierLabel') }}</span
          ><input v-model="model.label" /></label
        ><label class="admin-field"
          ><span>{{ t('unit') }}</span
          ><select v-model="model.billingUnit">
            <option value="token">{{ t('perMillion') }}</option>
            <option value="request">
              {{ t(imageCount ? 'perImage' : 'perRequest') }}
            </option>
          </select></label
        >
      </div>
      <label v-if="model.billingUnit === 'request'" class="admin-field"
        ><span>{{ t(imageCount ? 'imagePrice' : 'fixed') }} (USD)</span
        ><input v-model="model.fixedPrice" inputmode="decimal"
      /></label>
      <template v-else>
        <div
          v-for="(price, index) in model.prices"
          :key="index"
          class="price-tier-line"
        >
          <label class="admin-field"
            ><span>{{ t('quantity') }}</span
            ><!-- The serializer reuses an origin's source text, including its
            variable; a changed dimension must be written as a new term. --><select
              v-model="price.variable"
              @change="price.origin = undefined"
            >
              <option v-for="variable in priceVariables" :key="variable">
                {{ variable }}
              </option>
            </select></label
          ><label class="admin-field"
            ><span>{{ t('price') }} (USD)</span
            ><input v-model="price.value" inputmode="decimal" /></label
          ><ConsoleButton
            variant="ghost"
            size="sm"
            @click="model.prices.splice(index, 1)"
            >{{ t('remove') }}</ConsoleButton
          >
        </div>
        <ConsoleButton
          variant="ghost"
          size="sm"
          @click="model.prices.push({ variable: 'p', value: '' })"
          >{{ t('addPrice') }}</ConsoleButton
        >
      </template>
      <ConsoleButton variant="secondary" size="sm" @click="branch">{{
        t('branch')
      }}</ConsoleButton>
    </template>
    <template v-else>
      <PricingCondition v-model="model.condition" />
      <div class="price-tier-branch">
        <div class="price-tier-heading">
          <span>{{ t('yes') }}</span
          ><ConsoleButton
            variant="ghost"
            size="sm"
            @click="model = model.yes"
            >{{ t('deleteBranch') }}</ConsoleButton
          >
        </div>
        <PricingTier v-model="model.yes" :image-count="imageCount" />
      </div>
      <div class="price-tier-branch">
        <div class="price-tier-heading">
          <span>{{ t('no') }}</span
          ><ConsoleButton variant="ghost" size="sm" @click="model = model.no">{{
            t('deleteBranch')
          }}</ConsoleButton>
        </div>
        <PricingTier v-model="model.no" :image-count="imageCount" />
      </div>
    </template>
  </div>
</template>
<style scoped>
.price-tier {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
}
.price-tier-line,
.price-tier-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 0.5rem;
}
.price-tier-line label,
.price-tier-heading label {
  flex: 1 1 6rem;
}
.price-tier-branch {
  border-left: 2px solid var(--border-subtle);
  padding: 0.75rem;
  display: grid;
  gap: 0.75rem;
}
</style>
