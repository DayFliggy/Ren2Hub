<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  checkoutTarget,
  enabledPayMethods,
  parseCreationFailure,
  parseCreemProducts,
  quoteChangedFrom,
  rememberGmpayOrder,
  topupApi,
  type CreemProduct,
  type TopupInfo,
  type TopupQuote,
} from '@/api/topup'
import { ApiError } from '@/api/types'
import AmountInput from '@/components/common/AmountInput.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import PaymentMethods from '@/components/console/wallet/PaymentMethods.vue'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { nativePaymentMessages } from '@/i18n/nativePayment'
import { rateToPercent } from '@/features/topup-discount/amount'
import { compareDecimal } from '@/utils/decimal'
import { addQuota } from '@/utils/quota'
import { formatQuota } from '@/utils/format'
import { topupInputAmounts, validTopupAmount } from '@/utils/topupAmount'

const props = withDefaults(
  defineProps<{ balanceQuota?: string | null; paymentMethod?: string }>(),
  { balanceQuota: null, paymentMethod: '' }
)
const emit = defineEmits<{
  done: []
  'update:paymentMethod': [method: string]
  'track-order': [tradeNo: string]
  capabilities: [info: TopupInfo]
  'capabilities-state': [state: 'idle' | 'ready' | 'failed']
}>()
const { t } = useI18n()
const { t: paymentT } = useI18n({
  useScope: 'local',
  messages: nativePaymentMessages,
})
const toast = useToast()
const app = useAppStore()
const auth = useAuthStore()
const info = ref<TopupInfo | null>(null)
const configState = ref<'idle' | 'ready' | 'failed'>('idle')
watch(configState, (state) => emit('capabilities-state', state), {
  immediate: true,
})
const amount = ref<number | null>(10)
const submittingTopup = ref(false)
const paymentError = ref('')
const paymentBlocked = ref(false)
const product = ref('')
const submethod = ref('')
const products = ref<CreemProduct[]>([])
const method = computed({
  get: () => props.paymentMethod,
  set: (value: string) => emit('update:paymentMethod', value),
})
const tokens = computed(() => app.status.quota_display_type === 'TOKENS')
const paymentMethods = computed(() =>
  info.value
    ? enabledPayMethods(info.value).filter(
        (item) => !(tokens.value && item.type === 'stripe')
      )
    : []
)
const selectableMethods = computed(() =>
  paymentMethods.value.map((item) => ({ value: item.type, label: item.name }))
)
const isCrypto = computed(() => method.value.startsWith('gmpay:'))
const submitLabel = computed(() =>
  isCrypto.value ? t('wallet.gmpaySubmit') : t('wallet.topupNow')
)
const amounts = computed(() =>
  info.value ? topupInputAmounts(info.value, method.value, app.status) : null
)
const selectedMinimumTopup = computed(() => amounts.value?.minimum ?? 1)
const presets = computed(() =>
  (amounts.value?.presets ?? []).filter(
    (value) => value >= selectedMinimumTopup.value
  )
)
const selectedProduct = computed(() =>
  products.value.find((item) => item.productId === product.value)
)
const invalidAmount = computed(
  () =>
    configState.value === 'ready' &&
    method.value !== 'creem' &&
    amount.value !== null &&
    !validTopupAmount(amount.value, amounts.value)
)
// server is the discounted providers' quote, confirmed back on pay; Stripe and
// Creem prices are not discounted and carry none.
const quote = ref<{
  pay_amount: string
  currency: string
  expected_quota: string | null
  server: TopupQuote | null
} | null>(null)
const quoteLoading = ref(false)
const quoteError = ref('')
const quoteChanged = ref(false)
const quoteSymbol = computed(() => {
  const currency = quote.value?.currency ?? ''
  if (currency === 'CNY') return '¥'
  if (currency === 'USD') return '$'
  return currency ? `${currency} ` : ''
})
const payLabel = computed(() => {
  const currency = quote.value?.currency
  if (currency === 'CNY') return t('wallet.summaryPayNow')
  if (currency === 'USD') return t('wallet.summaryPayNowUsd')
  return paymentT('nativePayment.due')
})
const discount = computed(() => {
  const server = quote.value?.server
  if (!server || compareDecimal(server.applied_rate, '0') <= 0) return null
  return {
    listPayAmount: server.list_pay_amount,
    rate: rateToPercent(server.applied_rate),
  }
})
function serverQuote(value: TopupQuote) {
  return {
    pay_amount: value.pay_amount,
    currency: value.currency,
    expected_quota: value.expected_quota,
    server: value,
  }
}
let quoteSequence = 0
let quoteController: AbortController | null = null
let configController: AbortController | null = null
let disposed = false
const balanceProjection = computed(() => {
  if (!quote.value?.expected_quota || props.balanceQuota == null)
    return { quota: null, invalid: false }
  try {
    const quota = addQuota(props.balanceQuota, quote.value.expected_quota)
    return { quota, invalid: BigInt(quota) > BigInt(Number.MAX_SAFE_INTEGER) }
  } catch {
    return { quota: null, invalid: true }
  }
})
const balanceAfter = computed(() =>
  balanceProjection.value.quota === null
    ? '—'
    : formatQuota(balanceProjection.value.quota)
)
const canSubmit = computed(
  () =>
    configState.value === 'ready' &&
    !paymentBlocked.value &&
    !balanceProjection.value.invalid &&
    paymentMethods.value.some((item) => item.type === method.value) &&
    (method.value === 'creem'
      ? !!selectedProduct.value
      : amount.value !== null && !invalidAmount.value && !!quote.value) &&
    !quoteLoading.value
)
function trackOrder(tradeNo: string) {
  if (auth.user) rememberGmpayOrder(auth.user.id, tradeNo)
  emit('track-order', tradeNo)
}
async function topup(): Promise<void> {
  if (!canSubmit.value || submittingTopup.value) return
  submittingTopup.value = true
  paymentError.value = ''
  quoteChanged.value = false
  const selected = method.value
  const server = quote.value?.server
  const sequence = quoteSequence
  try {
    const response = await topupApi.pay(
      selected,
      selected === 'creem'
        ? { product_id: product.value }
        : {
            amount: amount.value!,
            ...(server
              ? {
                  expected_pay_amount: server.pay_amount,
                  expected_currency: server.currency,
                }
              : {}),
            ...(selected === 'waffo' && submethod.value !== ''
              ? { pay_method_index: Number(submethod.value) }
              : {}),
            ...(selected === 'stripe'
              ? {
                  success_url: `${window.location.origin}/wallet`,
                  cancel_url: `${window.location.origin}/wallet`,
                }
              : {}),
          }
    )
    if (selected.startsWith('gmpay:')) {
      const tracked = parseCreationFailure(response.data)
      if (!tracked) throw new ApiError(t('wallet.paymentMethodsUnavailable'))
      trackOrder(tracked.trade_no)
      paymentBlocked.value = true
    }
    if (disposed) return
    const target = checkoutTarget(selected, response)
    if (target.form) {
      const form = document.createElement('form')
      form.method = 'POST'
      form.action = target.url
      form.style.display = 'none'
      for (const [name, value] of Object.entries(target.form)) {
        const input = document.createElement('input')
        input.type = 'hidden'
        input.name = name
        input.value = value
        form.append(input)
      }
      document.body.append(form)
      try {
        form.submit()
      } finally {
        form.remove()
      }
    } else window.location.assign(target.url)
    toast.success(t('wallet.callbackNote'))
    emit('done')
  } catch (error) {
    // No order exists yet: show the new price and wait for another click.
    const changed = quoteChangedFrom(error)
    if (changed) {
      // A newer quote for changed input wins over this rejection.
      if (!disposed && sequence === quoteSequence) {
        quote.value = serverQuote(changed)
        quoteChanged.value = true
      }
      return
    }
    const failure =
      error instanceof ApiError ? parseCreationFailure(error.data) : undefined
    if (selected.startsWith('gmpay:')) {
      if (failure) trackOrder(failure.trade_no)
      // A timeout or malformed creation response does not prove rejection.
      paymentBlocked.value = failure
        ? failure.creation_status !== 'failed'
        : !(error instanceof ApiError && error.business)
    }
    if (!disposed)
      paymentError.value =
        error instanceof Error ? error.message : String(error)
  } finally {
    submittingTopup.value = false
  }
}
async function refreshQuote(): Promise<void> {
  const sequence = ++quoteSequence
  quoteController?.abort()
  quote.value = null
  quoteError.value = ''
  quoteChanged.value = false
  quoteLoading.value = false
  if (method.value === 'creem') {
    if (selectedProduct.value)
      quote.value = {
        pay_amount: String(selectedProduct.value.price),
        currency: selectedProduct.value.currency,
        expected_quota: String(selectedProduct.value.quota),
        server: null,
      }
    return
  }
  if (
    !paymentMethods.value.some((item) => item.type === method.value) ||
    amount.value === null ||
    invalidAmount.value
  )
    return
  const controller = new AbortController()
  quoteController = controller
  quoteLoading.value = true
  const currentAmount = amount.value
  try {
    const result = await topupApi.quote(
      method.value,
      currentAmount,
      controller.signal
    )
    if (sequence !== quoteSequence || controller.signal.aborted) return
    if (result.quote) {
      quote.value = serverQuote(result.quote)
      return
    }
    // Stripe answers only the payable amount; credit follows the request.
    const scale = app.status.quota_per_unit
    const credited =
      typeof scale === 'number' && Number.isSafeInteger(scale) && scale > 0
        ? tokens.value
          ? currentAmount
          : currentAmount * scale
        : null
    quote.value = {
      pay_amount: result.pay_amount,
      currency: '',
      expected_quota:
        credited !== null && Number.isSafeInteger(credited)
          ? String(credited)
          : null,
      server: null,
    }
  } catch (error) {
    if (sequence === quoteSequence && !controller.signal.aborted)
      quoteError.value =
        error instanceof ApiError && error.code !== 'INVALID_RESPONSE'
          ? error.message
          : t('wallet.quoteFailed')
  } finally {
    if (sequence === quoteSequence) quoteLoading.value = false
  }
}
async function loadPaymentConfig(): Promise<void> {
  configController?.abort()
  const controller = new AbortController()
  configController = controller
  configState.value = 'idle'
  try {
    const value = await topupApi.info(controller.signal)
    const catalog = value.enable_creem_topup
      ? parseCreemProducts(value.creem_products)
      : []
    if (controller.signal.aborted) return
    info.value = value
    products.value = catalog
    product.value = catalog[0]?.productId ?? ''
    configState.value = 'ready'
    emit('capabilities', value)
    if (!paymentMethods.value.some((item) => item.type === method.value))
      method.value = paymentMethods.value[0]?.type ?? ''
    amount.value = presets.value[0] ?? selectedMinimumTopup.value
    void refreshQuote()
  } catch {
    if (!controller.signal.aborted) configState.value = 'failed'
  }
}
watch(
  [
    amount,
    method,
    product,
    () => app.status.quota_per_unit,
    () => app.status.quota_display_type,
  ],
  () => void refreshQuote()
)
onMounted(() => void loadPaymentConfig())
onUnmounted(() => {
  disposed = true
  configController?.abort()
  ++quoteSequence
  quoteController?.abort()
})
</script>

