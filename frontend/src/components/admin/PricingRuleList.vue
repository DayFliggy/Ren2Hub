<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import {
  visualConditionExpression,
  type VisualBillingIssue,
  type VisualCondition,
  type VisualPricingNode,
} from '@/features/pricing/lib/billing-expression/visual'
import { isDecimal, multiplyDecimal } from '@/utils/decimal'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import PricingConditionTree from './PricingConditionTree.vue'
import PricingTierFields from './PricingTierFields.vue'
// Absorbed from NewAPI's PricingRuleList: a chain of branches reads as
// ordered rules where the first match wins and the last tier is the fallback.
const props = withDefaults(
  defineProps<{
    node: VisualPricingNode
    source: string
    issues: VisualBillingIssue[]
    rate: string
    currency: string
    imageCount?: boolean
    prefix?: string
  }>(),
  { imageCount: false, prefix: '' }
)
const emit = defineEmits<{ change: [node: VisualPricingNode] }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const toggled = reactive(new Set<string>())
const rules = computed(() => {
  const list: VisualPricingNode[] = []
  let current = props.node
  while (current.kind === 'branch') {
    list.push(current)
    current = current.no
  }
  list.push(current)
  return list
})
// Rebuilds the chain above a changed rule; later rules stay as they are.
function replace(index: number, next: VisualPricingNode) {
  let root = next
  for (let previous = index - 1; previous >= 0; previous--) {
    const branch = rules.value[previous]
    if (branch?.kind === 'branch') root = { ...branch, no: root }
  }
  emit('change', root)
}
function tierOf(node: VisualPricingNode) {
  return node.kind === 'tier' ? node : node.yes
}
function number(index: number) {
  return `${props.prefix}${index + 1}`
}
function name(node: VisualPricingNode, index: number) {
  const tier = tierOf(node)
  return tier.kind === 'tier' && tier.label
    ? tier.label
    : t('ruleName', { number: number(index) })
}
function matchText(node: VisualPricingNode, index: number) {
  if (node.kind === 'tier') return t(index ? 'ruleFallback' : 'ruleDefault')
  const condition =
    visualConditionExpression(node.condition, props.source) ??
    t('tierConditions')
  return index ? t('ruleAfter', { condition }) : condition
}
function amount(usd: string) {
  if (!isDecimal(usd)) return '—'
  return props.rate === '1' ? usd : multiplyDecimal(usd, props.rate)
}
function overview(node: VisualPricingNode) {
  const tier = tierOf(node)
  if (tier.kind !== 'tier') return ''
  if (tier.billingUnit === 'request')
    return `${amount(tier.fixedPrice)} ${props.currency} ${t(props.imageCount ? 'perImage' : 'perRequest')}`
  const prices = tier.prices.map(
    (price) => `${price.variable} ${amount(price.value)}`
  )
  return `${prices.join(' · ')} ${props.currency} ${t('perMillion')}`
}
// A rule owns its condition and matched subtree, not the rules after it.
function ownIds(node: VisualPricingNode) {
  const ids = new Set<string>()
  const pending: Array<VisualPricingNode | VisualCondition> =
    node.kind === 'tier' ? [node] : [node.condition, node.yes]
  for (let index = 0; index < pending.length; index++) {
    const current = pending[index]!
    ids.add(current.id)
    if (current.kind === 'branch')
      pending.push(current.condition, current.yes, current.no)
    if (current.kind === 'all' || current.kind === 'any')
      pending.push(...current.children)
    if (current.kind === 'not') pending.push(current.child)
  }
  return ids
}
// The first rule starts open; toggled holds the cards flipped from that
// default. A card with issues stays open so they are visible.
function open(node: VisualPricingNode, index: number) {
  if (toggled.has(node.id) !== (index === 0)) return true
  const ids = ownIds(node)
  return props.issues.some((issue) => ids.has(issue.id.split(':')[0]!))
}
function toggle(node: VisualPricingNode) {
  if (toggled.has(node.id)) toggled.delete(node.id)
  else toggled.add(node.id)
}
</script>
<template>
  <ol class="price-rules" :aria-label="t('pricingRules')">
    <li v-for="(rule, index) in rules" :key="rule.id" class="price-rule-card">
      <div class="price-rule-head">
        <button
          type="button"
          class="price-rule-toggle"
          :aria-expanded="open(rule, index)"
          @click="toggle(rule)"
        >
          <span class="price-rule-number">{{ number(index) }}</span>
          <span class="price-rule-summary">
            <strong>{{ name(rule, index) }}</strong>
            <span>{{ matchText(rule, index) }}</span>
            <small>{{ overview(rule) }}</small>
          </span>
        </button>
        <ConsoleButton
          v-if="rule.kind === 'branch'"
          variant="ghost"
          size="sm"
          @click="replace(index, rule.no)"
          >{{ t('removeRule') }}</ConsoleButton
        >
      </div>
      <div v-show="open(rule, index)" class="price-rule-body">
        <template v-if="rule.kind === 'branch'">
          <p class="price-rule-caption">{{ t('tierConditions') }}</p>
          <PricingConditionTree
            :node="rule.condition"
            @change="replace(index, { ...rule, condition: $event })"
          />
        </template>
        <PricingTierFields
          v-if="tierOf(rule).kind === 'tier'"
          :node="tierOf(rule) as Extract<VisualPricingNode, { kind: 'tier' }>"
          :image-count="imageCount"
          :rate="rate"
          :currency="currency"
          @change="
            replace(
              index,
              rule.kind === 'tier' ? $event : { ...rule, yes: $event }
            )
          "
        />
        <div v-else-if="rule.kind === 'branch'" class="price-rule-nested">
          <p class="price-rule-caption">{{ t('ruleWhen') }}</p>
          <PricingRuleList
            :node="rule.yes"
            :source="source"
            :issues="issues"
            :rate="rate"
            :currency="currency"
            :image-count="imageCount"
            :prefix="`${number(index)}.`"
            @change="replace(index, { ...rule, yes: $event })"
          />
        </div>
      </div>
    </li>
  </ol>
</template>
<style scoped>
.price-rules {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}
.price-rule-card {
  min-width: 0;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
}
.price-rule-head {
  display: flex;
  align-items: flex-start;
  gap: 0.25rem;
  padding: 0.75rem;
}
.price-rule-toggle {
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  text-align: left;
  border-radius: 0.375rem;
}
.price-rule-number {
  flex-shrink: 0;
  min-width: 1.5rem;
  padding: 0.125rem 0.375rem;
  border-radius: 0.375rem;
  background: var(--surface-muted);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  text-align: center;
}
.price-rule-summary {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
  overflow-wrap: anywhere;
}
.price-rule-summary span,
.price-rule-summary small {
  color: var(--text-secondary);
  font-size: 0.8125rem;
}
.price-rule-summary small {
  font-variant-numeric: tabular-nums;
}
.price-rule-body {
  display: grid;
  gap: 0.75rem;
  padding: 0.75rem;
  border-top: 1px solid var(--border-subtle);
}
.price-rule-caption {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 500;
}
.price-rule-nested {
  display: grid;
  gap: 0.75rem;
  padding-left: 0.75rem;
  border-left: 2px solid var(--border-subtle);
}
</style>
