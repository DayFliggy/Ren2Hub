<script setup lang="ts">
import './admin.css'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import PricingExpressionEditor from './PricingExpressionEditor.vue'
import {
  pricingConfigApi,
  type PricingValues,
  type ModelPricingEntry,
  type PricingPreview,
} from '@/api/pricingConfig'
import {
  TOKEN_LANES,
  decimalText,
  tokenPrices,
  tokenPricing,
  pricingMode,
  clonePricing,
  validatePricing,
  type PricingMode,
} from '@/features/pricing/draft'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { isRecord } from '@/api/contracts'
import { isDecimal, multiplyDecimal, divideDecimal } from '@/utils/decimal'
import { getQuotaPerUnit } from '@/utils/quota'
const props = withDefaults(
  defineProps<{
    modelName: string
    pricing: PricingValues
    entry?: ModelPricingEntry
    revision?: number
    saving?: boolean
    siteCurrency?: string
    exchangeRate?: string
  }>(),
  {
    entry: undefined,
    revision: 0,
    saving: false,
    siteCurrency: 'USD',
    exchangeRate: '1',
  }
)
const emit = defineEmits<{
  update: [name: string, pricing: PricingValues]
  save: []
  dirty: [dirty: boolean]
}>()
const { t, te, locale } = useI18n({
  useScope: 'local',
  messages: modelPricingMessages,
})
const name = ref(''),
  mode = ref<PricingMode>('tiered_expr'),
  fixed = ref(''),
  expression = ref('')
const prices = reactive<Record<string, string>>({ input: '' })
const enabled = reactive<Record<string, boolean>>({})
const pluginExpressions = ref<Record<string, string>>({}),
  plugin = ref('')
const taskPrices = reactive<Record<string, string>>(
  Object.create(null) as Record<string, string>
)
const taskConditions = ref<
    Array<{ field: string; operator: string; value: string }>
  >([]),
  taskMultiplier = ref('')
const currency = ref('USD'),
  error = ref(''),
  conversion = ref<PricingPreview | null>(null),
  converting = ref(false)
const preview = ref<PricingPreview | null>(null),
  previewError = ref('')
const expressionEditor = ref<InstanceType<
    typeof PricingExpressionEditor
  > | null>(null),
  // Each hydration mounts a fresh expression editor, so no draft, mode or
  // error carries over to another model or a reloaded price.
  hydration = ref(0)
let baseline = '',
  defaultBaseline = '',
  hydrating = false,
  previewTimer: ReturnType<typeof setTimeout> | undefined,
  previewController: AbortController | undefined
const variant = computed(() =>
  props.entry?.plugin_variants?.find((item) => item.plugin_key === plugin.value)
)
const schema = computed(
  () => variant.value?.usage_schema ?? props.entry?.usage_schema
)
const activeExpression = computed({
  get: () =>
    plugin.value
      ? (pluginExpressions.value[plugin.value] ?? '')
      : expression.value,
  set: (value) => {
    if (plugin.value)
      pluginExpressions.value = {
        ...pluginExpressions.value,
        [plugin.value]: value,
      }
    else expression.value = value
  },
})
const rate = computed(() =>
  currency.value === 'USD' ? '1' : props.exchangeRate
)
const label = (value: unknown) =>
  typeof value === 'string'
    ? value
    : isRecord(value)
      ? String(
          value[locale.value] ??
            value[locale.value === 'zh-CN' ? 'zh' : 'en'] ??
            ''
        )
      : ''
