<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  modelPricingApi,
  PRICE_QUANTITIES,
  PRICE_UNITS,
  validatePriceVersion,
  type PriceMode,
  type PriceVersion,
  type UnitPrice,
} from '@/api/modelPricing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

const props = withDefaults(
  defineProps<{ open?: boolean; modelName: string }>(),
  { open: true }
)
const { t } = useI18n()
const initial = ref<PriceVersion | null>(null)
const mode = ref<PriceMode>('unit_price')
const prices = ref<UnitPrice[]>([])
const expression = ref('')
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const originalName = ref('')
const seedMessage = ref('')
let request: AbortController | null = null
const snapshot = () =>
  JSON.stringify([mode.value, prices.value, expression.value])
const initialForm = ref('')
const dirty = computed(() => initialForm.value !== snapshot())

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
    initial.value =
      data.find((price) => price.model === props.modelName) ?? null
    mode.value = initial.value?.mode ?? 'unit_price'
    prices.value = initial.value?.prices.map((line) => ({ ...line })) ?? [
      { quantity: 'p', unit: 'per_million_tokens', price: '' },
      { quantity: 'c', unit: 'per_million_tokens', price: '' },
    ]
    expression.value = initial.value?.expression ?? ''
    initialForm.value = snapshot()
    loaded.value = true
  } catch {
    if (!controller.signal.aborted) error.value = t('billing.priceUnavailable')
  } finally {
    if (request === controller) loading.value = false
  }
}
function draft(name: string): PriceVersion {
  return {
    id: 0,
    model: name,
    mode: mode.value,
    prices:
      mode.value === 'free' ? [] : prices.value.map((price) => ({ ...price })),
    expression:
      mode.value === 'expression' ? expression.value.trim() : undefined,
    created_at: 0,
    hash: '',
  }
}
async function validate(name = props.modelName) {
  if (!loaded.value || loading.value)
    throw new Error(t('billing.priceUnavailable'))
  try {
    validatePriceVersion(draft(name))
  } catch {
    throw new Error(t('billing.invalidPrice'))
  }
  if (!name.trim()) throw new Error(t('billing.invalidPrice'))
  if (!dirty.value && name === originalName.value) return
  const latest = await modelPricingApi.load()
  const current = latest.find((row) => row.model === name)
  const expected = name === originalName.value ? (initial.value?.id ?? 0) : 0
  if ((current?.id ?? 0) !== expected)
    throw new Error(t('billing.priceConflict'))
}
async function save(name: string) {
  await validate(name)
  if (!dirty.value && name === originalName.value && initial.value) return
  initial.value = await modelPricingApi.publish(
    draft(name),
    name === originalName.value ? (initial.value?.id ?? 0) : 0
  )
  originalName.value = name
  initialForm.value = snapshot()
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
    mode.value = price.mode
    prices.value = price.prices.map((line) => ({ ...line }))
    expression.value = price.expression ?? ''
    seedMessage.value = t('billing.seedLoaded', { source: seed.source })
  } catch {
    if (!controller.signal.aborted) error.value = t('billing.priceUnavailable')
  } finally {
    if (request === controller) loading.value = false
  }
}
watch(mode, (value) => {
  if (value === 'expression')
    prices.value = prices.value.filter(
      (price) =>
        ['calls', 'images', 'seconds'].includes(price.quantity) ||
        price.quantity.startsWith('tool:')
    )
})
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
      v-if="error"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <div class="flex flex-wrap gap-2">
      <ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('billing.refresh')
      }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading || !loaded"
        @click="importSeed"
        >{{ t('billing.importSeed') }}</ConsoleButton
      >
    </div>
    <p
      v-if="seedMessage"
      role="status"
      class="text-xs text-[var(--text-secondary)]"
    >
      {{ seedMessage }}
    </p>
    <fieldset :disabled="loading || !loaded" class="space-y-4">
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
      <template v-if="mode !== 'free'">
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
                v-for="quantity in PRICE_QUANTITIES"
                :key="quantity"
                :value="quantity"
              >
                {{ t(`billing.quantities.${quantity}`) }}
              </option>
            </datalist></label
          >
          <label class="text-xs"
            >{{ t('billing.unit')
            }}<select
              v-model="line.unit"
              class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 focus-ring"
            >
              <option v-for="unit in PRICE_UNITS" :key="unit" :value="unit">
                {{ t(`billing.units.${unit}`) }}
              </option>
            </select></label
          >
          <label class="text-xs"
            >{{ t('billing.unitPrice')
            }}<input
              v-model="line.price"
              inputmode="decimal"
              class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 font-mono focus-ring"
          /></label>
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
      <p v-if="mode === 'free'" class="text-sm text-[var(--text-secondary)]">
        {{ t('billing.freeHint') }}
      </p>
    </fieldset>
  </section>
</template>
