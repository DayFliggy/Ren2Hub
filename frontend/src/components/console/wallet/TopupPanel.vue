<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { parseGmpayTopupInfo, parseTopupInfo } from '@/api/liveContracts'
import AmountInput from '@/components/common/AmountInput.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import PaymentMethods from '@/components/console/wallet/PaymentMethods.vue'
import { useToast } from '@/composables/useToast'
import { addQuota, decimalToQuota, isQuota } from '@/utils/quota'
import { formatQuota } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    balanceQuota?: string | null
    paymentMethod?: string
  }>(),
  { balanceQuota: null, paymentMethod: 'epay' }
)

const emit = defineEmits<{
  done: []
  'update:paymentMethod': [method: string]
}>()

const { t } = useI18n()
const toast = useToast()

const amount = ref<number | null>(10)
const submittingTopup = ref(false)
type PaymentProvider = 'epay' | 'gmpay'
interface SelectablePaymentMethod {
  value: string
  label: string
  minTopup: number
  amountOptions: number[]
}
const providerMethods = reactive<
  Record<PaymentProvider, SelectablePaymentMethod[]>
>({ epay: [], gmpay: [] })
const configStates = reactive<
  Record<PaymentProvider, 'idle' | 'ready' | 'failed'>
>({ epay: 'idle', gmpay: 'idle' })
const configControllers: Record<PaymentProvider, AbortController | null> = {
  epay: null,
  gmpay: null,
}
const paymentMethods = computed(() => [
  ...providerMethods.epay,
  ...providerMethods.gmpay,
])
let initialSelectionApplied = false
/**
 * One server-priced offer. Epay settles in CNY and GMPay in USD, so the
 * currency travels with the quote instead of being assumed by the panel.
 */
interface TopupQuote {
  amount: number
  amount_unit: 'USD'
  payment_method: string
  currency: 'CNY' | 'USD'
  amount_minor: string
  pay_amount: string
  expected_quota: string
  min_topup: number
  quote_fingerprint: string
  token?: string
  network?: string
}
const quote = ref<TopupQuote | null>(null)
const quoteLoading = ref(false)
const quoteError = ref('')
const quoteChanged = ref(false)
let quoteSequence = 0
let quoteController: AbortController | null = null
let disposed = false
const method = computed({
  get: () => props.paymentMethod,
  set: (value: string) => emit('update:paymentMethod', value),
})

const selectableMethods = computed(() => paymentMethods.value)
const isCrypto = computed(() => method.value.startsWith('gmpay:'))
const submitLabel = computed(() =>
  isCrypto.value ? t('wallet.gmpaySubmit') : t('wallet.topupNow')
)
/** The cashier currency symbol, taken from the server quote and never guessed. */
const quoteSymbol = computed(() =>
  quote.value?.currency === 'USD' ? '$' : '\u00a5'
)
const orderEndpoint = computed(() =>
  isCrypto.value ? '/api/next/wallet/gmpay/topup' : '/api/next/wallet/topup'
)
/** Both rails take the bare provider method; the prefix is a UI concern only. */
function providerMethod(value: string): string {
  return value.replace(/^(?:epay|gmpay):/, '')
}
const selectedMethod = computed(() =>
  paymentMethods.value.find((item) => item.value === method.value)
)
const selectedMinimumTopup = computed(() => selectedMethod.value?.minTopup ?? 1)
const presets = computed(() => {
  if (!selectedMethod.value) return []
  const valid = selectedMethod.value.amountOptions.filter(
    (value) => value >= selectedMinimumTopup.value
  )
  return valid.length ? valid : [selectedMinimumTopup.value]
})
const invalidAmount = computed(() => {
  if (amount.value === null) return false
  if (!Number.isSafeInteger(amount.value) || amount.value <= 0) return true
  try {
    decimalToQuota(String(amount.value))
    return false
  } catch (error) {
    if (!(error instanceof RangeError)) throw error
    return true
  }
})
const balanceProjection = computed(() => {
  if (!quote.value) return { quota: null, invalid: false }
  try {
    return {
      quota:
        props.balanceQuota == null
          ? null
          : addQuota(props.balanceQuota, quote.value.expected_quota),
      invalid: false,
    }
  } catch (error) {
    if (!(error instanceof RangeError)) throw error
    return { quota: null, invalid: true }
  }
})
const canSubmit = computed(
  () =>
    amount.value !== null &&
    !invalidAmount.value &&
    !balanceProjection.value.invalid &&
    amount.value >= selectedMinimumTopup.value &&
    selectedMethod.value !== undefined &&
    quote.value !== null &&
    !quoteLoading.value
)

