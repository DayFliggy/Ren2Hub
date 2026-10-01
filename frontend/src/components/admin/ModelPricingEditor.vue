<script setup lang="ts">
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
import {
  isDecimal,
  multiplyDecimal,
  divideDecimal,
  formatDecimalUSD,
} from '@/utils/decimal'
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
> | null>(null)
const usage = ref('{"p":1000,"c":1000,"len":1000}'),
  params = ref('{}'),
  headers = ref('{}'),
  time = ref(new Date().toISOString())
const simulation = ref(''),
  simulationError = ref(''),
  simulating = ref(false)
let baseline = '',
  hydrating = false,
  previewTimer: ReturnType<typeof setTimeout> | undefined,
  previewController: AbortController | undefined
const variant = computed(() =>
  props.entry?.plugin_variants?.find((item) => item.plugin_key === plugin.value)
)
const schema = computed(
  () => variant.value?.usage_schema ?? props.entry?.usage_schema
)
const taskSimulation = computed(() => schema.value !== undefined)
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
function formSnapshot() {
  return JSON.stringify([
    name.value,
    mode.value,
    prices,
    enabled,
    fixed.value,
    expression.value,
    pluginExpressions.value,
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
    error.value = ''
    simulation.value = ''
    preview.value = null
    Object.assign(prices, tokenPrices(props.pricing))
    for (const lane of TOKEN_LANES)
      enabled[lane.key] = props.pricing[lane.key] !== undefined
    baseline = formSnapshot()
    hydrating = false
  },
  { immediate: true }
)
watch(schema, () => {
  for (const key of Object.keys(taskPrices)) delete taskPrices[key]
  taskConditions.value = []
  taskMultiplier.value = ''
})
function build(): PricingValues {
  let result: PricingValues
  if (mode.value === 'tiered_expr')
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
  const plugins = Object.fromEntries(
    Object.entries(pluginExpressions.value).filter(([, value]) => value.trim())
  )
  if (Object.keys(plugins).length)
    result['billing_setting.plugin_billing_expr'] = plugins
  validatePricing(result)
  return result
}
function commit(): { name: string; pricing: PricingValues } | null {
  error.value = ''
  try {
    expressionEditor.value?.validate()
    if (!name.value.trim()) throw new Error('unknownModel')
    const pricing =
      baseline === formSnapshot() && Object.keys(props.pricing).length
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
watch(formSnapshot, () => {
  if (hydrating) return
  emit('dirty', baseline !== formSnapshot())
  preview.value = null
  previewError.value = ''
  simulation.value = ''
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
  const factor = divideDecimal(nextRate, rate.value)
  for (const key of Object.keys(prices))
    if (isDecimal(prices[key]))
      prices[key] = multiplyDecimal(prices[key]!, factor)
  if (isDecimal(fixed.value)) fixed.value = multiplyDecimal(fixed.value, factor)
  currency.value = next
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
async function simulate() {
  simulationError.value = ''
  simulation.value = ''
  simulating.value = true
  const before = JSON.stringify([
    formSnapshot(),
    plugin.value,
    usage.value,
    params.value,
    headers.value,
    time.value,
  ])
  try {
    const draft = build(),
      parsedUsage: unknown = JSON.parse(usage.value),
      parsedParams: unknown = taskSimulation.value
        ? {}
        : JSON.parse(params.value),
      parsedHeaders: unknown = taskSimulation.value
        ? {}
        : JSON.parse(headers.value)
    if (
      !isRecord(parsedUsage) ||
      !isRecord(parsedParams) ||
      !isRecord(parsedHeaders) ||
      Object.values(parsedHeaders).some((value) => typeof value !== 'string')
    )
      throw new Error('badJson')
    const result = await pricingConfigApi.simulate({
      model_name: name.value,
      ...(plugin.value ? { plugin_key: plugin.value } : {}),
      pricing: draft,
      usage: parsedUsage,
      request: {
        params: parsedParams,
        headers: parsedHeaders as Record<string, string>,
      },
      time: time.value,
    })
    if (
      before !==
      JSON.stringify([
        formSnapshot(),
        plugin.value,
        usage.value,
        params.value,
        headers.value,
        time.value,
      ])
    )
      return
    simulation.value = isDecimal(result.amount_usd)
      ? formatDecimalUSD(result.amount_usd)
      : String(result.amount_usd)
  } catch (cause) {
    simulationError.value =
      cause instanceof Error
        ? te(cause.message)
          ? t(cause.message)
          : cause.message
        : String(cause)
  } finally {
    simulating.value = false
  }
}
onBeforeUnmount(() => {
  clearTimeout(previewTimer)
  previewController?.abort()
})
defineExpose({ commit, dirty: () => baseline !== formSnapshot() })
</script>
<template>
  <section class="pricing-editor" :aria-label="t('edit')">
    <header>
      <h2>{{ t(modelName ? 'edit' : 'add') }}</h2>
    </header>
    <div class="pricing-editor-scroll">
      <label class="admin-field"
        ><span>{{ t('name') }}</span
        ><input
          v-model="name"
          :disabled="Boolean(modelName)"
          :placeholder="t('inputPrompt')"
      /></label>
      <label class="admin-field"
        ><span>{{ t('currency') }}</span
        ><select :value="currency" @change="changeCurrency">
          <option value="USD">USD</option>
          <option v-if="siteCurrency !== 'USD'" :value="siteCurrency">
            {{ siteCurrency }}
          </option>
        </select></label
      >
      <p class="admin-muted">{{ t('currencyHint') }}</p>
      <div class="pricing-mode" role="tablist" :aria-label="t('mode')">
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
      <div v-if="mode !== 'tiered_expr'" class="pricing-convert">
        <p>{{ t('conversionHint') }}</p>
        <ConsoleButton
          size="sm"
          :loading="converting"
          :disabled="Boolean(expression.trim())"
          @click="convert"
          >{{ t('convert') }}</ConsoleButton
        >
      </div>
      <template v-if="mode === 'per-token'">
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
      <label v-else-if="mode === 'per-request'" class="admin-field"
        ><span>{{ t('fixed') }} · {{ currency }} {{ t('perRequest') }}</span
        ><input
          v-model="fixed"
          inputmode="decimal"
          :placeholder="t('zeroHint')"
      /></label>
      <template v-else>
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
        <p v-if="variant?.stale" class="admin-error">{{ t('stale') }}</p>
        <p v-if="variant && !variant.compatible" class="admin-error">
          {{ t('unavailable') }}
        </p>
        <p v-if="plugin" class="admin-muted">{{ t('inherited') }}</p>
        <PricingExpressionEditor
          :key="plugin"
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
      <details class="pricing-simulation">
        <summary>{{ t('simulation') }}</summary>
        <p class="admin-muted">{{ t('simulationHint') }}</p>
        <p v-if="taskSimulation" class="admin-muted">
          {{ t('taskSimulationHint') }}
        </p>
        <ConsoleButton
          v-for="example in variant?.usage_examples ?? []"
          :key="example.label"
          size="sm"
          variant="ghost"
          @click="usage = JSON.stringify(example.facts, null, 2)"
          >{{ example.label }}</ConsoleButton
        ><label class="admin-field"
          ><span>{{ t('usage') }}</span
          ><textarea v-model="usage" rows="3" /></label
        ><label v-if="!taskSimulation" class="admin-field"
          ><span>{{ t('params') }}</span
          ><textarea v-model="params" rows="3" /></label
        ><label v-if="!taskSimulation" class="admin-field"
          ><span>{{ t('headers') }}</span
          ><textarea v-model="headers" rows="2" /></label
        ><label class="admin-field"
          ><span>{{ t('instant') }}</span
          ><input v-model="time" /></label
        ><ConsoleButton
          :loading="simulating"
          variant="secondary"
          @click="simulate"
          >{{ t('simulate') }}</ConsoleButton
        >
        <p v-if="simulation" role="status">
          {{ t('result') }}: {{ simulation }}
        </p>
        <p v-if="simulationError" role="alert" class="admin-error">
          {{ simulationError }}
        </p>
      </details>
    </div>
    <footer>
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
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  overflow: hidden;
}
header,
footer {
  padding: 1rem;
  border-bottom: 1px solid var(--border-subtle);
}
footer {
  border-top: 1px solid var(--border-subtle);
  border-bottom: 0;
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
  overflow-y: auto;
  padding: 1rem;
  max-height: 66vh;
}
.pricing-mode {
  display: flex;
  gap: 0.25rem;
  border-bottom: 1px solid var(--border-subtle);
}
.pricing-mode button {
  flex: 1;
  padding: 0.75rem 0.4rem;
  font-size: 0.8rem;
  border-bottom: 2px solid transparent;
}
.pricing-mode button[aria-selected='true'] {
  border-color: var(--text-primary);
  font-weight: 600;
}
.pricing-task-condition {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 0.5rem;
  margin: 0.75rem 0;
}
.pricing-task-condition label {
  flex: 1 1 7rem;
}
.pricing-lane {
  display: grid;
  gap: 0.4rem;
  padding: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
}
.pricing-lane > input {
  width: 100%;
  border: 1px solid var(--border-subtle);
  padding: 0.5rem;
  border-radius: 0.4rem;
  background: var(--surface-solid);
}
.pricing-convert,
.pricing-preview {
  padding: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  display: grid;
  gap: 0.5rem;
}
.pricing-simulation > * {
  margin-bottom: 0.75rem;
}
pre,
textarea {
  max-width: 100%;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-family: monospace;
  font-size: 0.8rem;
}
summary {
  cursor: pointer;
  padding: 0.5rem 0;
}
@media (max-width: 767px) {
  .pricing-editor-scroll {
    max-height: none;
    overflow: visible;
  }
}
</style>