<template>
  <ConsoleCard :title="t('wallet.quickTopup')">
    <p
      v-if="configState === 'idle'"
      role="status"
      class="mb-3 text-xs text-[var(--text-tertiary)]"
    >
      {{ t('common.loading') }}
    </p>
    <fieldset v-else :disabled="submittingTopup" class="min-w-0 border-0 p-0">
      <div v-if="method !== 'creem'" class="grid grid-cols-3 gap-2">
        <button
          v-for="preset in presets"
          :key="preset"
          type="button"
          class="h-14 rounded-xl text-sm font-bold transition-all focus-ring"
          :style="
            amount === preset
              ? 'background:var(--accent);color:var(--accent-contrast)'
              : 'background:var(--surface-muted);color:var(--text-secondary)'
          "
          :class="{ 'hover:bg-[var(--surface-hover)]': amount !== preset }"
          :aria-pressed="amount === preset"
          @click="amount = preset"
        >
          {{ tokens ? '' : '$' }}{{ preset }}
        </button>
      </div>

      <div v-if="method !== 'creem'" class="mt-4">
        <AmountInput
          v-model="amount"
          :placeholder="t('wallet.amountPlaceholder')"
          :min="selectedMinimumTopup"
          :prefix="tokens ? '' : '$'"
          integer
        />
        <p v-if="tokens" class="mt-2 text-xs text-[var(--text-tertiary)]">
          {{ t('wallet.nativeTokenUnit', { units: amounts?.scale ?? '—' }) }}
        </p>
        <p
          v-if="invalidAmount || balanceProjection.invalid"
          role="alert"
          class="mt-2 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('wallet.invalidTopupAmount') }}
        </p>
      </div>

      <div class="mt-4">
        <PaymentMethods v-model="method" :methods="selectableMethods" />
        <p
          v-if="configState === 'failed'"
          role="alert"
          class="mt-2 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('wallet.paymentMethodsUnavailable') }}
          <ConsoleButton
            class="mt-2"
            variant="secondary"
            size="sm"
            @click="loadPaymentConfig"
            >{{ t('common.retry') }}</ConsoleButton
          >
        </p>
        <p
          v-else-if="configState === 'ready' && !paymentMethods.length"
          class="mt-2 text-xs text-[var(--text-tertiary)]"
        >
          {{ paymentT('nativePayment.none') }}
        </p>
        <div v-if="method === 'creem' && products.length" class="mt-3">
          <label
            class="mb-1 block text-xs text-[var(--text-tertiary)]"
            for="topup-product"
            >{{ paymentT('nativePayment.product') }}</label
          >
          <select
            id="topup-product"
            v-model="product"
            class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 text-sm"
          >
            <option
              v-for="item in products"
              :key="item.productId"
              :value="item.productId"
            >
              {{ item.name }} · {{ item.currency }} {{ item.price }}
            </option>
          </select>
        </div>
        <div
          v-if="method === 'waffo' && info?.waffo_pay_methods?.length"
          class="mt-3"
        >
          <label
            class="mb-1 block text-xs text-[var(--text-tertiary)]"
            for="topup-submethod"
            >{{ paymentT('nativePayment.methodDetail') }}</label
          >
          <select
            id="topup-submethod"
            v-model="submethod"
            class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 text-sm"
          >
            <option value="">{{ paymentT('nativePayment.automatic') }}</option>
            <option
              v-for="(item, index) in info.waffo_pay_methods"
              :key="index"
              :value="String(index)"
            >
              {{ item.name }}
            </option>
          </select>
        </div>
      </div>

      <div
        class="mt-5 sibling-space-y-2 border-t border-[var(--border-subtle)] pt-4"
      >
        <template v-if="discount">
          <div class="flex items-center justify-between text-sm">
            <span class="text-[var(--text-secondary)]">{{
              t('wallet.summaryListPrice')
            }}</span>
            <span class="tabular-nums text-[var(--text-tertiary)] line-through"
              >{{ quoteSymbol }}{{ discount.listPayAmount }}</span
            >
          </div>
          <div class="flex items-center justify-between text-sm">
            <span class="text-[var(--text-secondary)]">{{
              t('wallet.summaryDiscount')
            }}</span>
            <span
              class="font-semibold tabular-nums text-[var(--status-success-text)]"
              >{{
                t('wallet.summaryDiscountRate', { rate: discount.rate })
              }}</span
            >
          </div>
        </template>
        <div class="flex items-center justify-between text-sm">
          <span class="text-[var(--text-secondary)]">{{ payLabel }}</span>
          <span class="font-semibold tabular-nums text-[var(--text-primary)]">
            {{ quote?.pay_amount ? `${quoteSymbol}${quote.pay_amount}` : '—' }}
          </span>
        </div>
        <div class="flex items-center justify-between text-sm">
          <span class="text-[var(--text-secondary)]">{{
            t('wallet.summaryCredit')
          }}</span>
          <span class="font-semibold tabular-nums text-[var(--text-primary)]">
            {{
              quote?.expected_quota ? formatQuota(quote.expected_quota, 3) : '—'
            }}
          </span>
        </div>
        <div class="flex items-center justify-between gap-3 text-sm">
          <span class="text-[var(--text-secondary)]">{{
            t('wallet.summaryBalanceAfter')
          }}</span>
          <span class="font-semibold tabular-nums text-[var(--accent-text)]">{{
            balanceAfter
          }}</span>
        </div>
      </div>

      <p
        v-if="quoteLoading"
        role="status"
        class="mt-3 text-xs text-[var(--text-tertiary)]"
      >
        {{ t('wallet.quoteLoading') }}
      </p>
      <div
        v-if="quoteError"
        role="alert"
        class="mt-3 text-xs text-[var(--status-danger-text)]"
      >
        <p>{{ quoteError }}</p>
        <ConsoleButton
          class="mt-2"
          variant="secondary"
          size="sm"
          @click="refreshQuote"
        >
          {{ t('wallet.retryQuote') }}
        </ConsoleButton>
      </div>
      <p
        v-if="quoteChanged"
        role="status"
        class="mt-3 text-xs text-[var(--status-warning-text)]"
      >
        {{ t('wallet.quoteChanged') }}
      </p>
      <p
        v-if="paymentBlocked"
        role="status"
        class="mt-3 text-xs text-[var(--status-warning-text)]"
      >
        {{ t('wallet.creationUncertain') }}
      </p>
      <p
        v-if="paymentError"
        role="alert"
        class="mt-3 text-xs text-[var(--status-danger-text)]"
      >
        {{ paymentError }}
      </p>

      <ConsoleButton
        class="mt-4"
        block
        size="lg"
        :loading="submittingTopup"
        :disabled="!canSubmit"
        @click="topup"
      >
        {{ submitLabel }}
      </ConsoleButton>

      <p v-if="isCrypto" class="mt-2 text-xs text-[var(--text-tertiary)]">
        {{ t('wallet.gmpayNetworkGuidance') }}
      </p>
      <p v-if="isCrypto" class="mt-2 text-xs text-[var(--text-tertiary)]">
        {{ t('wallet.gmpayRedirectNote') }}
      </p>
      <p class="mt-3 text-xs text-[var(--text-tertiary)]">
        {{ t('wallet.callbackNote') }}
        {{ t('wallet.nativeCurrencyNote') }}
      </p>
    </fieldset>
  </ConsoleCard>
</template>
