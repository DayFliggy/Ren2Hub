<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  modelPricingApi,
  PRICE_QUANTITIES,
  PRICE_UNITS,
  validatePriceVersion,
  type PriceMode,
  type PriceScopeKind,
  type PriceVersion,
  type PriceProvenance,
  type UnitPrice,
  type UsageSchema,
  type PricePreview,
  type TaskPriceSchema,
} from '@/api/modelPricing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import {
  automaticPricingApi,
  type ModelPriceOrigin,
} from '@/api/automaticPricing'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import UsageFactsForm from './UsageFactsForm.vue'
import UsageSchemaReview from './UsageSchemaReview.vue'
import {
  divideDecimal,
  formatDecimal,
  formatDecimalUSD,
  isDecimal,
  multiplyDecimal,
} from '@/utils/decimal'

const props = withDefaults(
  defineProps<{
    open?: boolean
    modelName: string
    seedDraft?: PriceVersion
  }>(),
  { open: true, seedDraft: undefined }
)
const { t } = useI18n()
const initial = ref<PriceVersion | null>(null)
const origin = ref<ModelPriceOrigin | null>(null)
const originError = ref('')
const originRequest = useLatestRequest()
const restoring = ref(false)
const restoreConfirm = ref(false)
let disposed = false
const mode = ref<PriceMode>('unit_price')
const prices = ref<UnitPrice[]>([])
const availablePrices = ref<PriceVersion[]>([])
const scopeKind = ref<PriceScopeKind>('global_model')
const channelId = ref(0)
const flatFee = ref('')
const provenanceKind = ref<'evidence' | 'retail'>('retail')
const retailReason = ref('')
const evidenceFields = ref('[]')
const meteringMethod = ref('')
const reservationQuantity = ref('')
const meteringOptions: Record<string, NonNullable<PriceVersion['metering']>> = {
  unicode_codepoints: {
    request_kind: 'speech',
    quantity: 'characters',
    source: 'request_fixed',
    method: 'unicode_codepoints',
  },
  provider_usage: {
    request_kind: 'speech',
    quantity: 'characters',
    source: 'completion_fact',
    method: 'provider_usage',
  },
  provider_duration: {
    request_kind: 'transcription',
    quantity: 'seconds',
    source: 'completion_fact',
    method: 'provider_duration',
  },
}
const effectiveTo = ref('')
const structuredPrices = ref(false)
const pricesJson = ref('[]')
const expression = ref('')
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const originalName = ref('')
const seedMessage = ref('')
const usageSchema = ref<UsageSchema | undefined>()
const usageScope = ref<PriceVersion['usage_scope']>()
const currentSchema = ref<TaskPriceSchema | null>(null)
const schemaError = ref('')
const selectedPlugin = ref('')
const schemaLoading = ref(false)
let schemaRequest: AbortController | null = null
let schemaTimer: ReturnType<typeof setTimeout> | undefined
async function loadTaskSchema() {
  clearTimeout(schemaTimer)
  schemaRequest?.abort()
  if (!props.modelName.trim()) {
    currentSchema.value = null
    schemaError.value = ''
    schemaLoading.value = false
    return
  }
  const request = new AbortController()
  schemaRequest = request
  schemaLoading.value = true
  schemaError.value = ''
  currentSchema.value = null
  try {
    const result = await modelPricingApi.taskSchema(
      props.modelName,
      selectedPlugin.value || undefined,
      request.signal
    )
    if (!request.signal.aborted) currentSchema.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      schemaError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!request.signal.aborted) schemaLoading.value = false
  }
}
function adoptCurrentSchema() {
  if (!currentSchema.value?.usage_schema) return
  usageSchema.value = JSON.parse(
    JSON.stringify(currentSchema.value.usage_schema)
  ) as UsageSchema
}
const sourceDraft = ref<PriceVersion | null>(null)
const candidate = ref<PriceVersion | null>(null)
const candidateOriginal = ref<PriceVersion | null>(null)
const conditionField = ref('')
const conditionValues = ref<Record<string, unknown>>({})
const conditionMultiplier = ref('1')
const conditionOperator = ref('==')
const localCurrency = ref('CNY')
const exchangeRate = ref('')
const localAmounts = ref<Record<number, string>>({})
const timeZone = ref('Asia/Shanghai')
const timeStart = ref(0)
const timeEnd = ref(6)
const timeMultiplier = ref('0.8')
const previewInput = ref(
  '{"input_tokens":1000,"output_tokens":100,"context_tokens":1000,"input_includes_cache":true,"complete":true}'
)
const previewAttributes = ref('{}')
const previewFacts = ref('{}')
const typedPreviewFacts = computed({
  get: () => parseObject(previewFacts.value),
  set: (value: Record<string, unknown>) => {
    previewFacts.value = JSON.stringify(value)
  },
})
const typedActualFacts = computed({
  get: () => {
    const facts = parseObject(previewInput.value).actual_facts
    return facts && typeof facts === 'object' && !Array.isArray(facts)
      ? (facts as Record<string, unknown>)
      : {}
  },
  set: (value: Record<string, unknown>) => {
    previewInput.value = JSON.stringify({
      ...objectInput(previewInput.value),
      actual_facts: value,
      complete: true,
    })
  },
})
const previewJsonValid = computed(() => {
  try {
    objectInput(previewInput.value)
    objectInput(previewFacts.value)
    return true
  } catch {
    return false
  }
})
function parseObject(value: string): Record<string, unknown> {
  try {
    return objectInput(value)
  } catch {
    return {}
  }
}
const previewRatio = ref('1')
const previewTime = ref(new Date().toISOString())
const preview = ref<PricePreview | null>(null)
const previewLoading = ref(false)
const previewError = ref('')
const previewEpoch = ref(0)
const quantities = computed(() =>
  usageSchema.value
    ? [...new Set(['calls', ...Object.keys(usageSchema.value.fields)])]
    : PRICE_QUANTITIES.filter(
        (quantity) =>
          quantity !== 'characters' ||
          meteringOptions[meteringMethod.value]?.quantity === 'characters'
      )
)
const units = computed(() =>
  usageSchema.value
    ? [
        ...new Set([
          'per_call',
          ...Object.values(usageSchema.value.fields)
            .filter((field) => field.type === 'number')
            .map((field) => `per_${field.unit}`),
        ]),
      ]
    : [...PRICE_UNITS]
)
const canRemoveSchemaForFixedCall = computed(
  () =>
    Boolean(usageSchema.value) &&
    !structuredPrices.value &&
    mode.value === 'unit_price' &&
    prices.value.length === 1 &&
    prices.value[0].quantity === 'calls' &&
    prices.value[0].unit === 'per_call' &&
    isDecimal(prices.value[0].price)
)
let request: AbortController | null = null
const snapshot = () =>
  JSON.stringify([
    mode.value,
    prices.value,
    expression.value,
    usageSchema.value,
    usageScope.value,
    scopeKind.value,
    channelId.value,
    flatFee.value,
    provenanceKind.value,
    retailReason.value,
    evidenceFields.value,
    meteringMethod.value,
    reservationQuantity.value,
    effectiveTo.value,
    structuredPrices.value ? pricesJson.value : null,
  ])
