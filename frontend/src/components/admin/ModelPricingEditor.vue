<script setup lang="ts">
import './admin.css'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import PricingAmountInput from './PricingAmountInput.vue'
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
import { translateError } from '@/features/pricing/errors'
import { compileBillingExpression } from '@/features/pricing/lib/billing-expression/parser'
import { pricingPreviewRows } from '@/features/pricing/previewRows'
import {
  readTaskPrices,
  taskConditionExpression,
  taskPriceExpression,
} from '@/features/pricing/taskExpression'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { isRecord } from '@/api/contracts'
import { isDecimal } from '@/utils/decimal'
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
// prices and fixed hold USD; the display currency only changes the inputs.
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
  // The zero ordinary price stays unsaved until the administrator prices it.
  ordinaryPlaceholder = '',
  hydrating = false,
  previewTimer: ReturnType<typeof setTimeout> | undefined,
  previewController: AbortController | undefined,
  convertController: AbortController | undefined
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
// The site currency is offered only with a usable exchange rate.
const localCurrency = computed(() =>
  props.siteCurrency !== 'USD' &&
  isDecimal(props.exchangeRate) &&
  Number(props.exchangeRate) > 0
    ? props.siteCurrency
    : ''
)
const rate = computed(() =>
  currency.value === localCurrency.value ? props.exchangeRate : '1'
)
// Plugin prices are edited as source only, so they always show USD.
const expressionCurrency = computed(() =>
  plugin.value
    ? { rate: '1', name: 'USD' }
    : { rate: rate.value, name: currency.value }
)
const previewRows = computed(() =>
  preview.value
    ? pricingPreviewRows(
        preview.value.effective,
        preview.value.cache_write_mode,
        preview.value.billing_details
      )
    : []
)
// Plugins without their own expression bill with the model default, and a
// u() default cannot price ordinary requests at all.
const inheritingPlugins = computed(() =>
  (props.entry?.plugin_variants ?? []).filter(
    (item) => !item.stale && !pluginExpressions.value[item.plugin_key]?.trim()
  )
)
const ordinaryPricingAvailable = computed(
  () =>
    !plugin.value &&
    mode.value === 'tiered_expr' &&
    Boolean(expression.value.trim()) &&
    (inheritingPlugins.value.length > 0 ||
      (Boolean(props.entry?.plugin_variants?.some((item) => !item.stale)) &&
        usesTaskUsage(expression.value)))
)
function usesTaskUsage(source: string) {
  const compiled = compileBillingExpression(source.trim())
  return compiled.status === 'ready' && compiled.functions.has('u')
}
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
    ordinaryPlaceholder = ''
    hydration.value++
    error.value = ''
    preview.value = null
    convertController?.abort()
    conversion.value = null
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
// The task generator starts from the unit prices billed today.
watch(
  [schema, hydration],
  () => {
    for (const key of Object.keys(taskPrices)) delete taskPrices[key]
    Object.assign(
      taskPrices,
      readTaskPrices(activeExpression.value, schema.value)
    )
    taskConditions.value = []
    taskMultiplier.value = ''
  },
  { immediate: true }
)
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
  } else if (mode.value === 'tiered_expr') {
    if (expression.value.trim() === ordinaryPlaceholder)
      throw new Error('ordinaryPriceRequired')
    result = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': expression.value.trim(),
    }
  } else if (mode.value === 'per-request') {
    if (!isDecimal(fixed.value)) throw new Error('invalid')
    result = { ModelPrice: fixed.value }
  } else {
    const usd: Record<string, string> = { input: prices.input ?? '' }
    for (const lane of TOKEN_LANES)
      if (enabled[lane.key]) usd[lane.key] = prices[lane.key] ?? ''
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
    error.value = translateError(cause, t, te)
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
  // A response for an older draft must not describe the current one.
  previewController?.abort()
  convertController?.abort()
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
async function convert() {
  error.value = ''
  convertController?.abort()
  const controller = new AbortController()
  convertController = controller
  converting.value = true
  try {
    const result = await pricingConfigApi.convert(
      name.value.trim(),
      build(),
      controller.signal
    )
    if (controller.signal.aborted) return
    if (result.unsupported_reason) throw new Error(result.unsupported_reason)
    if (!result.expression) throw new Error('unsupported')
    conversion.value = result
  } catch (cause) {
    if (!controller.signal.aborted) error.value = translateError(cause, t, te)
  } finally {
    if (convertController === controller) converting.value = false
  }
}
function applyConversion() {
  if (!conversion.value?.expression) return
  expression.value = conversion.value.expression
  mode.value = 'tiered_expr'
  conversion.value = null
}
// Absorbed from upstream 8802113 configureOrdinaryPricing: inheriting plugins
// keep the current expression as their own override, then the model default
// becomes an ordinary relay price, per returned image when the model has one.
function configureOrdinaryPricing() {
  const inherited = expression.value.trim()
  const inheriting = inheritingPlugins.value
  const next = { ...pluginExpressions.value }
  for (const item of inheriting) next[item.plugin_key] = inherited
  pluginExpressions.value = next
  const perImage = [props.entry, ...inheriting].some(
    (item) => item?.usage_schema?.image_count?.unit === 'count'
  )
  expression.value = perImage
    ? 'tier("image", fixed(0)) * image_count'
    : 'tier("base", p * 0 + c * 0)'
  ordinaryPlaceholder = expression.value
  hydration.value++
}
function createTaskExpression() {
  error.value = ''
  try {
    activeExpression.value = taskPriceExpression(taskPrices, schema.value ?? {})
  } catch (cause) {
    error.value = translateError(cause, t, te)
  }
}
function applyTaskConditions() {
  error.value = ''
  try {
    activeExpression.value = taskConditionExpression(
      activeExpression.value,
      taskConditions.value,
      taskMultiplier.value,
      schema.value ?? {}
    )
  } catch (cause) {
    error.value = translateError(cause, t, te)
  }
}
onBeforeUnmount(() => {
  clearTimeout(previewTimer)
  previewController?.abort()
  convertController?.abort()
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
        ><select v-model="currency">
          <option value="USD">USD</option>
          <option v-if="localCurrency" :value="localCurrency">
            {{ localCurrency }}
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
      <div v-if="ordinaryPricingAvailable" class="pricing-convert">
        <p>{{ t('ordinaryPricingHint') }}</p>
        <ConsoleButton size="sm" @click="configureOrdinaryPricing">{{
          t('ordinaryPricing')
        }}</ConsoleButton>
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
        <p class="admin-muted">{{ t('laneFallback') }}</p>
        <label class="admin-field"
          ><span>{{ t('input') }} · {{ currency }} {{ t('perMillion') }}</span
          ><PricingAmountInput
            v-model="prices.input"
            :rate="rate"
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
          ><PricingAmountInput
            v-model="prices[lane.key]!"
            :rate="rate"
            :aria-label="t(lane.label)"
            :disabled="!enabled[lane.key]"
          /><small>{{ currency }} {{ t('perMillion') }}</small>
        </div>
      </template>
      <label v-else-if="!plugin && mode === 'per-request'" class="admin-field"
        ><span>{{ t('fixed') }} · {{ currency }} {{ t('perRequest') }}</span
        ><PricingAmountInput
          v-model="fixed"
          :rate="rate"
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
          :rate="expressionCurrency.rate"
          :currency="expressionCurrency.name"
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
        <dl v-if="previewRows.length" class="pricing-preview-rows">
          <div v-for="row in previewRows" :key="row.key">
            <dt>{{ row.translateLabel ? t(row.label) : row.label }}</dt>
            <dd>
              {{ row.value }}
              <small v-if="row.unit !== 'none'">{{ t(row.unit) }}</small>
            </dd>
          </div>
        </dl>
        <p v-else-if="!preview" class="admin-muted">{{ t('savedOnly') }}</p>
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
.pricing-preview-rows {
  display: grid;
  gap: 0.375rem;
  margin: 0;
  font-size: 0.8125rem;
}
.pricing-preview-rows div {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
  min-width: 0;
}
.pricing-preview-rows dt {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.pricing-preview-rows dd {
  margin: 0;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  text-align: right;
  overflow-wrap: anywhere;
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
/* Matches ConsoleButton: 44px touch targets below the sm breakpoint. The
   condition and tier fields are child components, hence :deep. */
@media (max-width: 639px) {
  .pricing-editor :deep(.admin-field input:not([type='checkbox'])),
  .pricing-editor :deep(.admin-field select) {
    min-height: 2.75rem;
  }
}
</style>
