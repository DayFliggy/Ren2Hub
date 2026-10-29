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
import PricingRuleList from './PricingRuleList.vue'
// As in NewAPI's tiered editor, every edit that serializes is written to the
// expression at once. An incomplete draft keeps the last valid expression and
// is exposed as `issue`; the host counts it as unsaved and refuses to save it.
const model = defineModel<string>({ required: true })
// Visual prices are entered in the display currency and written in USD.
withDefaults(defineProps<{ rate?: string; currency?: string }>(), {
  rate: '1',
  currency: 'USD',
})
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
// Compilable frame for the preset tier; its blank prices must still be entered.
const PRESET_SOURCE = 'tier("base", p * 0 + c * 0)'
const mode = ref<'source' | 'visual'>('source'),
  document = ref<VisualBillingDocument | null>(null)
const rules = ref<RequestRuleGroup[]>([]),
  ruleSource = ref(''),
  base = ref(''),
  rulesInvalid = ref(false)
watch(
  model,
  (value) => {
    // A value this editor wrote itself keeps the live draft and its inputs.
    if (value === combineBillingExpr(base.value, ruleSource.value)) return
    const split = splitBillingExprAndRequestRules(value)
    base.value = split.billingExpr
    ruleSource.value = split.requestRuleExpr
    document.value = parseVisualBillingDocument(split.billingExpr)
    rules.value = tryParseRequestRuleExpr(split.requestRuleExpr) ?? []
    if (!document.value) mode.value = 'source'
  },
  { immediate: true }
)
// Each model mounts a fresh editor. A saved expression opens visually when it
// can be edited that way; otherwise its source stays as written.
if (document.value) mode.value = 'visual'
const opaqueRules = computed(
  () => ruleSource.value && !tryParseRequestRuleExpr(ruleSource.value)
)
const visual = computed(() =>
  mode.value === 'visual' && document.value
    ? serializeVisualBillingDocument(document.value)
    : null
)
watch(visual, (result) => {
  if (!result?.ok || result.source === base.value) return
  base.value = result.source
  model.value = combineBillingExpr(base.value, ruleSource.value)
})
watch(
  rules,
  (next, previous) => {
    // A replaced list was just read from the expression; only edits of the
    // current list are drafts to check and write.
    if (next !== previous) {
      rulesInvalid.value = false
      return
    }
    rulesInvalid.value = next.some(
      (rule) =>
        !isDecimal(rule.multiplier) ||
        !rule.conditions.length ||
        rule.conditions.some(
          (condition) =>
            (condition.mode !== 'exists' &&
              condition.mode !== 'range' &&
              !condition.value.trim()) ||
            // The builder silently omits a condition it cannot express, such
            // as an empty path, an out-of-range time or a non-numeric bound.
            !buildRequestRuleExpr([
              { conditions: [condition], multiplier: rule.multiplier },
            ])
        )
    )
    if (rulesInvalid.value) return
    ruleSource.value = buildRequestRuleExpr(next)
    model.value = combineBillingExpr(base.value, ruleSource.value)
  },
  { deep: true }
)
const visualIssues = computed(() =>
  visual.value && !visual.value.ok ? visual.value.issues : []
)
const issue = computed(() => {
  const messages = new Set<string>()
  const result = visual.value
  if (result && !result.ok)
    for (const item of result.issues) messages.add(t(item.message))
  if (rulesInvalid.value) messages.add(t('requestRuleInvalid'))
  return [...messages].join('; ')
})
function showSource() {
  // Leaving visual mode drops an unserializable draft; the expression keeps
  // its last valid value, and the next visual session starts from it.
  mode.value = 'source'
  document.value = parseVisualBillingDocument(base.value)
}
function changeRuleSource(event: Event) {
  ruleSource.value = (event.target as HTMLTextAreaElement).value
  rules.value = tryParseRequestRuleExpr(ruleSource.value) ?? rules.value
  model.value = combineBillingExpr(base.value, ruleSource.value)
}
function addBase() {
  document.value = {
    source: PRESET_SOURCE,
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
defineExpose({ issue })
</script>
<template>
  <section class="price-expression">
    <div class="price-expression-actions">
      <ConsoleButton
        variant="ghost"
        size="sm"
        :aria-pressed="mode === 'source'"
        @click="showSource"
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
    <template v-else-if="document">
      <p v-if="document.root.kind === 'branch'" class="admin-muted">
        {{ t('rulesHint') }}
      </p>
      <PricingRuleList
        :node="document.root"
        :source="document.source"
        :issues="visualIssues"
        :rate="rate"
        :currency="currency"
        :image-count="document.imageCount"
        @change="document = { ...document, root: $event }"
      />
    </template>
    <p v-if="model && !document" class="admin-muted">{{ t('exprAdvanced') }}</p>
    <p v-if="issue" class="admin-error" role="alert">{{ issue }}</p>
    <details>
      <summary>{{ t('rules') }}</summary>
      <p class="admin-muted">{{ t('paramsHint') }}</p>
      <label v-if="opaqueRules" class="admin-field"
        ><span>{{ t('conditionsRaw') }}</span
        ><textarea :value="ruleSource" rows="3" @input="changeRuleSource" />
      </label>
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
          >
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