const initialForm = ref('')
const dirty = computed(() => initialForm.value !== snapshot())

function matchesScope(price: PriceVersion, name: string): boolean {
  return (
    price.model === name &&
    (price.scope_kind ?? 'global_model') === scopeKind.value &&
    (price.channel_id ?? 0) ===
      (scopeKind.value === 'channel_model' ? channelId.value : 0)
  )
}
function selectScope() {
  if (scopeKind.value === 'global_model') channelId.value = 0
  initial.value =
    availablePrices.value.find((price) =>
      matchesScope(price, originalName.value)
    ) ?? null
  applyDraft(
    initial.value ?? {
      id: 0,
      model: props.modelName,
      mode: 'unit_price',
      scope_kind: scopeKind.value,
      channel_id: channelId.value,
      prices: [
        { quantity: 'p', unit: 'per_million_tokens', price: '' },
        { quantity: 'c', unit: 'per_million_tokens', price: '' },
      ],
      created_at: 0,
      hash: '',
    }
  )
  sourceDraft.value = null
  candidate.value = null
  initialForm.value = snapshot()
  void loadOrigin()
}
function enableStructuredPrices() {
  pricesJson.value = JSON.stringify(prices.value, null, 2)
  structuredPrices.value = true
}
function currentPrices(): UnitPrice[] {
  if (!structuredPrices.value) return structuredClone(toRaw(prices.value))
  const parsed: unknown = JSON.parse(pricesJson.value)
  if (!Array.isArray(parsed)) throw new Error(t('billing.invalidPrice'))
  return parsed as UnitPrice[]
}

