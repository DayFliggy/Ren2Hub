<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import {
  createEmptyVisualCondition,
  visualNodeId,
  type VisualCondition,
} from '@/features/pricing/lib/billing-expression/visual'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
const model = defineModel<VisualCondition>({ required: true })
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
function changeKind(event: Event) {
  const kind = (event.target as HTMLSelectElement).value
  if (kind === 'comparison') model.value = createEmptyVisualCondition()
  else if (kind === 'not')
    model.value = { id: visualNodeId(), kind, child: model.value }
  else if (kind === 'all' || kind === 'any')
    model.value = {
      id: visualNodeId(),
      kind,
      children: [model.value, createEmptyVisualCondition()],
    }
}
</script>
<template>
  <div class="price-condition">
    <label class="admin-field"
      ><span>{{ t('condition') }}</span
      ><select :value="model.kind" @change="changeKind">
        <option value="comparison">{{ t('operator') }}</option>
        <option value="all">{{ t('all') }}</option>
        <option value="any">{{ t('any') }}</option>
        <option value="not">{{ t('not') }}</option>
      </select></label
    >
    <template v-if="model.kind === 'comparison'">
      <label class="admin-field"
        ><span>{{ t('variable') }}</span
        ><select v-model="model.probe">
          <option
            v-for="variable in [
              'p',
              'c',
              'len',
              'hour',
              'minute',
              'weekday',
              'month',
              'day',
            ]"
            :key="variable"
          >
            {{ variable }}
          </option>
        </select></label
      >
      <label v-if="!['p', 'c', 'len'].includes(model.probe)" class="admin-field"
        ><span>{{ t('timezone') }}</span
        ><input v-model="model.timezone"
      /></label>
      <label class="admin-field"
        ><span>{{ t('operator') }}</span
        ><select v-model="model.operator">
          <option
            v-for="operator in ['<', '<=', '>', '>=', '==', '!=']"
            :key="operator"
          >
            {{ operator }}
          </option>
        </select></label
      >
      <label class="admin-field"
        ><span>{{ t('value') }}</span
        ><input v-model="model.value" inputmode="decimal"
      /></label>
    </template>
    <PricingCondition v-else-if="model.kind === 'not'" v-model="model.child" />
    <template v-else>
      <div
        v-for="(condition, index) in model.children"
        :key="condition.id"
        class="price-condition-child"
      >
        <PricingCondition v-model="model.children[index]!" /><ConsoleButton
          v-if="model.children.length > 1"
          variant="ghost"
          size="sm"
          @click="model.children.splice(index, 1)"
          >{{ t('remove') }}</ConsoleButton
        >
      </div>
      <ConsoleButton
        variant="ghost"
        size="sm"
        @click="model.children.push(createEmptyVisualCondition())"
        >{{ t('addCondition') }}</ConsoleButton
      >
    </template>
  </div>
</template>
<style scoped>
.price-condition {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: end;
  min-width: 0;
}
.price-condition > label {
  flex: 1 1 7rem;
}
.price-condition-child {
  border-left: 2px solid var(--border-subtle);
  padding: 0.5rem;
  width: 100%;
}
</style>
