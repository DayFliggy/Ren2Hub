<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  checkoutTarget,
  enabledPayMethods,
  parseCreemProducts,
  topupApi,
  type CreemProduct,
  type TopupInfo,
} from '@/api/topup'
import { invalidResponse } from '@/api/contracts'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FormField from '@/components/common/FormField.vue'
import PaymentMethods from '@/components/console/wallet/PaymentMethods.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { paymentRestorationMessages } from '@/i18n/paymentRestoration'
import { useAppStore } from '@/stores/app'
import { topupInputAmounts, validTopupAmount } from '@/utils/topupAmount'

defineProps<{ balanceQuota?: string | null }>()
const method = defineModel<string>('paymentMethod', { default: '' })
const emit = defineEmits<{ done: []; capabilities: [info: TopupInfo] }>()
const { t } = useI18n({ messages: paymentRestorationMessages })
const app = useAppStore()
const info = ref<TopupInfo | null>(null)
const amountInput = ref('')
const amount = computed(() =>
  /^[0-9]+$/.test(amountInput.value) ? Number(amountInput.value) : null
)
const product = ref('')
const submethod = ref('')
const due = ref('')
const infoError = ref('')
const quoteError = ref('')
const paymentError = ref('')
const quoteLoading = ref(false)
const submittingTopup = ref(false)
const quoteRequest = useLatestRequest()
const infoRequest = useLatestRequest()
const paymentRequest = useLatestRequest()
const products = ref<CreemProduct[]>([])
const methods = computed(() =>
  info.value ? enabledPayMethods(info.value) : []
)
const tokens = computed(() => app.status.quota_display_type === 'TOKENS')
const methodUnavailable = (value: string) => tokens.value && value === 'stripe'
const selectableMethods = computed(() =>
  methods.value
    .filter((item) => !methodUnavailable(item.type))
    .map((item) => ({ value: item.type, label: item.name }))
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
const isCrypto = computed(() => method.value.startsWith('gmpay:'))
const selectedProduct = computed(() =>
  products.value.find((item) => item.productId === product.value)
)
const submitLabel = computed(() =>
  isCrypto.value ? t('wallet.gmpaySubmit') : t('wallet.topupNow')
)
const unitError = computed(() =>
  info.value && method.value !== 'creem' && !amounts.value
    ? t('nativePayment.invalidUnit')
    : ''
)
const invalidAmount = computed(
  () =>
    method.value !== 'creem' &&
    amountInput.value !== '' &&
    !validTopupAmount(amount.value ?? 0, amounts.value)
)
const quantityError = computed(() =>
  amounts.value?.tokens &&
  amount.value !== null &&
  amount.value > 0 &&
  amount.value % amounts.value.step !== 0
    ? t('nativePayment.wholeUnits', { units: amounts.value.step })
    : ''
)
const valid = computed(
  () =>
    methods.value.some((item) => item.type === method.value) &&
    !methodUnavailable(method.value) &&
    (method.value === 'creem'
      ? !!selectedProduct.value
      : validTopupAmount(amount.value ?? 0, amounts.value))
)
const canSubmit = computed(
  () =>
    valid.value &&
    (method.value === 'creem' || !!due.value) &&
    !quoteLoading.value
)
const failure = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

function initialAmount() {
  amountInput.value = amounts.value
    ? String(
        Math.max(
          amounts.value.minimum,
          presets.value[0] ?? amounts.value.minimum
        )
      )
    : ''
}
async function loadPaymentConfig() {
  infoError.value = ''
  const result = await infoRequest.run(async (signal) => {
    const value = await topupApi.info(signal)
    return {
      value,
      products: value.enable_creem_topup
        ? parseCreemProducts(value.creem_products)
        : [],
    }
  })
  if (result.stale) return
  if (!result.ok) {
    infoError.value = failure(result.error)
    return
  }
  info.value = result.value.value
  products.value = result.value.products
  emit('capabilities', info.value)
  if (
    !methods.value.some(
      (item) => item.type === method.value && !methodUnavailable(item.type)
    )
  )
    method.value = selectableMethods.value[0]?.value ?? ''
  product.value = products.value[0]?.productId ?? ''
  initialAmount()
}
async function refreshQuote() {
  due.value = ''
  quoteError.value = ''
  quoteLoading.value = false
  quoteRequest.cancel()
  if (!valid.value || method.value === 'creem') return
  quoteLoading.value = true
  const result = await quoteRequest.run(async (signal) => {
    const value = await topupApi.quote(method.value, amount.value!, signal)
    if (typeof value !== 'string' || !/^\d+(?:\.\d+)?$/.test(value))
      invalidResponse('/api/user/amount')
    return value
  })
  if (result.stale) return
  quoteLoading.value = false
  if (result.ok) due.value = result.value
  else quoteError.value = failure(result.error)
}
async function topup() {
  if (!canSubmit.value || submittingTopup.value) return
  submittingTopup.value = true
  paymentError.value = ''
  const selectedMethod = method.value
  const result = await paymentRequest.run((signal) =>
    topupApi.pay(
      selectedMethod,
      selectedMethod === 'creem'
        ? { product_id: product.value }
        : {
            amount: amount.value!,
            ...(selectedMethod === 'waffo' && submethod.value !== ''
              ? { pay_method_index: Number(submethod.value) }
              : {}),
            ...(selectedMethod === 'stripe'
              ? {
                  success_url: `${window.location.origin}/wallet`,
                  cancel_url: `${window.location.origin}/wallet`,
                }
              : {}),
          },
      signal
    )
  )
  if (result.stale) return
  if (!result.ok) {
    submittingTopup.value = false
    paymentError.value = failure(result.error)
    return
  }
  try {
    const target = checkoutTarget(selectedMethod, result.value)
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
    emit('done')
  } catch (error) {
    paymentError.value = failure(error)
  } finally {
    submittingTopup.value = false
  }
}
watch([method, amountInput, product, submethod], () => {
  paymentRequest.cancel()
  submittingTopup.value = false
  paymentError.value = ''
  void refreshQuote()
})
watch(
  [() => app.status.quota_display_type, () => app.status.quota_per_unit],
  () => {
    paymentRequest.cancel()
    submittingTopup.value = false
    paymentError.value = ''
    if (methodUnavailable(method.value))
      method.value = selectableMethods.value[0]?.value ?? ''
    initialAmount()
    void refreshQuote()
  }
)
onMounted(loadPaymentConfig)
</script>

<template>
  <ConsoleCard :title="t('wallet.quickTopup')">
    <ErrorBanner
      v-if="infoError"
      :message="infoError"
      @retry="loadPaymentConfig"
    />
    <p
      v-if="info && !methods.length"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('nativePayment.none') }}
    </p>
    <fieldset :disabled="submittingTopup || !info" class="min-w-0 border-0 p-0">
      <template v-if="method !== 'creem'">
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
            @click="amountInput = String(preset)"
          >
            {{ preset }}
          </button>
        </div>
        <div class="mt-4">
          <div class="pencil-underline relative" data-handdrawn="underline">
            <input
              v-model="amountInput"
              type="text"
              inputmode="numeric"
              pattern="[0-9]*"
              autocomplete="off"
              :placeholder="t('wallet.amountPlaceholder')"
              :aria-label="
                t(tokens ? 'nativePayment.tokenAmount' : 'nativePayment.amount')
              "
              :aria-invalid="!!(unitError || quantityError || invalidAmount)"
              class="h-11 w-full border-0 border-b-[1.5px] bg-transparent px-4 font-mono text-[0.9375rem] font-semibold leading-[1.375rem] text-[var(--text-primary)] placeholder:font-normal placeholder:font-sans placeholder:text-[var(--text-tertiary)] transition-[border-color,background-color,box-shadow] focus:outline-hidden focus:border-[var(--accent)]"
              style="border-color: var(--border-default)"
            />
          </div>
          <p class="mt-2 text-xs leading-5 text-[var(--text-tertiary)]">
            {{
              tokens && amounts
                ? t('nativePayment.tokenUnit', { units: amounts.scale })
                : t('nativePayment.unit')
            }}
            {{
              t('nativePayment.minimum', { amount: amounts?.minimum ?? '—' })
            }}
          </p>
          <p
            v-if="unitError || quantityError || invalidAmount"
            role="alert"
            class="mt-2 text-xs text-[var(--status-danger-text)]"
          >
            {{ unitError || quantityError || t('wallet.invalidTopupAmount') }}
          </p>
        </div>
      </template>
      <div class="mt-4">
        <PaymentMethods v-model="method" :methods="selectableMethods" />
        <p
          v-if="tokens && methods.some((item) => item.type === 'stripe')"
          class="mt-2 text-xs leading-5 text-[var(--text-tertiary)]"
        >
          {{ t('nativePayment.stripeTokensUnavailable') }}
        </p>
      </div>
      <FormField
        v-if="method === 'creem'"
        :label="t('nativePayment.product')"
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
        :label="t('nativePayment.methodDetail')"
        class="mt-4"
      >
        <select v-model="submethod" class="console-input w-full">
          <option value="">{{ t('nativePayment.automatic') }}</option>
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
            t('nativePayment.due')
          }}</span>
          <span class="font-semibold tabular-nums text-[var(--text-primary)]">{{
            method === 'creem' && selectedProduct
              ? `${selectedProduct.price} ${selectedProduct.currency}`
              : due || '—'
          }}</span>
        </div>
        <div class="flex items-center justify-between text-sm">
          <span class="text-[var(--text-secondary)]">{{
            t('paymentRestoration.credit')
          }}</span
          ><span class="font-semibold tabular-nums text-[var(--text-primary)]"
            >—</span
          >
        </div>
        <div class="flex items-center justify-between gap-3 text-sm">
          <span class="text-[var(--text-secondary)]">{{
            t('wallet.summaryBalanceAfter')
          }}</span
          ><span class="font-semibold tabular-nums text-[var(--accent-text)]"
            >—</span
          >
        </div>
        <p class="text-xs leading-5 text-[var(--text-tertiary)]">
          {{ t('paymentRestoration.currencyNote') }}
          {{ t('paymentRestoration.creditNote') }}
        </p>
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
          >{{ t('wallet.retryQuote') }}</ConsoleButton
        >
      </div>
      <p
        v-if="paymentError"
        role="alert"
        class="mt-3 text-sm text-[var(--status-danger-text)]"
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
        >{{ submitLabel }}</ConsoleButton
      >
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