async function loadOrigin() {
  origin.value = null
  originError.value = ''
  const result = await originRequest.run(automaticPricingApi.status)
  if (result.stale) return
  if (!result.ok) {
    originError.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  origin.value =
    result.value.origins.find(
      (row) =>
        row.model === originalName.value &&
        (row.channel_id ?? 0) === channelId.value &&
        row.version_id === initial.value?.id
    ) ?? null
}
async function load() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    const data = await modelPricingApi.load(controller.signal)
    if (controller.signal.aborted) return
    originalName.value = props.modelName
    availablePrices.value = data
    selectScope()
    loaded.value = true
    candidate.value = null
    selectedPlugin.value = ''
    void loadTaskSchema()
    if (props.seedDraft?.model === props.modelName) applyDraft(props.seedDraft)
  } catch {
    if (!controller.signal.aborted) error.value = t('billing.priceUnavailable')
  } finally {
    if (request === controller) loading.value = false
  }
}
function applyDraft(price: PriceVersion) {
  sourceDraft.value = structuredClone(toRaw(price))
  mode.value = price.mode
  scopeKind.value = price.scope_kind ?? 'global_model'
  channelId.value = price.channel_id ?? 0
  flatFee.value = price.flat_fee ?? ''
  provenanceKind.value =
    price.provenance?.kind === 'evidence' ? 'evidence' : 'retail'
  retailReason.value =
    price.provenance?.kind === 'retail'
      ? (price.provenance.retail_reason ?? '')
      : ''
  evidenceFields.value = JSON.stringify(price.provenance?.fields ?? [], null, 2)
  meteringMethod.value = price.metering?.method ?? ''
  reservationQuantity.value = price.metering?.reservation_quantity ?? ''
  effectiveTo.value = price.effective_to ?? ''
  prices.value = structuredClone(toRaw(price.prices))
  pricesJson.value = JSON.stringify(price.prices, null, 2)
  structuredPrices.value = price.prices.some((line) => line.tier_mode)
  expression.value = price.expression ?? ''
  usageScope.value = price.usage_scope
  usageSchema.value = price.usage_schema
    ? structuredClone(toRaw(price.usage_schema))
    : undefined
  localAmounts.value = {}
}
watch(usageScope, (value) => {
  if (value === 'embedding')
    previewInput.value = JSON.stringify({
      ...parseObject(previewInput.value),
      output_tokens: 0,
    })
})
watch(
  () => props.seedDraft,
  (price) => {
    if (loaded.value && price?.model === props.modelName) applyDraft(price)
  }
)
watch(
  () => props.modelName,
  () => {
    clearTimeout(schemaTimer)
    schemaRequest?.abort()
    currentSchema.value = null
    selectedPlugin.value = ''
    schemaLoading.value = false
    if (loaded.value && props.open)
      schemaTimer = setTimeout(loadTaskSchema, 300)
  }
)
function applyLocalAmount(index: number) {
  try {
    const line = prices.value[index]
    if (!line) return
    line.price = divideDecimal(
      localAmounts.value[index] ?? '',
      exchangeRate.value
    )
    error.value = ''
  } catch {
    error.value = t('billing.currencyInvalid')
  }
}
function localDisplay(value: string): string {
  try {
    return formatDecimal(multiplyDecimal(value, exchangeRate.value))
  } catch {
    return '—'
  }
}
function convertUnitDraft() {
  try {
    const terms: string[] = []
    const extras: UnitPrice[] = []
    for (const line of currentPrices()) {
      if (line.tier_mode)
        throw new Error('Tiered prices require structured pricing')
      if (!isDecimal(line.price)) throw new Error('price')
      if (usageSchema.value)
        terms.push(`u(${JSON.stringify(line.quantity)}) * ${line.price}`)
      else if (line.unit === 'per_million_tokens')
        terms.push(`${line.quantity} * ${line.price}`)
      else if (line.quantity === 'calls') terms.push(`fixed(${line.price})`)
      else if (line.quantity === 'images')
        terms.push(`fixed(${line.price}) * image_count`)
      else extras.push({ ...line })
    }
    if (!terms.length) throw new Error('no expression dimensions')
    candidateOriginal.value = JSON.parse(
      JSON.stringify(draft(props.modelName))
    ) as PriceVersion
    candidate.value = {
      ...candidateOriginal.value,
      mode: 'expression',
      prices: extras,
      expression: `${usageSchema.value ? 'v2' : 'v1'}:${terms.join(' + ')}`,
      result_unit: usageSchema.value ? 'usd' : 'micro_usd',
    }
    validatePriceVersion(candidate.value)
    error.value = ''
  } catch {
    error.value = t('billing.invalidPrice')
  }
}
function applyCandidate() {
  if (!candidate.value) return
  const previous = candidateOriginal.value
  applyDraft(JSON.parse(JSON.stringify(candidate.value)) as PriceVersion)
  sourceDraft.value = previous
  candidate.value = null
}
function addSchemaCondition() {
  const field = usageSchema.value?.fields[conditionField.value]
  const value = conditionValues.value[conditionField.value]
  if (
    !field ||
    value === undefined ||
    field.source === 'estimate_only' ||
    !isDecimal(conditionMultiplier.value)
  ) {
    error.value = t('billing.invalidPrice')
    return
  }
  if (field.type === 'number' && !isDecimal(String(value))) {
    error.value = t('billing.invalidPrice')
    return
  }
  if (field.enum?.length && !field.enum.includes(String(value))) {
    error.value = t('billing.factInvalidEnum')
    return
  }
  const literal =
    field.type === 'number' ? String(value) : JSON.stringify(value)
  const operator = field.type === 'number' ? conditionOperator.value : '=='
  const current = expression.value.replace(/^v2:/, '').trim()
  if (!current || !expression.value.startsWith('v2:')) {
    error.value = t('billing.invalidPrice')
    return
  }
  expression.value = `v2:(${current}) * (u(${JSON.stringify(conditionField.value)}) ${operator} ${literal} ? ${conditionMultiplier.value} : 1)`
  error.value = ''
}
function applyTimeRule() {
  try {
    new Intl.DateTimeFormat('en', { timeZone: timeZone.value }).format()
    if (
      !expression.value.trim() ||
      !isDecimal(timeMultiplier.value) ||
      !Number.isInteger(timeStart.value) ||
      !Number.isInteger(timeEnd.value) ||
      timeStart.value < 0 ||
      timeStart.value > 23 ||
      timeEnd.value < 0 ||
      timeEnd.value > 24 ||
      timeStart.value === timeEnd.value
    )
      throw new Error('time')
    const hour = `hour(${JSON.stringify(timeZone.value)})`
    const condition = `${hour} >= ${timeStart.value} ${timeStart.value < timeEnd.value ? '&&' : '||'} ${hour} < ${timeEnd.value}`
    const match = expression.value.match(/^(v[12]:)?([\s\S]*)$/)!
    expression.value = `${match[1] ?? 'v1:'}(${match[2]}) * (${condition} ? ${timeMultiplier.value} : 1)`
    error.value = ''
  } catch {
    error.value = t('billing.timeRuleInvalid')
  }
}
const sourceRows = computed(() => {
  if (!sourceDraft.value) return []
  const names = new Set([
    ...prices.value.map((line) => line.quantity),
    ...sourceDraft.value.prices.map((line) => line.quantity),
  ])
  return Array.from(names).map((quantity) => {
    const current = prices.value.find((line) => line.quantity === quantity)
    const source = sourceDraft.value?.prices.find(
      (line) => line.quantity === quantity
    )
    return {
      quantity,
      current: current ? `${current.price} / ${current.unit}` : '—',
      source: source ? `${source.price} / ${source.unit}` : '—',
    }
  })
})
function draft(name: string): PriceVersion {
  return {
    id: 0,
    model: name,
    ...(meteringMethod.value
      ? {
          metering: {
            ...meteringOptions[meteringMethod.value]!,
            ...(meteringMethod.value !== 'unicode_codepoints'
              ? { reservation_quantity: reservationQuantity.value }
              : {}),
          },
        }
      : {}),
    provenance:
      provenanceKind.value === 'retail'
        ? { kind: 'retail', retail_reason: retailReason.value.trim() }
        : {
            kind: 'evidence',
            fields: JSON.parse(
              evidenceFields.value
            ) as PriceProvenance['fields'],
          },
    scope_kind: scopeKind.value,
    channel_id: scopeKind.value === 'channel_model' ? channelId.value : 0,
    mode: mode.value,
    prices: mode.value === 'free' ? [] : currentPrices(),
    ...(mode.value !== 'free' && flatFee.value.trim()
      ? { flat_fee: flatFee.value.trim() }
      : {}),

    ...(effectiveTo.value.trim()
      ? { effective_to: effectiveTo.value.trim() }
      : {}),
    expression:
      mode.value === 'expression' ? expression.value.trim() : undefined,
    created_at: 0,
    hash: '',
    contract_version: 3,
    calculation_version: 'billing-v3',
    result_unit:
      mode.value === 'expression' && !expression.value.trim().startsWith('v2:')
        ? 'micro_usd'
        : 'usd',
    usage_schema: usageSchema.value,
    ...(usageScope.value ? { usage_scope: usageScope.value } : {}),
  }
}
async function validate(name = props.modelName) {
  if (!loaded.value || loading.value || restoring.value)
    throw new Error(t('billing.priceUnavailable'))
  try {
    validatePriceVersion(draft(name))
  } catch {
    throw new Error(t('billing.invalidPrice'))
  }
  if (!name.trim()) throw new Error(t('billing.invalidPrice'))
  if (!dirty.value && name === originalName.value) return
  const provenance = draft(name).provenance!
  if (
    provenance.kind === 'retail'
      ? !provenance.retail_reason
      : !provenance.fields?.length ||
        provenance.fields.some((field) =>
          [
            'target',
            'source_path',
            'raw_value',
            'source_unit',
            'conversion',
          ].some(
            (key) =>
              typeof field[key as keyof typeof field] !== 'string' ||
              !field[key as keyof typeof field].trim()
          )
        )
  )
    throw new Error(t('billing.priceEvidenceRequired'))
  if (schemaLoading.value || schemaError.value || !currentSchema.value)
    throw new Error(schemaError.value || t('billing.schemaUnavailable'))
  if (currentSchema.value.plugins.length > 1 && !selectedPlugin.value)
    throw new Error(t('billing.schemaChoosePlugin'))
  const latest = await modelPricingApi.load()
  const current = latest.find((row) => matchesScope(row, name))
  const expected = name === originalName.value ? (initial.value?.id ?? 0) : 0
  if ((current?.id ?? 0) !== expected)
    throw new Error(t('billing.priceConflict'))
}
async function save(name: string) {
  await validate(name)
  if (!dirty.value && name === originalName.value && initial.value) return
  const submittedForm = snapshot()
  initial.value = await modelPricingApi.publish(
    draft(name),
    name === originalName.value ? (initial.value?.id ?? 0) : 0,
    selectedPlugin.value || currentSchema.value?.plugin_key || undefined
  )
  originalName.value = name
  availablePrices.value = availablePrices.value.filter(
    (price) => !matchesScope(price, name)
  )
  availablePrices.value.push(initial.value)
  initialForm.value = submittedForm
  originRequest.cancel()
  origin.value = {
    model: name,
    channel_id: initial.value.channel_id ?? 0,
    version_id: initial.value.id,
    origin: 'manual',
    source_model: '',
    source_hash: '',
  }
}
async function restoreAutomatic() {
  if (
    restoring.value ||
    !initial.value ||
    props.modelName !== originalName.value
  )
    return
  const model = originalName.value
  restoring.value = true
  error.value = ''
  try {
    const price = await automaticPricingApi.restore(model, initial.value.id)
    if (disposed || !props.open || props.modelName !== model) return
    initial.value = price
    availablePrices.value = availablePrices.value.filter(
      (row) => !matchesScope(row, model)
    )
    availablePrices.value.push(price)
    applyDraft(price)
    sourceDraft.value = null
    candidate.value = null
    seedMessage.value = ''
    initialForm.value = snapshot()
    restoreConfirm.value = false
    await loadOrigin()
  } catch (cause) {
    if (!disposed) {
      restoreConfirm.value = false
      error.value =
        cause instanceof ApiError && cause.status === 409
          ? t('billing.priceConflict')
          : cause instanceof Error
            ? cause.message
            : String(cause)
    }
  } finally {
    if (!disposed) restoring.value = false
  }
}
async function importSeed() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  seedMessage.value = ''
  try {
    const seed = await modelPricingApi.seed(controller.signal)
    if (controller.signal.aborted) return
    const price = seed.prices.find((row) => row.model === props.modelName)
    if (!price) {
      seedMessage.value = t('billing.seedUnconfigured')
      return
    }
    candidateOriginal.value = JSON.parse(
      JSON.stringify(draft(props.modelName))
    ) as PriceVersion
    candidate.value = {
      ...structuredClone(price),
      scope_kind: scopeKind.value,
      channel_id: channelId.value,
    }
    seedMessage.value = t('billing.seedLoaded', { source: seed.source })
  } catch {
    if (!controller.signal.aborted) error.value = t('billing.priceUnavailable')
  } finally {
    if (request === controller) loading.value = false
  }
}
watch(mode, (value) => {
  if (value === 'expression' && !usageSchema.value)
    prices.value = prices.value.filter(
      (price) =>
        ['calls', 'images', 'seconds'].includes(price.quantity) ||
        price.quantity.startsWith('tool:')
    )
})
function objectInput(text: string): Record<string, never> {
  const parsed: unknown = JSON.parse(text)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
    throw new Error('Object required')
  return parsed as Record<string, never>
}
async function runPreview() {
  const epoch = ++previewEpoch.value
  previewLoading.value = true
  preview.value = null
  previewError.value = ''
  try {
    const result = await modelPricingApi.preview({
      price: draft(props.modelName),
      usage: objectInput(previewInput.value),
      attributes: objectInput(previewAttributes.value),
      request_facts: objectInput(previewFacts.value),
      channel_ratio:
        scopeKind.value === 'channel_model' ? '1' : previewRatio.value,
      frozen_at: previewTime.value,
    })
    if (epoch === previewEpoch.value) preview.value = result
  } catch (cause) {
    if (epoch === previewEpoch.value)
      previewError.value =
        cause instanceof Error ? cause.message : t('billing.previewFailed')
  } finally {
    if (epoch === previewEpoch.value) previewLoading.value = false
  }
}
watch(
  [
    () => props.modelName,
    () => props.open,
    snapshot,
    previewInput,
    previewAttributes,
    previewFacts,
    previewRatio,
    previewTime,
  ],
  () => {
    previewEpoch.value++
    preview.value = null
    previewError.value = ''
    previewLoading.value = false
  }
)
watch(
  () => props.open,
  (open) => {
    if (open) void load()
    else {
      request?.abort()
      originRequest.cancel()
      restoreConfirm.value = false
      schemaRequest?.abort()
      clearTimeout(schemaTimer)
    }
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  disposed = true
  clearTimeout(schemaTimer)
  request?.abort()
  schemaRequest?.abort()
  previewEpoch.value++
})
defineExpose({ validate, save })
</script>

