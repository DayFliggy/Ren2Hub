<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { TOKEN_VARIABLES } from '@/features/pricing/lib/billing-expression/types'
import {
  createEmptyVisualCondition,
  visualNodeId,
  type VisualPrice,
  type VisualPricingNode,
} from '@/features/pricing/lib/billing-expression/visual'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import PricingAmountInput from './PricingAmountInput.vue'
type Tier = Extract<VisualPricingNode, { kind: 'tier' }>
// Prices stay in USD; rate only changes what the inputs display. With
// imageCount the whole document is multiplied by image_count, so a fixed
// price is per image.
const props = defineProps<{
  node: Tier
  imageCount?: boolean
  rate: string
  currency: string
}>()
const emit = defineEmits<{ change: [node: VisualPricingNode] }>()
const { t, te } = useI18n({ useScope: 'local', messages: modelPricingMessages })
// len is the tier-condition context length; it is never priced on its own.
const priceVariables = TOKEN_VARIABLES.filter((variable) => variable !== 'len')
function change(patch: Partial<Tier>) {
  emit('change', { ...props.node, ...patch })
}
function changePrice(index: number, patch: Partial<VisualPrice>) {
  change({
    prices: props.node.prices.map((price, at) =>
      at === index ? { ...price, ...patch } : price
    ),
  })
}
function field(event: Event) {
  return (event.target as HTMLInputElement | HTMLSelectElement).value
}
// As in NewAPI, the new branch matches first with a copy of this tier, and
// this tier stays as the fallback when the condition does not match.
function branch() {
  const node = props.node
  emit('change', {
    id: visualNodeId(),
    kind: 'branch',
    condition: createEmptyVisualCondition(),
    yes: {
      ...node,
      id: visualNodeId(),
      origin: undefined,
      prices: node.prices.map(({ variable, value }) => ({ variable, value })),
    },
    no: node,
  })
}
</script>
<template>
  <div class="price-tier">
    <div class="price-tier-heading">
      <label class="admin-field"
        ><span>{{ t('tierLabel') }}</span
        ><input :value="node.label" @input="change({ label: field($event) })"
      /></label>
      <label class="admin-field"
        ><span>{{ t('unit') }}</span
        ><select
          :value="node.billingUnit"
          @change="
            change({ billingUnit: field($event) as Tier['billingUnit'] })
          "
        >
          <option value="token">{{ t('perMillion') }}</option>
          <option value="request">
            {{ t(imageCount ? 'perImage' : 'perRequest') }}
          </option>
        </select></label
      >
    </div>
    <label v-if="node.billingUnit === 'request'" class="admin-field"
      ><span>{{ t(imageCount ? 'imagePrice' : 'fixed') }} ({{ currency }})</span
      ><PricingAmountInput
        :model-value="node.fixedPrice"
        :rate="rate"
        @update:model-value="change({ fixedPrice: $event })"
    /></label>
    <template v-else>
      <div
        v-for="(price, index) in node.prices"
        :key="index"
        class="price-tier-line"
      >
        <label class="admin-field"
          ><span>{{ t('quantity') }}</span
          ><!-- The serializer reuses an origin's source text, including its
          variable; a changed dimension must be written as a new term. --><select
            :value="price.variable"
            @change="
              changePrice(index, {
                variable: field($event) as VisualPrice['variable'],
                origin: undefined,
              })
            "
          >
            <option
              v-for="variable in priceVariables"
              :key="variable"
              :value="variable"
            >
              {{
                te(`priceVar_${variable}`)
                  ? t(`priceVar_${variable}`)
                  : variable
              }}
            </option>
          </select></label
        ><label class="admin-field"
          ><span>{{ t('price') }} ({{ currency }})</span
          ><PricingAmountInput
            :model-value="price.value"
            :rate="rate"
            @update:model-value="
              changePrice(index, { value: $event })
            " /></label
        ><ConsoleButton
          variant="ghost"
          size="sm"
          @click="
            change({ prices: node.prices.filter((_, at) => at !== index) })
          "
          >{{ t('remove') }}</ConsoleButton
        >
      </div>
      <ConsoleButton
        variant="ghost"
        size="sm"
        @click="
          change({ prices: [...node.prices, { variable: 'p', value: '' }] })
        "
        >{{ t('addPrice') }}</ConsoleButton
      >
    </template>
    <ConsoleButton variant="secondary" size="sm" @click="branch">{{
      t('branch')
    }}</ConsoleButton>
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
.price-tier-line label:first-child {
  flex: 1.5 1 9.5rem;
}
</style>