const balanceAfter = computed(() =>
  balanceProjection.value.quota === null
    ? '—'
    : formatQuota(balanceProjection.value.quota)
)

async function topup(): Promise<void> {
  if (!canSubmit.value || submittingTopup.value) return
  submittingTopup.value = true
  let navigating = false
  try {
    const value = amount.value
    const payload = {
      amount: value,
      amount_unit: 'USD',
      payment_method: providerMethod(method.value),
      quote_fingerprint: quote.value?.quote_fingerprint,
    }
    if (isCrypto.value) {
      // A crypto checkout leaves the site for the gateway cashier, which is the
      // only place the deposit address is shown.
      const response = await api.post<{ url: string; trade_no: string }>(
        orderEndpoint.value,
        payload
      )
      if (disposed) return
      let cashier: URL
      try {
        cashier = new URL(response.url)
      } catch {
        throw new ApiError(t('wallet.paymentMethodsUnavailable'))
      }
      if (
        cashier.protocol !== 'https:' ||
        cashier.username ||
        cashier.password
      ) {
        throw new ApiError(t('wallet.paymentMethodsUnavailable'))
      }
      toast.success(t('wallet.callbackNote'))
      window.location.href = cashier.href
      navigating = true
      emit('done')
      return
    }
    const response = await api.post<{
      url: string
      data: Record<string, unknown>
    }>(orderEndpoint.value, payload)
    if (disposed) return
    if (!response.url || !response.data) {
      throw new ApiError(t('wallet.paymentMethodsUnavailable'))
    }
    const form = document.createElement('form')
    form.method = 'POST'
    form.action = response.url
    form.style.display = 'none'
    for (const [key, raw] of Object.entries(response.data)) {
      const input = document.createElement('input')
      input.type = 'hidden'
      input.name = key
      input.value = String(raw)
      form.append(input)
    }
    document.body.append(form)
    form.submit()
    navigating = true
    form.remove()
    toast.success(t('wallet.callbackNote'))
    emit('done')
  } catch (error) {
    if (disposed) return
    if (error instanceof ApiError && error.code === 'QUOTE_CHANGED') {
      quoteChanged.value = true
      await refreshQuote()
      return
    }
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    if (!navigating) submittingTopup.value = false
  }
}

async function refreshQuote(): Promise<void> {
  const sequence = ++quoteSequence
  quoteController?.abort()
  quote.value = null
  quoteError.value = ''
  quoteLoading.value = false
  const currentAmount = amount.value
  const currentMethod = method.value
  if (
    !selectedMethod.value ||
    currentAmount === null ||
    invalidAmount.value ||
    currentAmount < selectedMinimumTopup.value ||
    !paymentMethods.value.some((item) => item.value === currentMethod)
  ) {
    return
  }
  const controller = new AbortController()
  quoteController = controller
  quoteLoading.value = true
  try {
    const crypto = currentMethod.startsWith('gmpay:')
    const expectedCurrency = crypto ? 'USD' : 'CNY'
    const expectedMethod = providerMethod(currentMethod)
    const response = await api.post<TopupQuote>(
      crypto ? '/api/next/wallet/gmpay/quote' : '/api/next/wallet/topup/quote',
      {
        amount: currentAmount,
        amount_unit: 'USD',
        payment_method: expectedMethod,
      },
      { signal: controller.signal }
    )
    if (sequence !== quoteSequence || controller.signal.aborted) return
    if (
      !response ||
      response.amount !== currentAmount ||
      response.amount_unit !== 'USD' ||
      response.payment_method !== expectedMethod ||
      response.currency !== expectedCurrency ||
      !isQuota(response.amount_minor) ||
      BigInt(response.amount_minor) <= 0n ||
      !isQuota(response.expected_quota) ||
      BigInt(response.expected_quota) <= 0n ||
      !Number.isSafeInteger(response.min_topup) ||
      response.min_topup < 1 ||
      typeof response.quote_fingerprint !== 'string' ||
      !response.quote_fingerprint ||
      response.pay_amount !==
        `${BigInt(response.amount_minor) / 100n}.${(BigInt(response.amount_minor) % 100n).toString().padStart(2, '0')}` ||
      (crypto &&
        (!response.token ||
          !response.network ||
          `gmpay:${response.token}.${response.network}` !== currentMethod))
    ) {
      throw new Error('Invalid payment quote')
    }
    quote.value = response
  } catch (error) {
    if (sequence === quoteSequence && !controller.signal.aborted) {
      quoteError.value =
        error instanceof ApiError ? error.message : t('wallet.quoteFailed')
    }
  } finally {
    if (sequence === quoteSequence) quoteLoading.value = false
  }
}