<template>
  <section
    class="space-y-4 border-t border-[var(--border-subtle)] pt-4"
    :aria-busy="loading"
  >
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-sm font-semibold">{{ t('billing.priceTitle') }}</h3>
      <span v-if="initial" class="font-mono text-xs text-[var(--text-tertiary)]"
        >v{{ initial.id }}</span
      >
    </div>
    <p class="text-xs leading-relaxed text-[var(--text-tertiary)]">
      {{ t('billing.priceVersionHint') }}
    </p>
    <p
      v-if="usageScope === 'embedding'"
      data-price-usage-scope
      class="text-xs leading-relaxed text-[var(--text-secondary)]"
    >
      {{ t('billing.embeddingScope') }}
    </p>
    <div
      v-if="initial && props.modelName === originalName"
      class="space-y-1 text-xs text-[var(--text-secondary)]"
      data-price-origin
    >
      <p>
        {{ t('billing.automatic.currentOrigin') }}:
        {{
          origin
            ? t(`billing.automatic.origins.${origin.origin}`)
            : t('billing.automatic.originUnavailable')
        }}
      </p>
      <p v-if="origin?.source_model">
        {{ t('billing.automatic.sourceModel') }}: {{ origin.source_model }}
      </p>
      <p v-if="origin?.source_hash" class="break-all font-mono">
        SHA: {{ origin.source_hash }}
      </p>
      <p v-if="origin?.origin === 'manual'">
        {{ t('billing.automatic.manualHint') }}
      </p>
    </div>
    <p
      v-if="originError"
      role="alert"
      class="text-xs text-[var(--status-danger-text)]"
    >
      {{ originError }}
    </p>
    <p
      v-if="error"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <div class="flex flex-wrap gap-2">
      <ConsoleButton
        variant="secondary"
        :disabled="loading || restoring"
        @click="load"
        >{{ t('billing.refresh') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading || !loaded || restoring"
        @click="importSeed"
        >{{ t('billing.importSeed') }}</ConsoleButton
      >
      <ConsoleButton
        v-if="
          initial &&
          scopeKind === 'global_model' &&
          props.modelName === originalName &&
          origin?.origin === 'manual'
        "
        variant="secondary"
        :disabled="loading || !loaded"
        :loading="restoring"
        data-restore-automatic
        @click="restoreConfirm = true"
        >{{ t('billing.automatic.restore') }}</ConsoleButton
      >
    </div>
    <p
      v-if="seedMessage"
      role="status"
      class="text-xs text-[var(--text-secondary)]"
    >
      {{ seedMessage }}
    </p>
    <fieldset :disabled="loading || !loaded || restoring" class="space-y-4">
      <label class="block text-sm"
        >{{ t('billing.priceMode')
        }}<select
          v-model="mode"
          class="mt-2 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 focus-ring"
        >
          <option
            v-for="value in ['unit_price', 'expression', 'free']"
            :key="value"
            :value="value"
          >
            {{ t(`billing.modes.${value}`) }}
          </option>
        </select></label
      >
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-sm"
          >{{ t('billing.priceScope') }}
          <select
            v-model="scopeKind"
            data-price-scope
            class="mt-2 w-full rounded border p-2 bg-[var(--surface-muted)] focus-ring"
            @change="selectScope"
          >
            <option value="global_model">{{ t('billing.scopeGlobal') }}</option>
            <option value="channel_model">
              {{ t('billing.scopeChannel') }}
            </option>
          </select>
        </label>
        <label v-if="scopeKind === 'channel_model'" class="text-sm"
          >{{ t('billing.priceChannel') }}
          <input
            v-model.number="channelId"
            data-price-channel
            type="number"
            min="1"
            step="1"
            class="mt-2 w-full rounded border p-2 bg-[var(--surface-muted)] focus-ring"
            @change="selectScope"
          />
          <span class="block text-xs text-[var(--text-tertiary)]">{{
            availablePrices
              .filter(
                (price) =>
                  price.model === originalName &&
                  price.scope_kind === 'channel_model'
              )
              .map((price) => price.channel_id)
              .join(', ')
          }}</span>
        </label>
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <p class="text-sm text-[var(--text-secondary)]" data-price-immediate>
          {{ t('billing.immediatePublication') }}
        </p>
        <label class="text-sm"
          >{{ t('billing.effectiveTo')
          }}<input
            v-model="effectiveTo"
            data-price-effective-to
            placeholder="2027-01-01T00:00:00Z"
            class="mt-2 w-full rounded border p-2 bg-[var(--surface-muted)] focus-ring"
        /></label>
      </div>
      <section
        class="space-y-3 rounded border border-[var(--border-subtle)] p-3"
        data-price-evidence
      >
        <label class="block text-sm"
          >{{ t('billing.metering')
          }}<select
            v-model="meteringMethod"
            data-price-metering
            class="mt-2 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
          >
            <option value="">{{ t('billing.meteringMethods.none') }}</option>
            <option
              v-for="(_, method) in meteringOptions"
              :key="method"
              :value="method"
            >
              {{ t(`billing.meteringMethods.${method}`) }}
            </option>
          </select></label
        >
        <label
          v-if="meteringMethod && meteringMethod !== 'unicode_codepoints'"
          class="block text-sm"
          >{{ t('billing.reservationQuantity')
          }}<input
            v-model="reservationQuantity"
            data-price-reservation-quantity
            inputmode="decimal"
            class="mt-2 w-full rounded border bg-[var(--surface-muted)] p-2 font-mono focus-ring"
        /></label>
        <label class="block text-sm"
          >{{ t('billing.priceBasis')
          }}<select
            v-model="provenanceKind"
            data-price-provenance
            class="mt-2 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
          >
            <option value="retail">{{ t('billing.retailPolicy') }}</option>
            <option value="evidence">
              {{ t('billing.verifiedEvidence') }}
            </option>
          </select></label
        >
        <label v-if="provenanceKind === 'retail'" class="block text-sm"
          >{{ t('billing.retailReason')
          }}<textarea
            v-model="retailReason"
            data-price-retail-reason
            rows="3"
            class="mt-2 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
          />
        </label>
        <label v-else class="block text-sm"
          >{{ t('billing.sourceFields')
          }}<textarea
            v-model="evidenceFields"
            data-price-evidence-fields
            rows="8"
            spellcheck="false"
            class="mt-2 w-full rounded border bg-[var(--surface-muted)] p-2 font-mono text-xs focus-ring"
          /><span class="mt-1 block text-xs text-[var(--text-tertiary)]">{{
            t('billing.sourceFieldsHint')
          }}</span></label
        >
        <details v-if="sourceDraft?.provenance">
          <summary class="cursor-pointer text-sm focus-ring">
            {{ t('billing.originalPriceEvidence') }}
          </summary>
          <pre
            class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs"
            >{{ JSON.stringify(sourceDraft.provenance, null, 2) }}</pre>
        </details>
      </section>
      <template v-if="mode !== 'free'">
        <label class="block text-sm"
          >{{ t('billing.flatFee')
          }}<input
            v-model="flatFee"
            data-price-flat-fee
            class="mt-2 w-full rounded border p-2 bg-[var(--surface-muted)] focus-ring"
        /></label>
        <label v-if="structuredPrices" class="block text-sm"
          >{{ t('billing.structuredPrices') }}
          <textarea
            v-model="pricesJson"
            data-price-structure
            rows="12"
            spellcheck="false"
            class="mt-2 w-full rounded border p-3 font-mono text-xs bg-[var(--surface-muted)] focus-ring"
          />
          <span class="mt-2 block text-xs text-[var(--text-tertiary)]">{{
            t('billing.structuredPricesHint')
          }}</span>
        </label>
        <template v-else>
          <details
            class="space-y-2 rounded border border-[var(--border-subtle)] p-3 text-xs"
          >
            <summary>{{ t('billing.currencyHelper') }}</summary>
            <p>{{ t('billing.currencyHint') }}</p>
            <label
              >{{ t('billing.localCurrency')
              }}<input
                v-model="localCurrency"
                maxlength="8"
                class="m-2 rounded border p-1 bg-[var(--surface-muted)]"
            /></label>
            <label
              >{{ t('billing.exchangeRate')
              }}<input
                v-model="exchangeRate"
                data-exchange-rate
                class="m-2 rounded border p-1 bg-[var(--surface-muted)]"
            /></label>
          </details>
          <div
            v-for="(line, index) in prices"
            :key="index"
            class="grid grid-cols-1 gap-2 rounded-lg bg-[var(--surface-muted)] p-3 sm:grid-cols-[1fr_1fr_1fr_auto]"
          >
            <label class="text-xs"
              >{{ t('billing.quantity')
              }}<input
                v-model="line.quantity"
                :list="`billing-quantities-${index}`"
                class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 font-mono focus-ring"
              /><datalist :id="`billing-quantities-${index}`">
                <option
                  v-for="quantity in quantities"
                  :key="quantity"
                  :value="quantity"
                >
                  {{
                    usageSchema ? quantity : t(`billing.quantities.${quantity}`)
                  }}
                </option>
              </datalist></label
            >
            <label class="text-xs"
              >{{ t('billing.unit')
              }}<select
                v-model="line.unit"
                class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 focus-ring"
              >
                <option v-for="unit in units" :key="unit" :value="unit">
                  {{ usageSchema ? unit : t(`billing.units.${unit}`) }}
                </option>
              </select></label
            >
            <label class="text-xs"
              >{{ t('billing.unitPrice')
              }}<input
                v-model="line.price"
                inputmode="decimal"
                class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 font-mono focus-ring"
              />
              <template v-if="isDecimal(exchangeRate) && exchangeRate !== '0'">
                <span class="block py-1"
                  >≈ {{ localDisplay(line.price) }} {{ localCurrency }}</span
                >
                <input
                  v-model="localAmounts[index]"
                  :aria-label="t('billing.localAmount')"
                  class="w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
                />
                <button
                  type="button"
                  class="mt-1 underline"
                  @click="applyLocalAmount(index)"
                >
                  {{ t('billing.applyCurrency') }}
                </button>
              </template>
            </label>
            <button
              type="button"
              class="self-end rounded p-2 text-sm focus-ring"
              :aria-label="t('billing.removePrice')"
              @click="prices.splice(index, 1)"
            >
              ×
            </button>
          </div>
          <ConsoleButton
            variant="secondary"
            @click="
              prices.push({ quantity: 'calls', unit: 'per_call', price: '' })
            "
            >{{ t('billing.addPrice') }}</ConsoleButton
          >
          <ConsoleButton
            variant="secondary"
            data-enable-structured-prices
            @click="enableStructuredPrices"
            >{{ t('billing.structuredPrices') }}</ConsoleButton
          >
        </template>
      </template>
      <label v-if="mode === 'expression'" class="block text-sm"
        >{{ t('billing.expression')
        }}<textarea
          v-model="expression"
          rows="5"
          spellcheck="false"
          class="mt-2 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs focus-ring"
        /><span class="mt-2 block text-xs text-[var(--text-tertiary)]">{{
          t('billing.expressionHint')
        }}</span></label
      >
      <ConsoleButton
        v-if="mode === 'unit_price'"
        variant="secondary"
        @click="convertUnitDraft"
        >{{ t('billing.convertExpression') }}</ConsoleButton
      >
      <details
        v-if="mode === 'expression'"
        class="space-y-2 rounded border border-[var(--border-subtle)] p-3 text-xs"
      >
        <summary>{{ t('billing.timeRule') }}</summary>
        <div class="grid grid-cols-2 gap-2">
          <label
            >{{ t('billing.timeZone')
            }}<input
              v-model="timeZone"
              class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2"
          /></label>
          <label
            >{{ t('billing.timeMultiplier')
            }}<input
              v-model="timeMultiplier"
              class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2"
          /></label>
          <label
            >{{ t('billing.timeStart')
            }}<input
              v-model.number="timeStart"
              type="number"
              min="0"
              max="23"
              class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2"
          /></label>
          <label
            >{{ t('billing.timeEnd')
            }}<input
              v-model.number="timeEnd"
              type="number"
              min="0"
              max="24"
              class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2"
          /></label>
        </div>
        <ConsoleButton variant="secondary" @click="applyTimeRule">{{
          t('billing.applyTimeRule')
        }}</ConsoleButton>
      </details>
      <section
        v-if="candidate"
        data-price-conversion-preview
        class="space-y-3 rounded-lg border border-[var(--border-subtle)] p-3 text-xs"
      >
        <h4 class="font-semibold">{{ t('billing.conversionReview') }}</h4>
        <p>{{ t('billing.conversionReviewHint') }}</p>
        <dl class="space-y-2">
          <dt>{{ t('billing.currentDraft') }}</dt>
          <dd class="whitespace-pre-wrap break-all font-mono">
            {{
              candidateOriginal?.expression ||
              candidateOriginal?.prices
                .map((line) => `${line.quantity}: ${line.price} / ${line.unit}`)
                .join('\n')
            }}
          </dd>
          <dt>{{ t('billing.convertedDraft') }}</dt>
          <dd class="whitespace-pre-wrap break-all font-mono">
            {{
              candidate.expression ||
              candidate.prices
                .map((line) => `${line.quantity}: ${line.price} / ${line.unit}`)
                .join('\n')
            }}
          </dd>
        </dl>
        <p v-if="candidate.usage_scope === 'embedding'">
          {{ t('billing.embeddingScope') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <ConsoleButton
            variant="secondary"
            data-apply-price-conversion
            @click="applyCandidate"
            >{{ t('billing.applyConversion') }}</ConsoleButton
          ><ConsoleButton variant="ghost" @click="candidate = null">{{
            t('common.cancel')
          }}</ConsoleButton>
        </div>
      </section>
      <details
        v-if="usageSchema && mode === 'expression'"
        class="space-y-3 rounded border border-[var(--border-subtle)] p-3 text-xs"
      >
        <summary>{{ t('billing.schemaCondition') }}</summary>
        <label class="block"
          >{{ t('billing.quantity')
          }}<select
            v-model="conditionField"
            data-condition-field
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 focus-ring"
          >
            <option value="">{{ t('billing.factMissing') }}</option>
            <option
              v-for="[name] in Object.entries(usageSchema.fields).filter(
                ([, field]) => field.source !== 'estimate_only'
              )"
              :key="name"
              :value="name"
            >
              {{ name }}
            </option>
          </select></label
        >
        <template v-if="conditionField && usageSchema.fields[conditionField]">
          <UsageFactsForm
            v-model="conditionValues"
            :schema="{
              ...usageSchema,
              fields: { [conditionField]: usageSchema.fields[conditionField]! },
            }"
            :sources="['request_fixed', 'completion_fact']"
          />
          <label
            v-if="usageSchema.fields[conditionField]?.type === 'number'"
            class="block"
            >{{ t('billing.conditionOperator')
            }}<select
              v-model="conditionOperator"
              class="ml-2 rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
            >
              <option
                v-for="operator in ['==', '!=', '>', '>=', '<', '<=']"
                :key="operator"
              >
                {{ operator }}
              </option>
            </select></label
          >
          <label class="block"
            >{{ t('billing.multiplier')
            }}<input
              v-model="conditionMultiplier"
              inputmode="decimal"
              class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
          /></label>
          <ConsoleButton
            variant="secondary"
            data-add-schema-condition
            @click="addSchemaCondition"
            >{{ t('billing.addSchemaCondition') }}</ConsoleButton
          >
        </template>
      </details>
      <details
        v-if="sourceDraft && !structuredPrices"
        class="space-y-2 text-xs"
      >
        <summary>{{ t('billing.sourceDifference') }}</summary>
        <table class="w-full table-fixed text-left">
          <thead>
            <tr>
              <th>{{ t('billing.quantity') }}</th>
              <th>{{ t('billing.currentDraft') }}</th>
              <th>{{ t('billing.sourceDraft') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in sourceRows"
              :key="row.quantity"
              :class="
                row.current !== row.source ? 'bg-[var(--accent-muted)]' : ''
              "
            >
              <td class="break-all">{{ row.quantity }}</td>
              <td class="break-all">{{ row.current }}</td>
              <td class="break-all">{{ row.source }}</td>
            </tr>
            <tr
              :class="
                expression !== (sourceDraft.expression ?? '')
                  ? 'bg-[var(--accent-muted)]'
                  : ''
              "
            >
              <td>{{ t('billing.expression') }}</td>
              <td class="whitespace-pre-wrap break-all">{{ expression }}</td>
              <td class="whitespace-pre-wrap break-all">
                {{ sourceDraft.expression }}
              </td>
            </tr>
          </tbody>
        </table>
      </details>
      <p v-if="mode === 'free'" class="text-sm text-[var(--text-secondary)]">
        {{ t('billing.freeHint') }}
      </p>
      <section
        v-if="
          schemaError ||
          currentSchema?.review_required ||
          currentSchema?.plugins.length ||
          currentSchema?.usage_schema
        "
        class="space-y-3 rounded border border-[var(--border-subtle)] p-3 text-xs"
        data-task-schema
      >
        <h4 class="font-semibold">{{ t('billing.currentPluginSchema') }}</h4>
        <p
          v-if="schemaError"
          role="alert"
          class="text-[var(--status-danger-text)]"
        >
          {{ schemaError }}
        </p>
        <ConsoleButton
          v-if="schemaError"
          variant="secondary"
          size="sm"
          @click="loadTaskSchema"
          >{{ t('common.retry') }}</ConsoleButton
        >
        <label
          v-if="currentSchema && currentSchema.plugins.length > 1"
          class="block"
          >{{ t('billing.schemaChoosePlugin')
          }}<select
            v-model="selectedPlugin"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
            @change="loadTaskSchema"
          >
            <option value="">{{ t('billing.factMissing') }}</option>
            <option
              v-for="plugin in currentSchema.plugins"
              :key="plugin.key"
              :value="plugin.key"
            >
              {{ plugin.name }} · {{ plugin.version }}
            </option>
          </select></label
        >
        <p v-if="currentSchema?.review_required">
          {{ t('billing.schemaReviewRequired') }}
        </p>
        <UsageSchemaReview
          v-if="
            currentSchema?.review_required &&
            currentSchema.plugin_key &&
            currentSchema.manifest_schema
          "
          :manifest="currentSchema.manifest_schema"
          :saved="usageSchema"
          @apply="usageSchema = $event"
        />
        <template v-if="currentSchema?.usage_schema"
          ><p>
            {{ currentSchema.plugin_key }} · {{ currentSchema.plugin_version }}
          </p>
          <dl class="space-y-1">
            <div
              v-for="(field, name) in currentSchema.usage_schema.fields"
              :key="name"
              class="break-all"
            >
              <dt class="font-mono">{{ name }}</dt>
              <dd>
                {{ field.type }} ·
                {{ field.unit || field.enum?.join(', ') || '—' }} ·
                {{ t(`billing.factSources.${field.source}`) }}
              </dd>
            </div>
          </dl>
          <p>{{ t('billing.schemaPreserveHint') }}</p>
          <ConsoleButton
            variant="secondary"
            size="sm"
            data-adopt-task-schema
            @click="adoptCurrentSchema"
            >{{ t('billing.adoptPluginSchema') }}</ConsoleButton
          ></template
        >
      </section>
      <details v-if="usageSchema" class="text-xs">
        <summary>{{ t('billing.usageSchema') }}</summary>
        <dl class="mt-2 grid grid-cols-2 gap-2">
          <template v-for="(field, name) in usageSchema.fields" :key="name">
            <dt class="font-mono">{{ name }}</dt>
            <dd>
              {{ field.type }} · {{ field.unit || field.enum?.join(', ') }} ·
              {{ field.source }}
            </dd>
          </template>
        </dl>
      </details>
      <div v-if="canRemoveSchemaForFixedCall" class="space-y-2 text-xs">
        <p>{{ t('billing.fixedCallWithoutSchemaHint') }}</p>
        <ConsoleButton
          size="sm"
          variant="secondary"
          data-fixed-call-without-schema
          @click="usageSchema = undefined"
          >{{ t('billing.fixedCallWithoutSchema') }}</ConsoleButton
        >
      </div>
      <details
        class="space-y-3 rounded-lg border border-[var(--border-subtle)] p-3"
      >
        <summary class="cursor-pointer text-sm">
          {{ t('billing.previewTitle') }}
        </summary>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('billing.previewHint') }}
        </p>
        <UsageFactsForm
          v-if="usageSchema && previewJsonValid"
          v-model="typedActualFacts"
          :schema="usageSchema"
          :sources="['completion_fact']"
        />
        <UsageFactsForm
          v-if="usageSchema && previewJsonValid"
          v-model="typedPreviewFacts"
          :schema="usageSchema"
          :sources="['request_fixed']"
        />
        <label class="block text-xs"
          >{{ t('billing.usage')
          }}<textarea
            v-model="previewInput"
            data-price-preview-usage
            rows="4"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 font-mono focus-ring"
          />
        </label>
        <label class="block text-xs"
          >{{ t('billing.previewAttributes')
          }}<textarea
            v-model="previewAttributes"
            rows="2"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 font-mono focus-ring"
          />
        </label>
        <label v-if="usageSchema" class="block text-xs"
          >{{ t('billing.previewFacts')
          }}<textarea
            v-model="previewFacts"
            rows="2"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 font-mono focus-ring"
          />
        </label>
        <label v-if="scopeKind === 'global_model'" class="block text-xs"
          >{{ t('billing.previewRatio')
          }}<input
            v-model="previewRatio"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 focus-ring"
        /></label>
        <label class="block text-xs"
          >{{ t('billing.previewTime')
          }}<input
            v-model="previewTime"
            class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 font-mono focus-ring"
        /></label>
        <ConsoleButton
          variant="secondary"
          :disabled="previewLoading"
          data-price-preview
          @click="runPreview"
          >{{ t('billing.previewAction') }}</ConsoleButton
        >
        <p
          v-if="previewError"
          role="alert"
          class="text-sm text-[var(--status-danger-text)]"
        >
          {{ previewError }}
        </p>
        <div
          v-if="preview"
          data-price-preview-result
          role="status"
          class="space-y-2 text-sm"
        >
          <p>
            {{ t('billing.finalCharge') }}:
            {{ formatDecimalUSD(preview.charge.total_usd) }} ·
            {{ preview.charge.final_quota }} quota
          </p>
          <ul class="space-y-1">
            <li v-for="(line, index) in preview.charge.lines" :key="index">
              {{ line.quantity }}: {{ formatDecimal(line.count) }} ×
              {{ formatDecimalUSD(line.unit_price) }} =
              {{ formatDecimalUSD(line.subtotal_usd) }}
            </li>
          </ul>
          <template v-if="preview.charge.trace">
            <p v-if="preview.charge.trace.image_count !== undefined">
              {{ t('billing.quantities.images') }}:
              {{ preview.charge.trace.image_count }}
            </p>
            <p>
              {{ t('billing.matchedTier') }}:
              {{ preview.charge.trace.matched_tier || '—' }}
            </p>
            <p
              v-for="(rule, index) in preview.charge.trace.request_rules"
              :key="index"
              class="break-words text-xs"
              :class="rule.matched ? 'bg-[var(--accent-muted)]' : ''"
            >
              {{ rule.cond }} ·
              {{
                t(
                  rule.matched ? 'billing.ruleMatched' : 'billing.ruleUnmatched'
                )
              }}
            </p>
          </template>
        </div>
      </details>
    </fieldset>
    <ConfirmDialog
      :open="restoreConfirm"
      :title="t('billing.automatic.restore')"
      :message="t('billing.automatic.restoreConfirm', { model: originalName })"
      :confirm-text="t('billing.automatic.restore')"
      :loading="restoring"
      @confirm="restoreAutomatic"
      @cancel="restoreConfirm = false"
    />
  </section>
</template>
