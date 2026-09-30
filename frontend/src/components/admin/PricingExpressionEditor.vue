<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { modelPricingMessages } from '@/i18n/modelPricing'
import {
  parseVisualBillingDocument,
  serializeVisualBillingDocument,
  visualNodeId,
  type VisualBillingDocument,
} from '@/features/pricing/lib/billing-expression/visual'
import {
  splitBillingExprAndRequestRules,
  combineBillingExpr,
  tryParseRequestRuleExpr,
  buildRequestRuleExpr,
  createEmptyRuleGroup,
  createEmptyCondition,
  createEmptyTimeCondition,
  type RequestRuleGroup,
} from '@/features/pricing/lib/billing-expr'
import { isDecimal } from '@/utils/decimal'
import PricingTier from './PricingTier.vue'
const model = defineModel<string>({ required: true })
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const mode = ref('source'),
  document = ref<VisualBillingDocument | null>(null)
const rules = ref<RequestRuleGroup[]>([]),
  ruleSource = ref(''),
  base = ref(''),
  error = ref('')
let emitted = ''
watch(
  model,
  (value) => {
    if (value === emitted) return
    const split = splitBillingExprAndRequestRules(value)
    base.value = split.billingExpr
    ruleSource.value = split.requestRuleExpr
    document.value = parseVisualBillingDocument(split.billingExpr)
    rules.value = tryParseRequestRuleExpr(split.requestRuleExpr) ?? []
  },
  { immediate: true }
)
const opaqueRules = computed(
  () => ruleSource.value && !tryParseRequestRuleExpr(ruleSource.value)
)
function applyVisual() {
  error.value = ''
  if (!document.value) return
  const serialized = serializeVisualBillingDocument(document.value)
  if (!serialized.ok) {
    error.value = serialized.issues.map((issue) => issue.message).join('; ')
    return
  }
  base.value = serialized.source
  emitted = combineBillingExpr(base.value, ruleSource.value)
  model.value = emitted
}
function addBase() {
  document.value = {
    source: '',
    root: {
      id: visualNodeId(),
      kind: 'tier',
      label: 'base',
      billingUnit: 'token',
      fixedPrice: '',
      prices: [
        { variable: 'p', value: '' },
        { variable: 'c', value: '' },
      ],
    },
  }
  mode.value = 'visual'
}
function applyRules() {
  error.value = ''
  if (
    rules.value.some(
      (rule) =>
        !isDecimal(rule.multiplier) ||
        !rule.conditions.length ||
        rule.conditions.some(
          (condition) =>
            (condition.source !== 'time' && !condition.path.trim()) ||
            (condition.mode !== 'exists' &&
              condition.mode !== 'range' &&
              !condition.value.trim())
        )
    )
  ) {
    error.value = t('requestRuleInvalid')
    return
  }
  ruleSource.value = buildRequestRuleExpr(rules.value)
  emitted = combineBillingExpr(base.value, ruleSource.value)
  model.value = emitted
}
function changeSource(rule: RequestRuleGroup, index: number, event: Event) {
  const source = (event.target as HTMLSelectElement).value
  rule.conditions[index] =
    source === 'time'
      ? createEmptyTimeCondition()
      : {
          ...createEmptyCondition(),
          source: source === 'header' ? 'header' : 'param',
        }
}
defineExpose({
  validate: () => {
    if (mode.value === 'visual') applyVisual()
    if (error.value) throw new Error(error.value)
  },
})
</script>
<template>
  <section class="price-expression">
    <div class="price-expression-actions">
      <ConsoleButton
        variant="ghost"
        size="sm"
        :aria-pressed="mode === 'source'"
        @click="mode = 'source'"
        >{{ t('exprSource') }}</ConsoleButton
      ><ConsoleButton
        variant="ghost"
        size="sm"
        :disabled="!document"
        :aria-pressed="mode === 'visual'"
        @click="mode = 'visual'"
        >{{ t('exprVisual') }}</ConsoleButton
      ><ConsoleButton
        v-if="!model.trim()"
        variant="secondary"
        size="sm"
        @click="addBase"
        >{{ t('preset') }}</ConsoleButton
      >
    </div>
    <label v-if="mode === 'source'" class="admin-field"
      ><span class="sr-only">{{ t('exprSource') }}</span
      ><textarea
        v-model="model"
        rows="6"
        spellcheck="false"
        :placeholder="t('exprRequired')"
      />
    </label>
    <template v-else-if="document"
      ><PricingTier v-model="document.root" /><ConsoleButton
        variant="secondary"
        size="sm"
        @click="applyVisual"
        >{{ t('apply') }}</ConsoleButton
      ></template
    >
    <p v-if="model && !document" class="admin-muted">{{ t('exprAdvanced') }}</p>
    <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
    <details>
      <summary>{{ t('rules') }}</summary>
      <p class="admin-muted">{{ t('paramsHint') }}</p>
      <label v-if="opaqueRules" class="admin-field"
        ><span>{{ t('conditionsRaw') }}</span
        ><textarea v-model="ruleSource" rows="3" /><ConsoleButton
          variant="secondary"
          size="sm"
          @click="model = combineBillingExpr(base, ruleSource)"
          >{{ t('apply') }}</ConsoleButton
        ></label
      >
      <template v-else>
        <div
          v-for="(rule, ruleIndex) in rules"
          :key="ruleIndex"
          class="price-rule"
        >
          <div
            v-for="(condition, conditionIndex) in rule.conditions"
            :key="conditionIndex"
            class="price-rule-fields"
          >
            <label class="admin-field"
              ><span>{{ t('source') }}</span
              ><select
                :value="condition.source"
                @change="changeSource(rule, conditionIndex, $event)"
              >
                <option value="param">{{ t('param') }}</option>
                <option value="header">{{ t('header') }}</option>
                <option value="time">{{ t('time') }}</option>
              </select></label
            >
            <template v-if="condition.source === 'time'"
              ><label class="admin-field"
                ><span>{{ t('variable') }}</span
                ><select v-model="condition.timeFunc">
                  <option
                    v-for="func in [
                      'hour',
                      'minute',
                      'weekday',
                      'month',
                      'day',
                    ]"
                    :key="func"
                  >
                    {{ func }}
                  </option>
                </select></label
              ><label class="admin-field"
                ><span>{{ t('timezone') }}</span
                ><input v-model="condition.timezone" /></label
            ></template>
            <label v-else class="admin-field"
              ><span>{{ t('path') }}</span
              ><input v-model="condition.path"
            /></label>
            <label class="admin-field"
              ><span>{{ t('operator') }}</span
              ><select v-model="condition.mode">
                <option value="eq">==</option>
                <option value="gte">&gt;=</option>
                <option value="lt">&lt;</option>
                <option v-if="condition.source !== 'time'" value="gt">
                  &gt;
                </option>
                <option v-if="condition.source !== 'time'" value="lte">
                  &lt;=
                </option>
                <option v-if="condition.source !== 'time'" value="contains">
                  {{ t('include') }}
                </option>
                <option v-if="condition.source !== 'time'" value="exists">
                  {{ t('exists') }}
                </option>
                <option v-if="condition.source === 'time'" value="range">
                  {{ t('rangeMode') }}
                </option>
              </select></label
            >
            <template
              v-if="condition.source === 'time' && condition.mode === 'range'"
              ><label class="admin-field"
                ><span>{{ t('value') }}</span
                ><input v-model="condition.rangeStart" /></label
              ><label class="admin-field"
                ><span>{{ t('conditionEnd') }}</span
                ><input v-model="condition.rangeEnd" /></label
            ></template>
            <label v-else-if="condition.mode !== 'exists'" class="admin-field"
              ><span>{{ t('value') }}</span
              ><input v-model="condition.value"
            /></label>
            <ConsoleButton
              variant="ghost"
              size="sm"
              @click="rule.conditions.splice(conditionIndex, 1)"
              >{{ t('remove') }}</ConsoleButton
            >
          </div>
          <div class="price-rule-fields">
            <label class="admin-field"
              ><span>{{ t('multiplier') }}</span
              ><input v-model="rule.multiplier" inputmode="decimal" /></label
            ><ConsoleButton
              variant="ghost"
              size="sm"
              @click="rule.conditions.push(createEmptyCondition())"
              >{{ t('addRuleCondition') }}</ConsoleButton
            ><ConsoleButton
              variant="ghost"
              size="sm"
              @click="rules.splice(ruleIndex, 1)"
              >{{ t('remove') }}</ConsoleButton
            >
          </div>
        </div>
        <div class="price-expression-actions">
          <ConsoleButton
            variant="ghost"
            size="sm"
            @click="rules.push(createEmptyRuleGroup())"
            >{{ t('addRule') }}</ConsoleButton
          ><ConsoleButton variant="secondary" size="sm" @click="applyRules">{{
            t('apply')
          }}</ConsoleButton>
        </div>
      </template>
    </details>
  </section>
</template>
<style scoped>
.price-expression {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
}
.price-expression-actions,
.price-rule-fields {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 0.5rem;
}
.price-rule-fields label {
  flex: 1 1 7rem;
}
.price-rule {
  padding: 0.75rem 0;
  border-bottom: 1px solid var(--border-subtle);
  display: grid;
  gap: 0.75rem;
}
textarea {
  font-family: monospace;
  width: 100%;
}
summary {
  cursor: pointer;
  padding: 0.75rem 0;
}
</style>
