<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from 'lucide-vue-next'
import {
  MODEL_PRICE_KEYS,
  mergeModelPriceEdit,
  modelPriceFields,
  modelPricingApi,
  validateModelPriceFields,
  type ModelPriceMaps,
  type ModelPriceMode,
} from '@/api/modelPricing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

const props = withDefaults(
  defineProps<{ open?: boolean; modelName: string }>(),
  { open: true }
)
const { t } = useI18n({
  useScope: 'local',
  messages: {
    'zh-CN': {
      pricing: '计费',
      perToken: '按量计费',
      perRequest: '按次计费',
      ratio: '倍率',
      unitPrice: '每百万 Token 价格',
      inputPrice: '输入价格（USD / 百万 Token）',
      outputPrice: '输出价格（USD / 百万 Token）',
      ModelPrice: '每次请求价格（USD）',
      ModelRatio: '模型输入倍率',
      CompletionRatio: '输出倍率',
      CacheRatio: '缓存读取倍率',
      ImageRatio: '图像倍率',
      AudioRatio: '音频输入倍率',
      AudioCompletionRatio: '音频输出倍率',
      loading: '正在读取价格配置',
      retry: '重新读取',
      unavailable: '部分价格配置未返回，对应字段不可修改。',
      priceInvalidNumber: '价格与倍率必须是有限的非负数。',
      priceUnavailable: '价格配置未成功加载，无法保存价格。',
      priceConcurrentChange: '该模型的价格已被其他操作修改，请重新打开编辑器。',
      priceTargetConflict:
        '目标模型名称已有价格配置，请检查名称或先处理价格冲突。',
      priceInputRequired: '输出价格大于零时，输入价格必须大于零。',
    },
    en: {
      pricing: 'Billing',
      perToken: 'Usage-based',
      perRequest: 'Per-request',
      ratio: 'Ratios',
      unitPrice: 'Price per million tokens',
      inputPrice: 'Input price (USD / 1M tokens)',
      outputPrice: 'Output price (USD / 1M tokens)',
      ModelPrice: 'Price per request (USD)',
      ModelRatio: 'Input model ratio',
      CompletionRatio: 'Completion ratio',
      CacheRatio: 'Cache read ratio',
      ImageRatio: 'Image ratio',
      AudioRatio: 'Audio input ratio',
      AudioCompletionRatio: 'Audio output ratio',
      loading: 'Loading pricing',
      retry: 'Reload pricing',
      unavailable:
        'Some price configurations are unavailable. Their fields cannot be edited.',
      priceInvalidNumber:
        'Prices and ratios must be finite, non-negative numbers.',
      priceUnavailable:
        'Pricing has not loaded successfully. Price changes cannot be saved.',
      priceConcurrentChange:
        'This model pricing changed elsewhere. Reopen the editor to load it again.',
      priceTargetConflict:
        'The target model name already has pricing. Resolve the name or price conflict first.',
      priceInputRequired:
        'A positive output price requires a positive input price.',
    },
  },
})
const maps = ref<ModelPriceMaps | null>(null)
const fields = ref(modelPriceFields({}, ''))
const mode = ref<ModelPriceMode>('per-token')
const subMode = ref('ratio')
const loading = ref(false)
const error = ref('')
const originalName = ref('')
const initialForm = ref('')
const inputPrice = ref('')
const outputPrice = ref('')
let request: AbortController | null = null
const dirty = computed(
  () => JSON.stringify([mode.value, fields.value]) !== initialForm.value
)
const missing = computed(
  () => maps.value && MODEL_PRICE_KEYS.some((key) => !maps.value?.[key])
)