function defaultPricingSnapshot() {
  return JSON.stringify([
    mode.value,
    prices,
    enabled,
    fixed.value,
    expression.value,
  ])
}
function formSnapshot() {
  return JSON.stringify([
    name.value,
    defaultPricingSnapshot(),
    pluginExpressions.value,
  ])
}
// An expression draft that cannot be written yet is an unsaved edit as well.
function dirty() {
  return baseline !== formSnapshot() || Boolean(expressionEditor.value?.issue)
}
function captureDefaultBaseline() {
  defaultBaseline = defaultPricingSnapshot()
  baseline = JSON.stringify([
    props.modelName,
    defaultBaseline,
    props.pricing['billing_setting.plugin_billing_expr'] ?? {},
  ])
}
watch(
  () => [props.modelName, props.revision],
  () => {
    hydrating = true
    name.value = props.modelName
    mode.value = Object.keys(props.pricing).length
      ? pricingMode(props.pricing)
      : 'tiered_expr'
    fixed.value =
      props.pricing.ModelPrice === undefined
        ? ''
        : decimalText(props.pricing.ModelPrice)
    expression.value = String(
      props.pricing['billing_setting.billing_expr'] ?? ''
    )
    pluginExpressions.value = clonePricing(
      props.pricing['billing_setting.plugin_billing_expr'] ?? {}
    )
    currency.value = 'USD'
    plugin.value = ''
    hydration.value++
    error.value = ''
    preview.value = null
    Object.assign(prices, tokenPrices(props.pricing))
    for (const lane of TOKEN_LANES)
      enabled[lane.key] = props.pricing[lane.key] !== undefined
    captureDefaultBaseline()
    hydrating = false
  },
  { immediate: true }
)
watch(getQuotaPerUnit, () => {
  // Keep edited dollar amounts as user intent. An untouched ratio must instead
  // be reprojected so its displayed price follows the newly published scale.
  if (defaultBaseline !== defaultPricingSnapshot()) return
  Object.assign(prices, tokenPrices(props.pricing))
  captureDefaultBaseline()
})
watch(schema, () => {
  for (const key of Object.keys(taskPrices)) delete taskPrices[key]
  taskConditions.value = []
  taskMultiplier.value = ''
})
function build(): PricingValues {
  const issue = expressionEditor.value?.issue
  if (issue) throw new Error(issue)
  const plugins = Object.fromEntries(
    Object.entries(pluginExpressions.value).filter(([, value]) => value.trim())
  )
  let result: PricingValues
  if (
    defaultBaseline === defaultPricingSnapshot() &&
    (Object.keys(props.pricing).length || Object.keys(plugins).length)
  ) {
    // Editing a plugin must preserve an absent default or its exact prices.
    result = clonePricing(props.pricing)
    delete result['billing_setting.plugin_billing_expr']
  } else if (mode.value === 'tiered_expr')
    result = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': expression.value.trim(),
    }
  else if (mode.value === 'per-request') {
    if (!isDecimal(fixed.value)) throw new Error('invalid')
    result = { ModelPrice: divideDecimal(fixed.value, rate.value) }
  } else {
    const usd: Record<string, string> = {
      input:
        prices.input && isDecimal(prices.input)
          ? divideDecimal(prices.input, rate.value)
          : (prices.input ?? ''),
    }
    for (const lane of TOKEN_LANES)
      if (enabled[lane.key])
        usd[lane.key] =
          prices[lane.key] && isDecimal(prices[lane.key])
            ? divideDecimal(prices[lane.key]!, rate.value)
            : (prices[lane.key] ?? '')
    result = tokenPricing(usd)
  }
  if (Object.keys(plugins).length)
    result['billing_setting.plugin_billing_expr'] = plugins
  validatePricing(result)
  return result
}
function commit(): { name: string; pricing: PricingValues } | null {
  error.value = ''
  try {
    if (!name.value.trim()) throw new Error('unknownModel')
    const pricing =
      !dirty() && Object.keys(props.pricing).length
        ? clonePricing(props.pricing)
        : build()
    validatePricing(pricing)
    emit('update', name.value.trim(), pricing)
    return { name: name.value.trim(), pricing }
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause)
    error.value = te(message) ? t(message) : message
    return null
  }
}
watch(dirty, (value) => emit('dirty', value))
// A commit error describes the draft it rejected, so any later change of the
// draft or of its expression issue clears it.
watch(
  () => expressionEditor.value?.issue ?? '',
  () => (error.value = '')
)
watch([formSnapshot, plugin, getQuotaPerUnit], () => {
  if (hydrating) return
  error.value = ''
  preview.value = null
  previewError.value = ''
  clearTimeout(previewTimer)
  previewController?.abort()
  previewTimer = setTimeout(async () => {
    if (!name.value.trim()) return
    let pricing: PricingValues
    try {
      pricing = build()
    } catch {
      return
    }
    const controller = new AbortController()
    previewController = controller
    try {
      const result = await pricingConfigApi.preview(
        name.value.trim(),
        pricing,
        controller.signal
      )
      if (!controller.signal.aborted) preview.value = result
    } catch (cause) {
      if (!controller.signal.aborted)
        previewError.value =
          cause instanceof Error ? cause.message : String(cause)
    }
  }, 350)
})
function changeCurrency(event: Event) {
  const next = (event.target as HTMLSelectElement).value
  const nextRate = next === 'USD' ? '1' : props.exchangeRate
  if (!isDecimal(nextRate) || Number(nextRate) <= 0) {
    error.value = t('invalid')
    return
  }
  const unchanged = defaultBaseline === defaultPricingSnapshot()
  const sourcePrices = unchanged ? tokenPrices(props.pricing) : prices
  const sourceFixed = unchanged
    ? props.pricing.ModelPrice === undefined
      ? ''
      : decimalText(props.pricing.ModelPrice)
    : fixed.value
  const factor = unchanged ? nextRate : divideDecimal(nextRate, rate.value)
  for (const key of Object.keys(sourcePrices))
    if (isDecimal(sourcePrices[key]))
      prices[key] = multiplyDecimal(sourcePrices[key]!, factor)
  if (isDecimal(sourceFixed)) fixed.value = multiplyDecimal(sourceFixed, factor)
  currency.value = next
  if (unchanged) captureDefaultBaseline()
}
async function convert() {
  error.value = ''
  converting.value = true
  const before = formSnapshot()
  try {
    const result = await pricingConfigApi.convert(name.value.trim(), build())
    if (result.unsupported_reason) throw new Error(result.unsupported_reason)
    if (!result.expression) throw new Error(t('unsupported'))
    if (before === formSnapshot()) conversion.value = result
  } catch (cause) {
    error.value =
      cause instanceof Error
        ? te(cause.message)
          ? t(cause.message)
          : cause.message
        : String(cause)
  } finally {
    converting.value = false
  }
}
function applyConversion() {
  if (!conversion.value?.expression) return
  expression.value = conversion.value.expression
  mode.value = 'tiered_expr'
  conversion.value = null
}
function createTaskExpression() {
  const terms = Object.entries(taskPrices).filter(([, value]) => value !== '')
  if (!terms.length || terms.some(([, value]) => !isDecimal(value))) {
    error.value = t('invalid')
    return
  }
  activeExpression.value = `${terms.map(([key, value]) => `u(${JSON.stringify(key)}) * ${value}`).join(' + ')}`
}
function applyTaskConditions() {
  error.value = ''
  if (
    !activeExpression.value.trim() ||
    !isDecimal(taskMultiplier.value) ||
    !taskConditions.value.length
  ) {
    error.value = t('requestRuleInvalid')
    return
  }
  const predicates: string[] = []
  for (const condition of taskConditions.value) {
    const field = schema.value?.[condition.field]
    if (!field || condition.value === '') {
      error.value = t('requestRuleInvalid')
      return
    }
    const numeric = !field.enum && field.type !== 'boolean'
    if (numeric && !isDecimal(condition.value)) {
      error.value = t('invalid')
      return
    }
    const value =
      field.type === 'boolean'
        ? String(condition.value === 'true')
        : numeric
          ? condition.value
          : JSON.stringify(condition.value)
    predicates.push(
      'u(' +
        JSON.stringify(condition.field) +
        ') ' +
        condition.operator +
        ' ' +
        value
    )
  }
  const source = activeExpression.value.trim()
  const prefix = source.startsWith('v1:') ? 'v1:' : ''
  const body = source.slice(prefix.length)
  activeExpression.value =
    prefix +
    '(' +
    body +
    ') * ((' +
    predicates.join(' && ') +
    ') ? ' +
    taskMultiplier.value +
    ' : 1)'
}
onBeforeUnmount(() => {
  clearTimeout(previewTimer)
  previewController?.abort()
})
defineExpose({ commit, dirty })
</script>
<template>
  <section class="pricing-editor" :aria-label="t('edit')">
    <header class="pricing-editor-header">
      <h2>{{ t(modelName ? 'edit' : 'add') }}</h2>
      <span
        v-if="modelName"
        class="rounded-md border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-2 py-0.5 font-mono text-xs font-medium text-[var(--text-secondary)]"
      >
        {{ modelName }}
      </span>
    </header>
    <div class="pricing-editor-scroll">
      <label class="admin-field"
        ><span>{{ t('name') }}</span
        ><input
          v-model="name"
          :disabled="Boolean(modelName)"
          :placeholder="t('inputPrompt')"
      /></label>
      <label v-if="entry?.plugin_variants?.length" class="admin-field"
        ><span>{{ t('plugin') }}</span
        ><select v-model="plugin">
          <option value="">{{ t('modelDefault') }}</option>
          <option
            v-for="item in entry.plugin_variants"
            :key="item.plugin_key"
            :value="item.plugin_key"
          >
            {{ item.plugin_name }}
          </option>
        </select></label
      >
      <label v-if="!plugin" class="admin-field"
        ><span>{{ t('currency') }}</span
        ><select :value="currency" @change="changeCurrency">
          <option value="USD">USD</option>
          <option v-if="siteCurrency !== 'USD'" :value="siteCurrency">
            {{ siteCurrency }}
          </option>
        </select></label
      >
      <p v-if="!plugin" class="admin-muted">{{ t('currencyHint') }}</p>
      <div
        v-if="!plugin"
        class="pricing-mode"
        role="tablist"
        :aria-label="t('mode')"
      >
        <button
          v-for="item in [
            { id: 'tiered_expr', label: 'expression' },
            { id: 'per-token', label: 'token' },
            { id: 'per-request', label: 'request' },
          ]"
          :key="item.id"
          role="tab"
          :aria-selected="mode === item.id"
          type="button"
          @click="mode = item.id as PricingMode"
        >
          {{ t(item.label) }}
        </button>
      </div>
      <div v-if="!plugin && mode !== 'tiered_expr'" class="pricing-convert">
        <p>{{ t('conversionHint') }}</p>
        <ConsoleButton
          size="sm"
          :loading="converting"
          :disabled="Boolean(expression.trim())"
          @click="convert"
          >{{ t('convert') }}</ConsoleButton
        >
      </div>
      <template v-if="!plugin && mode === 'per-token'">
        <p class="admin-muted">{{ t('noDefault') }}</p>
        <label class="admin-field"
          ><span>{{ t('input') }} · {{ currency }} {{ t('perMillion') }}</span
          ><input
            v-model="prices.input"
            inputmode="decimal"
            :placeholder="t('zeroHint')"
        /></label>
        <div v-for="lane in TOKEN_LANES" :key="lane.key" class="pricing-lane">
          <label class="admin-check"
            ><input
              v-model="enabled[lane.key]"
              type="checkbox"
              :disabled="
                lane.key === 'AudioCompletionRatio' && !enabled.AudioRatio
              "
              @change="
                !enabled.AudioRatio && (enabled.AudioCompletionRatio = false)
              "
            />{{ t(lane.label) }}</label
          ><input
            v-model="prices[lane.key]"
            :aria-label="t(lane.label)"
            inputmode="decimal"
            :disabled="!enabled[lane.key]"
          /><small>{{ currency }} {{ t('perMillion') }}</small>
        </div>
      </template>
      <label v-else-if="!plugin && mode === 'per-request'" class="admin-field"
        ><span>{{ t('fixed') }} · {{ currency }} {{ t('perRequest') }}</span
        ><input
          v-model="fixed"
          inputmode="decimal"
          :placeholder="t('zeroHint')"
      /></label>
      <template v-else>
        <p v-if="variant?.stale" class="admin-error">{{ t('stale') }}</p>
        <p v-if="variant && !variant.compatible" class="admin-error">
          {{ t('unavailable') }}
        </p>
        <p v-if="plugin" class="admin-muted">{{ t('inherited') }}</p>
        <PricingExpressionEditor
          :key="`${hydration}:${plugin}`"
          ref="expressionEditor"
          v-model="activeExpression"
        />
        <details v-if="schema && Object.keys(schema).length">
          <summary>{{ t('schema') }}</summary>
          <div v-for="(field, key) in schema" :key="key" class="pricing-lane">
            <label class="admin-field"
              ><span
                >{{ label(field.description) || key }}
                <small>{{ field.unit }}</small></span
              ><input
                v-if="field.type !== 'boolean' && !field.enum"
                v-model="taskPrices[key]"
                inputmode="decimal"
                :placeholder="t('usagePrice')"
              /><span v-else>{{
                field.enum?.join(', ') || field.type
              }}</span></label
            >
          </div>
          <ConsoleButton
            variant="secondary"
            size="sm"
            @click="createTaskExpression"
            >{{ t('addTaskPrice') }}</ConsoleButton
          >
          <div
            v-for="(condition, index) in taskConditions"
            :key="index"
            class="pricing-task-condition"
          >
            <label class="admin-field"
              ><span>{{ t('schemaValue') }}</span
              ><select v-model="condition.field">
                <option value="">{{ t('choose') }}</option>
                <option v-for="(field, key) in schema" :key="key" :value="key">
                  {{ label(field.description) || key }}
                </option>
              </select></label
            ><label class="admin-field"
              ><span>{{ t('operator') }}</span
              ><select v-model="condition.operator">
                <option
                  v-for="operator in ['==', '!=', '>', '>=', '<', '<=']"
                  :key="operator"
                >
                  {{ operator }}
                </option>
              </select></label
            ><label class="admin-field"
              ><span>{{ t('value') }}</span
              ><select
                v-if="
                  schema[condition.field]?.enum ||
                  schema[condition.field]?.type === 'boolean'
                "
                v-model="condition.value"
              >
                <option value="">{{ t('choose') }}</option>
                <option
                  v-for="value in schema[condition.field]?.enum ?? [
                    'true',
                    'false',
                  ]"
                  :key="value"
                >
                  {{ value }}
                </option></select
              ><input
                v-else
                v-model="condition.value"
                inputmode="decimal" /></label
            ><ConsoleButton
              size="sm"
              variant="ghost"
              @click="taskConditions.splice(index, 1)"
              >{{ t('remove') }}</ConsoleButton
            >
          </div>
          <ConsoleButton
            size="sm"
            variant="ghost"
            @click="
              taskConditions.push({ field: '', operator: '==', value: '' })
            "
            >{{ t('addCondition') }}</ConsoleButton
          ><template v-if="taskConditions.length"
            ><label class="admin-field"
              ><span>{{ t('multiplier') }}</span
              ><input v-model="taskMultiplier" inputmode="decimal" /></label
            ><ConsoleButton
              size="sm"
              variant="secondary"
              @click="applyTaskConditions"
              >{{ t('apply') }}</ConsoleButton
            ></template
          >
        </details>
      </template>
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <aside class="pricing-preview">
        <h3>{{ t('preview') }}</h3>
        <p v-if="previewError" class="admin-error">{{ previewError }}</p>
        <pre v-if="preview">{{
          JSON.stringify(preview.effective, null, 2)
        }}</pre>
        <p v-else class="admin-muted">{{ t('savedOnly') }}</p>
      </aside>
    </div>
    <footer class="pricing-editor-footer">
      <ConsoleButton :loading="saving" @click="commit() && emit('save')">{{
        t('save')
      }}</ConsoleButton>
    </footer>
    <ConsoleModal
      :open="Boolean(conversion)"
      :title="t('conversion')"
      @close="conversion = null"
      ><p>{{ t('conversionHint') }}</p>
      <pre>{{ conversion?.expression }}</pre>
      <ConsoleButton @click="applyConversion">{{
        t('confirm')
      }}</ConsoleButton></ConsoleModal
    >
  </section>
