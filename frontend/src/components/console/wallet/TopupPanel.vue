<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { parseTopupInfo } from '@/api/liveContracts'
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

const presets = ref([10, 20, 50, 100, 200, 500])
const amount = ref<number | null>(10)
const submittingTopup = ref(false)
const paymentMethods = ref<
  Array<{ value: string; label: string; minTopup: number }>
>([])
const minimumTopup = ref(1)
const paymentLoadState = ref<'idle' | 'ready' | 'failed'>('idle')
interface EpayQuote {
  amount: number
  amount_unit: 'USD'
  payment_method: string
  currency: 'CNY'
  amount_minor: string
  pay_amount: string
  expected_quota: string
  min_topup: number
  quote_fingerprint: string
}
const quote = ref<EpayQuote | null>(null)
const quoteLoading = ref(false)
const quoteError = ref('')
const quoteChanged = ref(false)
let quoteSequence = 0
let quoteController: AbortController | null = null
const configController = new AbortController()
const method = computed({
  get: () => props.paymentMethod,
  set: (value: string) => emit('update:paymentMethod', value),
})

const selectableMethods = computed(() => paymentMethods.value)
const selectedMinimumTopup = computed(
  () =>
    paymentMethods.value.find((item) => item.value === method.value)
      ?.minTopup ?? minimumTopup.value
)
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
    paymentLoadState.value === 'ready' &&
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
  try {
    const value = amount.value
    const epayMethod = method.value.replace(/^epay:/, '')
    const response = await api.post<{
      url: string
      data: Record<string, unknown>
    }>('/api/next/wallet/topup', {
      amount: value,
      amount_unit: 'USD',
      payment_method: epayMethod,
      quote_fingerprint: quote.value?.quote_fingerprint,
    })
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
    form.remove()
    toast.success(t('wallet.callbackNote'))
    emit('done')
  } catch (error) {
    if (error instanceof ApiError && error.code === 'QUOTE_CHANGED') {
      quoteChanged.value = true
      await refreshQuote()
      return
    }
    toast.error(error instanceof ApiError ? error.message : String(error))
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
  const currentAmount = amount.value
  const currentMethod = method.value
  if (
    paymentLoadState.value !== 'ready' ||
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
    const response = await api.post<EpayQuote>(
      '/api/next/wallet/topup/quote',
      {
        amount: currentAmount,
        amount_unit: 'USD',
        payment_method: currentMethod.replace(/^epay:/, ''),
      },
      { signal: controller.signal }
    )
    if (sequence !== quoteSequence || controller.signal.aborted) return
    if (
      !response ||
      response.amount !== currentAmount ||
      response.amount_unit !== 'USD' ||
      response.payment_method !== currentMethod.replace(/^epay:/, '') ||
      response.currency !== 'CNY' ||
      !isQuota(response.amount_minor) ||
      BigInt(response.amount_minor) <= 0n ||
      !isQuota(response.expected_quota) ||
      BigInt(response.expected_quota) <= 0n ||
      !Number.isSafeInteger(response.min_topup) ||
      response.min_topup < 1 ||
      typeof response.quote_fingerprint !== 'string' ||
      !response.quote_fingerprint ||
      response.pay_amount !==
        `${BigInt(response.amount_minor) / 100n}.${(BigInt(response.amount_minor) % 100n).toString().padStart(2, '0')}`
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

onMounted(async () => {
  try {
    const info = parseTopupInfo(
      await api.get<unknown>('/api/next/wallet/config', undefined, {
        signal: configController.signal,
      })
    )
    if (configController.signal.aborted) return
    paymentMethods.value = info.pay_methods.map((item) => ({
      value: `epay:${item.type}`,
      label: item.name,
      minTopup: Math.max(item.min_topup ?? info.min_topup, info.min_topup),
    }))
    minimumTopup.value = info.min_topup
    const validPresets = info.amount_options.filter(
      (value) => value >= info.min_topup
    )
    presets.value = validPresets.length ? validPresets : [info.min_topup]
    let selectedMethod = paymentMethods.value.find(
      (item) => item.value === method.value
    )
    if (!selectedMethod && paymentMethods.value.length > 0) {
      selectedMethod = paymentMethods.value[0]!
      method.value = selectedMethod.value
    }
    const initialMinimum = selectedMethod?.minTopup ?? info.min_topup
    if (amount.value === null || amount.value < initialMinimum) {
      amount.value =
        presets.value.find((value) => value >= initialMinimum) ?? initialMinimum
    }
    paymentLoadState.value = 'ready'
  } catch {
    if (!configController.signal.aborted) paymentLoadState.value = 'failed'
  }
})

watch(
  [amount, method, paymentLoadState],
  () => {
    quoteChanged.value = false
    void refreshQuote()
  },
  { flush: 'sync' }
)

onUnmounted(() => {
  configController.abort()
  ++quoteSequence
  quoteController?.abort()
})
</script>

<template>
  <ConsoleCard :title="t('wallet.quickTopup')">
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
        class="mt-2 text-xs text-[var(--danger-text)]"
      >
        {{ t('wallet.invalidTopupAmount') }}
      </p>
    </div>

    <div class="mt-4">
      <PaymentMethods v-model="method" :methods="selectableMethods" />
      <p
        v-if="paymentLoadState === 'failed'"
        class="mt-2 text-xs text-[var(--danger-text)]"
      >
        {{ t('wallet.paymentMethodsLoadFailed') }}
      </p>
      <p
        v-else-if="paymentLoadState === 'ready' && !paymentMethods.length"
        class="mt-2 text-xs text-[var(--text-tertiary)]"
      >
        {{ t('wallet.paymentMethodsUnavailable') }}
      </p>
    </div>

    <div class="mt-5 space-y-2 border-t border-[var(--border-subtle)] pt-4">
      <div class="flex items-center justify-between text-sm">
        <span class="text-[var(--text-secondary)]">{{
          t('wallet.summaryPayNow')
        }}</span>
        <span class="font-semibold tabular-nums text-[var(--text-primary)]">
          {{ quote?.pay_amount ? `¥${quote.pay_amount}` : '—' }}
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
      class="mt-3 text-xs text-[var(--danger-text)]"
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
      {{ t('wallet.topupNow') }}
    </ConsoleButton>

    <p class="mt-3 text-xs text-[var(--text-tertiary)]">
      {{ t('wallet.callbackNote') }}
    </p>
  </ConsoleCard>
</template>
