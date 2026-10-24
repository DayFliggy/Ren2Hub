<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  checkoutTarget,
  enabledPayMethods,
  parseCreationFailure,
  parseCreemProducts,
  rememberGmpayOrder,
  topupApi,
  type CreemProduct,
  type TopupInfo,
} from '@/api/topup'
import { ApiError } from '@/api/types'
import AmountInput from '@/components/common/AmountInput.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import FormField from '@/components/common/FormField.vue'
import PaymentMethods from '@/components/console/wallet/PaymentMethods.vue'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { nativePaymentMessages } from '@/i18n/nativePayment'
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
    method.value !== 'creem' &&
    amount.value !== null &&
    !validTopupAmount(amount.value, amounts.value)
)
const quote = ref<{
  pay_amount: string
  currency: string
  expected_quota: string | null
} | null>(null)
const quoteLoading = ref(false)
const quoteError = ref('')
const quoteSymbol = computed(() =>
  quote.value?.currency === 'CNY'
    ? '¥'
    : quote.value?.currency === 'USD'
      ? '$'
      : ''
)
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
  const selected = method.value
  try {
    const response = await topupApi.pay(
      selected,
      selected === 'creem'
        ? { product_id: product.value }
        : {
            amount: amount.value!,
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
  quoteLoading.value = false
  if (method.value === 'creem') {
    if (selectedProduct.value)
      quote.value = {
        pay_amount: String(selectedProduct.value.price),
        currency: selectedProduct.value.currency,
        expected_quota: String(selectedProduct.value.quota),
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
    const due = await topupApi.quote(
      method.value,
      currentAmount,
      controller.signal
    )
    if (sequence !== quoteSequence || controller.signal.aborted) return
    if (
      typeof due !== 'string' ||
      !/^\d+(?:\.\d+)?$/.test(due) ||
      Number(due) <= 0 ||
      !Number.isFinite(Number(due))
    )
      throw new Error(t('wallet.quoteFailed'))
    const scale = app.status.quota_per_unit
    const credited =
      typeof scale === 'number' && Number.isSafeInteger(scale) && scale > 0
        ? tokens.value
          ? currentAmount
          : currentAmount * scale
        : null
    quote.value = {
      pay_amount: due,
      currency:
        isCrypto.value ||
        !['stripe', 'waffo', 'waffo_pancake'].includes(method.value)
          ? 'CNY'
          : method.value === 'waffo_pancake'
            ? 'USD'
            : '',
      expected_quota:
        credited !== null && Number.isSafeInteger(credited)
          ? String(credited)
          : null,
    }
  } catch (error) {
    if (sequence === quoteSequence && !controller.signal.aborted)
      quoteError.value =
        error instanceof ApiError ? error.message : t('wallet.quoteFailed')
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
    <fieldset :disabled="submittingTopup" class="min-w-0 border-0 p-0">
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
          {{ t('wallet.epayMethodsLoadFailed') }}
          <ConsoleButton
            class="mt-2"
            variant="secondary"
            size="sm"
            @click="loadPaymentConfig"
            >{{ t('common.retry') }}</ConsoleButton
          >
        </p>
        <p
          v-if="configState === 'ready' && !paymentMethods.length"
          class="mt-2 text-xs text-[var(--text-tertiary)]"
        >
          {{ t('wallet.paymentMethodsUnavailable') }}
        </p>
        <p v-if="tokens" class="mt-2 text-xs text-[var(--text-tertiary)]">
          {{ t('wallet.nativeTokenUnit', { units: amounts?.scale ?? '—' }) }}
        </p>
      </div>

      <FormField
        v-if="method === 'creem'"
        :label="t('wallet.nativeProduct')"
        class="mt-4"
      >
        <select v-model="product" class="console-input w-full">
          <option
            v-for="item in products"
            :key="item.productId"
            :value="item.productId"
          >
            {{ item.name }} · {{ item.price }} {{ item.currency }}
          </option>
        </select>
      </FormField>
      <FormField
        v-if="method === 'waffo' && info?.waffo_pay_methods?.length"
        :label="t('wallet.payMethodLabel')"
        class="mt-4"
      >
        <select v-model="submethod" class="console-input w-full">
          <option value="">{{ paymentT('nativePayment.automatic') }}</option>
          <option
            v-for="(item, index) in info.waffo_pay_methods"
            :key="index"
            :value="String(index)"
          >
            {{ item.name }}
          </option>
        </select>
      </FormField>
      <div
        class="mt-5 sibling-space-y-2 border-t border-[var(--border-subtle)] pt-4"
      >
        <div class="flex items-center justify-between text-sm">
          <span class="text-[var(--text-secondary)]">{{
            !quote?.currency
              ? paymentT('nativePayment.due')
              : t(
                  quote?.currency === 'USD'
                    ? 'wallet.summaryPayNowUsd'
                    : 'wallet.summaryPayNow'
                )
          }}</span>
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
        v-if="paymentError"
        role="alert"
        class="mt-3 text-xs text-[var(--status-danger-text)]"
      >
        {{ paymentError }}
      </p>
      <p
        v-if="paymentBlocked"
        role="status"
        class="mt-3 text-xs text-[var(--text-secondary)]"
      >
        {{ t('wallet.creationUncertain') }}
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