async function loadPaymentConfig(provider: PaymentProvider): Promise<void> {
  configControllers[provider]?.abort()
  const controller = new AbortController()
  configControllers[provider] = controller
  configStates[provider] = 'idle'
  providerMethods[provider] = []
  try {
    const raw = await api.get<unknown>(
      provider === 'gmpay'
        ? '/api/next/wallet/gmpay/config'
        : '/api/next/wallet/config',
      undefined,
      { signal: controller.signal }
    )
    if (controller.signal.aborted) return
    const info =
      provider === 'gmpay' ? parseGmpayTopupInfo(raw) : parseTopupInfo(raw)
    const enabled =
      'enable_gmpay_topup' in info
        ? info.enable_gmpay_topup
        : info.enable_online_topup
    providerMethods[provider] = enabled
      ? info.pay_methods.map((item) => ({
          value: provider === 'gmpay' ? item.type : `epay:${item.type}`,
          label: item.name,
          minTopup: Math.max(item.min_topup ?? info.min_topup, info.min_topup),
          amountOptions: info.amount_options,
        }))
      : []
    configStates[provider] = 'ready'
  } catch {
    if (!controller.signal.aborted) configStates[provider] = 'failed'
  }
}

onMounted(() => {
  void loadPaymentConfig('epay')
  void loadPaymentConfig('gmpay')
})

watch(
  [paymentMethods, method, () => configStates.epay, () => configStates.gmpay],
  () => {
    if (!selectedMethod.value) {
      const preferred = method.value.startsWith('gmpay:') ? 'gmpay' : 'epay'
      if (method.value.includes(':') && configStates[preferred] === 'idle')
        return
      const first = paymentMethods.value[0]
      if (first) method.value = first.value
      return
    }
    if (!initialSelectionApplied) {
      initialSelectionApplied = true
      if (amount.value === null || amount.value < selectedMinimumTopup.value) {
        amount.value = presets.value[0] ?? selectedMinimumTopup.value
      }
    }
  },
  { flush: 'sync' }
)

watch(
  [amount, selectedMethod],
  () => {
    quoteChanged.value = false
    void refreshQuote()
  },
  { flush: 'sync' }
)

onUnmounted(() => {
  disposed = true
  configControllers.epay?.abort()
  configControllers.gmpay?.abort()
  ++quoteSequence
  quoteController?.abort()
})
</script>

<template>
  <ConsoleCard :title="t('wallet.quickTopup')">
    <fieldset :disabled="submittingTopup" class="min-w-0 border-0 p-0">
      <div class="grid grid-cols-3 gap-2">
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
          ${{ preset }}
        </button>
      </div>

      <div class="mt-4">
        <AmountInput
          v-model="amount"
          :placeholder="t('wallet.amountPlaceholder')"
          :min="selectedMinimumTopup"
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
          v-if="configStates.epay === 'failed'"
          role="alert"
          class="mt-2 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('wallet.epayMethodsLoadFailed') }}
          <ConsoleButton
            class="mt-2"
            variant="secondary"
            size="sm"
            @click="loadPaymentConfig('epay')"
            >{{ t('wallet.retryEpayMethods') }}</ConsoleButton
          >
        </p>
        <p
          v-if="configStates.gmpay === 'failed'"
          role="alert"
          class="mt-2 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('wallet.gmpayMethodsLoadFailed') }}
          <ConsoleButton
            class="mt-2"
            variant="secondary"
            size="sm"
            @click="loadPaymentConfig('gmpay')"
            >{{ t('wallet.retryGmpayMethods') }}</ConsoleButton
          >
        </p>
        <p
          v-if="
            configStates.epay === 'ready' &&
            configStates.gmpay === 'ready' &&
            !paymentMethods.length
          "
          class="mt-2 text-xs text-[var(--text-tertiary)]"
        >
          {{ t('wallet.paymentMethodsUnavailable') }}
        </p>
      </div>

      <div
        class="mt-5 sibling-space-y-2 border-t border-[var(--border-subtle)] pt-4"
      >
        <div class="flex items-center justify-between text-sm">
          <span class="text-[var(--text-secondary)]">{{
            t(
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
            {{ quote ? formatQuota(quote.expected_quota, 3) : '—' }}
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
        class="mt-3 text-xs text-[var(--text-secondary)]"
      >
        {{ t('wallet.quoteChanged') }}
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
      </p>
    </fieldset>
  </ConsoleCard>
</template>