</template>
<style scoped>
.pricing-editor {
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: transparent;
}
.pricing-editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 1rem 1.25rem;
  background: var(--surface-solid);
  border-bottom: 1px solid var(--border-subtle);
}
.pricing-editor-header h2 {
  font-size: 0.9375rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}
.pricing-editor-footer {
  padding: 0.875rem 1.25rem;
  border-top: 1px solid var(--border-subtle);
  background: var(--surface-solid);
  display: flex;
  justify-content: flex-end;
}
/* As in NewAPI's editor panel, Save stays in view while the form scrolls.
   The page scrolls here instead of an inner panel, so the footer sticks to
   the bottom of the scroll zone. The mobile drawer keeps it in flow. */
@media (min-width: 768px) {
  .pricing-editor-footer {
    position: sticky;
    bottom: 0;
    z-index: 1;
    /* Matches the hosting ConsoleCard's rounded-2xl bottom corners. */
    border-radius: 0 0 1rem 1rem;
  }
}
h2,
h3 {
  font-weight: 600;
}
.pricing-editor-scroll {
  display: grid;
  gap: 1rem;
  min-height: 0;
  padding: 1.25rem;
}
.pricing-mode {
  display: flex;
  gap: 0.25rem;
  padding: 0.25rem;
  background: var(--surface-muted);
  border-radius: 0.5rem;
  border: 1px solid var(--border-subtle);
}
.pricing-mode button {
  flex: 1;
  padding: 0.5rem 0.625rem;
  font-size: 0.8125rem;
  font-weight: 500;
  border-radius: 0.375rem;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.15s ease;
}
.pricing-mode button[aria-selected='true'] {
  background: var(--surface-solid);
  color: var(--text-primary);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.pricing-task-condition {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 0.5rem;
  margin: 0.75rem 0;
  padding: 0.75rem;
  background: var(--surface-muted);
  border-radius: 0.5rem;
  border: 1px solid var(--border-subtle);
}
.pricing-task-condition label {
  flex: 1 1 7rem;
}
.pricing-lane {
  display: grid;
  gap: 0.5rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-solid);
}
.pricing-lane > input {
  width: 100%;
  border: 1px solid var(--border-subtle);
  padding: 0.4375rem 0.75rem;
  border-radius: 0.375rem;
  background: var(--surface-solid);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.8125rem;
}
.pricing-convert {
  padding: 0.875rem 1rem;
  border: 1px solid var(--accent-soft);
  border-radius: 0.5rem;
  background: var(--accent-soft);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
.pricing-convert p {
  margin: 0;
  font-size: 0.8125rem;
  color: var(--text-secondary);
}
.pricing-preview {
  padding: 1rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-solid);
  display: grid;
  gap: 0.5rem;
}
.pricing-preview pre {
  padding: 0.75rem;
  background: var(--surface-muted);
  border: 1px solid var(--border-subtle);
  border-radius: 0.375rem;
  font-size: 0.75rem;
  max-height: 12rem;
  overflow-y: auto;
  color: var(--text-primary);
}
pre,
textarea {
  max-width: 100%;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.8125rem;
}
summary {
  cursor: pointer;
  padding: 0.375rem 0;
  font-weight: 600;
  font-size: 0.8125rem;
  color: var(--text-primary);
  user-select: none;
}
details {
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  padding: 0.75rem 1rem;
  background: var(--surface-solid);
}
</style>
