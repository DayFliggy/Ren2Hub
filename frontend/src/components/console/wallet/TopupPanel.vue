<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  bonusChangedFrom,
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
import { topupBonusApi } from '@/api/topupBonus'
import type { TopupBonusQuote } from '@/types/topupBonus'
import { ApiError } from '@/api/types'
import AmountInput from '@/components/common/AmountInput.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import BackupPayments from '@/components/console/wallet/BackupPayments.vue'
import PaymentMethods from '@/components/console/wallet/PaymentMethods.vue'
import { choiceClass } from '@/components/console/wallet/choiceStyle'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { nativePaymentMessages } from '@/i18n/nativePayment'
import { rateToPercent } from '@/features/topup-bonus/amount'
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
  paymentMethods.value.map((item) => ({
    value: item.type,
    label: item.name,
    icon: item.icon,
  }))
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
// bonus is the server's bonus for this credit and is confirmed back on pay.
// Locally priced providers answer it inside their quote; Stripe and Creem
// prices come from the provider, so their bonus comes from the preview.
const quote = ref<{
  pay_amount: string
  currency: string
  principal_quota: string | null
  bonus: TopupBonusQuote | null
  localPrice: boolean
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
const creditedQuota = computed(
  () => quote.value?.bonus?.expected_quota ?? quote.value?.principal_quota
)
const bonus = computed(() => {
  const value = quote.value?.bonus
  if (!value || BigInt(value.bonus_quota) <= 0n) return null
  return {
    quota: value.bonus_quota,
    rate: rateToPercent(value.applied_rate),
  }
})
function serverQuote(value: TopupQuote) {
  return {
    pay_amount: value.pay_amount,
    currency: value.currency,
    principal_quota: value.principal_quota,
    bonus: value as TopupBonusQuote,
    localPrice: true,
  }
}
let quoteSequence = 0
let quoteController: AbortController | null = null
let configController: AbortController | null = null
let disposed = false
// Paying must not push the balance past what the wallet can hold.
const balanceOverflow = computed(() => {
  if (!creditedQuota.value || props.balanceQuota == null) return false
  try {
    const total = addQuota(props.balanceQuota, creditedQuota.value)
    return BigInt(total) > BigInt(Number.MAX_SAFE_INTEGER)
  } catch {
    return true
  }
})
const canSubmit = computed(
  () =>
    configState.value === 'ready' &&
    !paymentBlocked.value &&
    !balanceOverflow.value &&
    paymentMethods.value.some((item) => item.type === method.value) &&
    (method.value === 'creem'
      ? !!selectedProduct.value
      : amount.value !== null && !invalidAmount.value) &&
    !!quote.value?.bonus &&
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
  const confirmed = quote.value!
  // The server rejects any other bonus before an order or session exists.
  const expectedBonus = {
    expected_bonus_quota: Number(confirmed.bonus!.bonus_quota),
  }
  const sequence = quoteSequence
  try {
    const response = await topupApi.pay(
      selected,
      selected === 'creem'
        ? { product_id: product.value, ...expectedBonus }
        : {
            amount: amount.value!,
            ...expectedBonus,
            ...(confirmed.localPrice
              ? {
                  expected_pay_amount: confirmed.pay_amount,
                  expected_currency: confirmed.currency,
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
    // No order exists yet: show the new quote and wait for another click.
    const changed = quoteChangedFrom(error)
    const changedBonus = bonusChangedFrom(error)
    if (changed || changedBonus) {
      // A newer quote for changed input wins over this rejection.
      if (!disposed && sequence === quoteSequence) {
        quote.value = changed
          ? serverQuote(changed)
          : {
              ...confirmed,
              principal_quota: changedBonus!.principal_quota,
              bonus: changedBonus,
            }
        quoteChanged.value = true
        void loadPresetRates()
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
  if (!paymentMethods.value.some((item) => item.type === method.value)) return
  if (
    method.value !== 'creem' &&
    (amount.value === null || invalidAmount.value)
  )
    return
  if (method.value === 'creem' && !selectedProduct.value) return
  const controller = new AbortController()
  quoteController = controller
  quoteLoading.value = true
  try {
    const next = await providerQuote(controller.signal)
    if (sequence !== quoteSequence || controller.signal.aborted) return
    quote.value = next
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
/**
 * The quota credited for an input amount at quota_per_unit, as Stripe credits
 * it; token amounts are already quota.
 */
function principalOf(value: number): string | null {
  const scale = app.status.quota_per_unit
  if (typeof scale !== 'number' || !Number.isSafeInteger(scale) || scale <= 0)
    return null
  const credited = tokens.value ? value : value * scale
  return Number.isSafeInteger(credited) && credited > 0
    ? String(credited)
    : null
}
// Each preset's bonus rate from the server preview. Decoration only: a preset
// without one shows no rate and paying never waits for it.
const presetRates = ref<Record<number, string>>({})
let presetController: AbortController | null = null
async function previewRate(preset: number, signal: AbortSignal) {
  const principal = principalOf(preset)
  if (!principal) return null
  const value = await topupBonusApi.preview(principal, signal)
  return BigInt(value.bonus_quota) > 0n
    ? rateToPercent(value.applied_rate)
    : null
}
async function loadPresetRates(): Promise<void> {
  presetController?.abort()
  presetRates.value = {}
  const list = presets.value
  if (method.value === 'creem' || !list.length) return
  const controller = new AbortController()
  presetController = controller
  const results = await Promise.allSettled(
    list.map((preset) => previewRate(preset, controller.signal))
  )
  if (controller.signal.aborted) return
  const next: Record<number, string> = {}
  results.forEach((result, index) => {
    if (result.status === 'fulfilled' && result.value)
      next[list[index]!] = result.value
  })
  presetRates.value = next
}
// The selected preset follows the confirmed quote, so card and receipt agree.
function presetRate(preset: number): string | undefined {
  if (amount.value === preset && quote.value?.bonus) return bonus.value?.rate
  return presetRates.value[preset]
}
const presetCards = computed(() =>
  presets.value.map((value) => ({ value, rate: presetRate(value) }))
)
// Up to six presets fill one row; more split into two.
const presetColumns = computed(() => {
  const count = presets.value.length
  return count <= 6 ? Math.max(3, count) : Math.ceil(count / 2)
})
watch(
  () =>
    [
      method.value === 'creem',
      presets.value.join(','),
      app.status.quota_per_unit,
      tokens.value,
    ].join('|'),
  () => void loadPresetRates()
)
// The quote of the current input. Stripe answers only the payable amount and
// Creem prices its products, so their principal is known here and their bonus
// comes from the preview.
async function providerQuote(signal: AbortSignal) {
  const item = selectedProduct.value
  if (method.value === 'creem' && item) {
    const principal = String(item.quota)
    return {
      pay_amount: String(item.price),
      currency: item.currency,
      principal_quota: principal,
      bonus: await topupBonusApi.preview(principal, signal),
      localPrice: false,
    }
  }
  const result = await topupApi.quote(method.value, amount.value!, signal)
  if (result.quote) return serverQuote(result.quote)
  const principal = principalOf(amount.value!)
  return {
    pay_amount: result.pay_amount,
    currency: '',
    principal_quota: principal,
    bonus: principal ? await topupBonusApi.preview(principal, signal) : null,
    localPrice: false,
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
  presetController?.abort()
})
</script>

<template>
  <!-- The method sets the amount limits, so it comes first in DOM and
       single-column order; at xl it moves to the right of the amount card. -->
  <div
    class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_23.75rem]"
    :aria-busy="configState === 'idle'"
  >
    <ConsoleCard
      class="xl:col-start-2 xl:row-start-1"
      :title="t('wallet.payMethodLabel')"
      stretch
    >
      <p
        v-if="configState === 'idle'"
        role="status"
        class="text-xs text-[var(--text-tertiary)]"
      >
        {{ t('common.loading') }}
      </p>
      <fieldset
        v-else
        :disabled="submittingTopup"
        class="flex min-w-0 grow flex-col border-0 p-0"
      >
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
          v-else-if="!paymentMethods.length"
          class="mt-2 text-xs text-[var(--text-tertiary)]"
        >
          {{ paymentT('nativePayment.none') }}
        </p>
        <div
          v-if="method === 'waffo' && info?.waffo_pay_methods?.length"
          class="mt-4"
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
        <p
          v-if="isCrypto"
          class="mt-4 rounded-xl bg-[var(--surface-muted)] p-3 text-xs text-[var(--text-secondary)]"
        >
          {{ t('wallet.gmpayNetworkGuidance') }}
        </p>
        <!-- With online methods the backup list sits at the bottom; without
             them it follows the notice directly. -->
        <div
          v-if="info?.backup_payment_links.length"
          :class="paymentMethods.length ? 'mt-auto pt-6' : 'mt-4'"
        >
          <BackupPayments :links="info.backup_payment_links" />
        </div>
      </fieldset>
    </ConsoleCard>

    <ConsoleCard
      class="xl:col-start-1 xl:row-start-1"
      :title="t('wallet.quickTopup')"
      stretch
    >
      <p
        v-if="configState === 'idle'"
        role="status"
        class="text-xs text-[var(--text-tertiary)]"
      >
        {{ t('common.loading') }}
      </p>
      <fieldset
        v-else
        :disabled="submittingTopup"
        class="flex min-w-0 grow flex-col border-0 p-0"
      >
        <div v-if="method === 'creem' && products.length">
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
        <template v-else-if="method !== 'creem'">
          <div
            v-if="presets.length"
            role="group"
            :aria-label="t('wallet.presetAmounts')"
            class="grid grid-cols-3 gap-2 sm:grid-cols-[repeat(var(--preset-columns),minmax(0,1fr))]"
            :style="{ '--preset-columns': presetColumns }"
          >
            <button
              v-for="card in presetCards"
              :key="card.value"
              type="button"
              class="flex h-14 min-w-0 flex-col items-center justify-center rounded-xl border px-1 transition-colors focus-ring"
              :class="choiceClass(amount === card.value)"
              :aria-pressed="amount === card.value"
              @click="amount = card.value"
            >
              <!-- Token amounts run to ten digits, so they are smaller; amounts
                   are never cut off. -->
              <span
                class="display-number max-w-full leading-6 [overflow-wrap:anywhere]"
                :class="tokens ? 'text-sm' : 'text-lg'"
              >
                {{ tokens ? '' : '$' }}{{ card.value }}
              </span>
              <span
                v-if="card.rate"
                class="text-xs font-medium leading-4 tabular-nums text-[var(--status-success-text)]"
              >
                <span aria-hidden="true">{{
                  t('wallet.presetBonus', { rate: card.rate })
                }}</span>
                <span class="sr-only">{{
                  t('wallet.summaryBonus', { rate: card.rate })
                }}</span>
              </span>
            </button>
          </div>

          <div :class="{ 'mt-5': presets.length }">
            <div class="mb-1 flex items-baseline justify-between gap-2">
              <label
                for="topup-amount"
                class="text-xs font-medium text-[var(--text-secondary)]"
                >{{ t('wallet.customAmount') }}</label
              >
              <span class="text-xs tabular-nums text-[var(--text-tertiary)]">{{
                t('wallet.minimumAmount', {
                  amount: `${tokens ? '' : '$'}${selectedMinimumTopup}`,
                })
              }}</span>
            </div>
            <AmountInput
              id="topup-amount"
              v-model="amount"
              :placeholder="t('wallet.amountPlaceholder')"
              :min="selectedMinimumTopup"
              :prefix="tokens ? '' : '$'"
              integer
            />
            <p v-if="tokens" class="mt-2 text-xs text-[var(--text-tertiary)]">
              {{
                t('wallet.nativeTokenUnit', { units: amounts?.scale ?? '—' })
              }}
            </p>
            <p
              v-if="invalidAmount || balanceOverflow"
              role="alert"
              class="mt-2 text-xs text-[var(--status-danger-text)]"
            >
              {{ t('wallet.invalidTopupAmount') }}
            </p>
          </div>
        </template>

        <!-- The receipt and pay button sit at the bottom when the method card is taller. -->
        <div class="mt-auto pt-6">
          <div
            class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-4"
          >
            <!-- Paid and credited use different units: side by side, never summed. -->
            <dl class="grid gap-3 sm:grid-cols-2 sm:gap-4">
              <div
                class="flex min-w-0 items-baseline justify-between gap-3 sm:block"
              >
                <dt class="shrink-0 text-xs text-[var(--text-secondary)]">
                  {{ t('wallet.summaryPayNow') }}
                </dt>
                <dd
                  class="display-number text-xl [overflow-wrap:anywhere] text-[var(--text-primary)] sm:mt-1 sm:text-2xl"
                >
                  {{
                    quote?.pay_amount
                      ? `${quoteSymbol}${quote.pay_amount}`
                      : '—'
                  }}
                </dd>
              </div>
              <div
                class="flex min-w-0 items-baseline justify-between gap-3 border-t border-[var(--border-subtle)] pt-3 sm:block sm:border-l sm:border-t-0 sm:pl-4 sm:pt-0"
              >
                <dt class="shrink-0 text-xs text-[var(--text-secondary)]">
                  {{ t('wallet.summaryCredit') }}
                </dt>
                <!-- The rate is on the preset card; here only the bonus amount. -->
                <dd class="min-w-0 text-right sm:mt-1 sm:text-left">
                  <span
                    class="display-number block text-xl [overflow-wrap:anywhere] text-[var(--accent-text)] sm:text-2xl"
                  >
                    {{ creditedQuota ? formatQuota(creditedQuota, 3) : '—' }}
                  </span>
                  <span
                    v-if="bonus"
                    class="mt-0.5 block text-xs font-medium tabular-nums text-[var(--status-success-text)]"
                  >
                    {{
                      t('wallet.creditBonus', {
                        amount: formatQuota(bonus.quota, 3),
                      })
                    }}
                  </span>
                </dd>
              </div>
            </dl>
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
        </div>
      </fieldset>
    </ConsoleCard>
  </div>
</template>