async function load() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  error.value = ''
  maps.value = null
  try {
    const data = await modelPricingApi.load(controller.signal)
    if (controller.signal.aborted) return
    originalName.value = props.modelName
    maps.value = data
    fields.value = modelPriceFields(data, originalName.value)
    mode.value = fields.value.ModelPrice !== '' ? 'per-request' : 'per-token'
    initialForm.value = JSON.stringify([mode.value, fields.value])
    updateUnitPrices()
  } catch (cause) {
    if (!controller.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (request === controller) loading.value = false
  }
}
function updateUnitPrices() {
  const ratio = fields.value.ModelRatio
  inputPrice.value = ratio === '' ? '' : String(Number(ratio) * 2)
  outputPrice.value =
    ratio === '' || fields.value.CompletionRatio === ''
      ? ''
      : String(Number(ratio) * 2 * Number(fields.value.CompletionRatio))
}
function updateRatios() {
  fields.value.ModelRatio =
    inputPrice.value === '' ? '' : String(Number(inputPrice.value) / 2)
  fields.value.CompletionRatio =
    outputPrice.value === ''
      ? ''
      : Number(inputPrice.value) > 0
        ? String(Number(outputPrice.value) / Number(inputPrice.value))
        : Number(outputPrice.value) === 0
          ? '0'
          : 'NaN'
}
function onUnitPriceInput(field: 'input' | 'output', event: Event) {
  const value = (event.target as HTMLInputElement).value
  if (field === 'input') inputPrice.value = value
  else outputPrice.value = value
  updateRatios()
}
async function validate(name?: string) {
  if (!maps.value || loading.value) throw new Error(t('priceUnavailable'))
  try {
    validateModelPriceFields(fields.value)
  } catch (cause) {
    throw new Error(t('priceInvalidNumber'), { cause })
  }
  if (
    subMode.value === 'price' &&
    Number(outputPrice.value) > 0 &&
    Number(inputPrice.value) <= 0
  )
    throw new Error(t('priceInputRequired'))
  if (name && (dirty.value || name !== originalName.value)) {
    try {
      mergeModelPriceEdit(await modelPricingApi.load(), {
        initial: maps.value,
        originalName: originalName.value,
        name,
        mode: mode.value,
        fields: fields.value,
        dirty: dirty.value,
      })
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause)
      throw new Error(message.startsWith('price') ? t(message) : message, {
        cause,
      })
    }
  }
}
async function save(name: string) {
  await validate()
  try {
    maps.value = await modelPricingApi.save({
      initial: maps.value!,
      originalName: originalName.value,
      name,
      mode: mode.value,
      fields: fields.value,
      dirty: dirty.value,
    })
    originalName.value = name
    fields.value = modelPriceFields(maps.value, name)
    initialForm.value = JSON.stringify([mode.value, fields.value])
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause)
    throw new Error(message.startsWith('price') ? t(message) : message, {
      cause,
    })
  }
}
watch(
  () => props.open,
  (open) => {
    if (open) void load()
    else request?.abort()
  },
  { immediate: true }
)
onBeforeUnmount(() => request?.abort())
defineExpose({ validate, save })
</script>
<template>
  <section class="admin-section">
    <h3>{{ t('pricing') }}</h3>
    <p v-if="loading" role="status" class="admin-muted">{{ t('loading') }}</p>
    <p v-if="error" role="alert" class="admin-error">{{ error }}</p>
    <ConsoleButton v-if="error" type="button" variant="ghost" @click="load"
      ><RefreshCw :size="16" />{{ t('retry') }}</ConsoleButton
    >
    <p v-if="missing" class="admin-muted">{{ t('unavailable') }}</p>
    <fieldset v-if="maps" class="admin-form" :disabled="loading">
      <div class="admin-row-actions" role="group" :aria-label="t('pricing')">
        <label class="admin-check"
          ><input v-model="mode" type="radio" value="per-token" />{{
            t('perToken')
          }}</label
        >
        <label class="admin-check"
          ><input v-model="mode" type="radio" value="per-request" />{{
            t('perRequest')
          }}</label
        >
      </div>
      <label v-if="mode === 'per-request'" class="admin-field"
        ><span>{{ t('ModelPrice') }}</span
        ><input
          :value="fields.ModelPrice"
          :disabled="!maps.ModelPrice"
          type="number"
          min="0"
          step="any"
          @input="
            fields.ModelPrice = ($event.target as HTMLInputElement).value
          "
      /></label>
      <template v-else>
        <div class="admin-row-actions" role="group" :aria-label="t('ratio')">
          <label class="admin-check"
            ><input v-model="subMode" type="radio" value="ratio" />{{
              t('ratio')
            }}</label
          >
          <label class="admin-check"
            ><input
              v-model="subMode"
              type="radio"
              value="price"
              @change="updateUnitPrices"
            />{{ t('unitPrice') }}</label
          >
        </div>
        <div v-if="subMode === 'price'" class="admin-fields">
          <label class="admin-field"
            ><span>{{ t('inputPrice') }}</span
            ><input
              :value="inputPrice"
              :disabled="!maps.ModelRatio || !maps.CompletionRatio"
              type="number"
              min="0"
              step="any"
              @input="onUnitPriceInput('input', $event)"
          /></label>
          <label class="admin-field"
            ><span>{{ t('outputPrice') }}</span
            ><input
              :value="outputPrice"
              :disabled="!maps.ModelRatio || !maps.CompletionRatio"
              type="number"
              min="0"
              step="any"
              @input="onUnitPriceInput('output', $event)"
          /></label>
        </div>
        <div class="admin-fields">
          <template
            v-for="key in MODEL_PRICE_KEYS.filter(
              (key) => key !== 'ModelPrice'
            )"
            :key="key"
          >
            <label
              v-if="
                subMode === 'ratio' ||
                (key !== 'ModelRatio' && key !== 'CompletionRatio')
              "
              class="admin-field"
              ><span>{{ t(key) }}</span
              ><input
                :value="fields[key]"
                :disabled="!maps[key]"
                type="number"
                min="0"
                step="any"
                @input="
                  fields[key] = ($event.target as HTMLInputElement).value
                "
            /></label>
          </template>
        </div>
      </template>
    </fieldset>
  </section>
</template>
