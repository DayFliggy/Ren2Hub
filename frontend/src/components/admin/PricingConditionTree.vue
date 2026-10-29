<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import {
  createEmptyVisualCondition,
  visualNodeId,
  type VisualComparison,
  type VisualCondition,
} from '@/features/pricing/lib/billing-expression/visual'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
// Every edit emits a new condition; the document owner replaces its node.
const props = defineProps<{ node: VisualCondition }>()
const emit = defineEmits<{ change: [node: VisualCondition] }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const PROBES = ['p', 'c', 'len', 'hour', 'minute', 'weekday', 'month', 'day']
const OPERATORS = ['<', '<=', '>', '>=', '==', '!='] as const
function changeKind(event: Event) {
  const kind = (event.target as HTMLSelectElement).value
  const node = props.node
  if (kind === node.kind) return
  if (kind === 'comparison') emit('change', createEmptyVisualCondition())
  else if (kind === 'not')
    emit('change', { id: visualNodeId(), kind, child: node })
  else if (kind === 'all' || kind === 'any')
    emit('change', {
      id: visualNodeId(),
      kind,
      children: [node, createEmptyVisualCondition()],
    })
}
function changeComparison(patch: Partial<VisualComparison>) {
  if (props.node.kind === 'comparison')
    emit('change', { ...props.node, ...patch })
}
function field(event: Event) {
  return (event.target as HTMLInputElement | HTMLSelectElement).value
}
function changeChildren(children: VisualCondition[]) {
  if (props.node.kind === 'all' || props.node.kind === 'any')
    emit('change', { ...props.node, children })
}
function replaceChild(index: number, child: VisualCondition) {
  if (props.node.kind !== 'all' && props.node.kind !== 'any') return
  changeChildren(
    props.node.children.map((item, at) => (at === index ? child : item))
  )
}
</script>
<template>
  <div class="price-condition">
    <label class="admin-field"
      ><span>{{ t('condition') }}</span
      ><select :value="node.kind" @change="changeKind">
        <option value="comparison">{{ t('operator') }}</option>
        <option value="all">{{ t('all') }}</option>
        <option value="any">{{ t('any') }}</option>
        <option value="not">{{ t('not') }}</option>
      </select></label
    >
    <template v-if="node.kind === 'comparison'">
      <label class="admin-field"
        ><span>{{ t('variable') }}</span
        ><select
          :value="node.probe"
          @change="
            changeComparison({
              probe: field($event) as VisualComparison['probe'],
            })
          "
        >
          <option v-for="probe in PROBES" :key="probe">{{ probe }}</option>
        </select></label
      >
      <label v-if="!['p', 'c', 'len'].includes(node.probe)" class="admin-field"
        ><span>{{ t('timezone') }}</span
        ><input
          :value="node.timezone"
          @input="changeComparison({ timezone: field($event) })"
      /></label>
      <label class="admin-field"
        ><span>{{ t('operator') }}</span
        ><select
          :value="node.operator"
          @change="
            changeComparison({
              operator: field($event) as VisualComparison['operator'],
            })
          "
        >
          <option v-for="operator in OPERATORS" :key="operator">
            {{ operator }}
          </option>
        </select></label
      >
      <label class="admin-field"
        ><span>{{ t('value') }}</span
        ><input
          :value="node.value"
          inputmode="decimal"
          @input="changeComparison({ value: field($event) })"
      /></label>
    </template>
    <div v-else-if="node.kind === 'not'" class="price-condition-child">
      <PricingConditionTree
        :node="node.child"
        @change="emit('change', { ...node, child: $event })"
      />
    </div>
    <template v-else>
      <div
        v-for="(child, index) in node.children"
        :key="child.id"
        class="price-condition-child"
      >
        <PricingConditionTree
          :node="child"
          @change="replaceChild(index, $event)"
        /><ConsoleButton
          v-if="node.children.length > 1"
          variant="ghost"
          size="sm"
          @click="changeChildren(node.children.filter((_, at) => at !== index))"
          >{{ t('remove') }}</ConsoleButton
        >
      </div>
      <ConsoleButton
        variant="ghost"
        size="sm"
        @click="
          changeChildren([...node.children, createEmptyVisualCondition()])
        "
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
