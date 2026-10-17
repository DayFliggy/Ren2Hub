<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import {
  topupApi,
  enabledPayMethods,
  checkoutTarget,
  parseCreemProducts,
  type TopupInfo,
  type CreemProduct,
} from '@/api/topup'
import { nativePaymentMessages } from '@/i18n/nativePayment'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useAppStore } from '@/stores/app'
import { topupInputAmounts, validTopupAmount } from '@/utils/topupAmount'
defineProps<{ balanceQuota?: string | null }>()
const app = useAppStore()
const method = defineModel<string>('paymentMethod', { default: '' }),
  emit = defineEmits<{ done: []; capabilities: [info: TopupInfo] }>()
const { t } = useI18n({ messages: nativePaymentMessages }),
  info = ref<TopupInfo | null>(null),
  amount = ref(''),
  product = ref(''),
  submethod = ref(''),
  due = ref(''),
  error = ref(''),
  loading = ref(false),
  paying = ref(false),
  request = useLatestRequest(),
  infoRequest = useLatestRequest(),
  paymentRequest = useLatestRequest()
const methods = computed(() =>
  info.value ? enabledPayMethods(info.value) : []
)
const products = ref<CreemProduct[]>([])
const tokens = computed(() => app.status.quota_display_type === 'TOKENS')
const methodUnavailable = (value: string) => tokens.value && value === 'stripe'
const amounts = computed(() =>
  info.value ? topupInputAmounts(info.value, method.value, app.status) : null
)
const unitError = computed(() =>
  info.value && method.value !== 'creem' && !amounts.value
    ? t('nativePayment.invalidUnit')
    : ''
)
const quantityError = computed(() =>
  method.value !== 'creem' &&
  amounts.value?.tokens &&
  Number(amount.value) > 0 &&
  Number(amount.value) % amounts.value.step !== 0
    ? t('nativePayment.wholeUnits', { units: amounts.value.step })
    : ''
)
function initialAmount() {
  if (amounts.value)
    amount.value = String(
      Math.max(
        amounts.value.minimum,
        amounts.value.presets[0] ?? amounts.value.minimum
      )
    )
}
watch(
  [() => app.status.quota_display_type, () => app.status.quota_per_unit],
  () => {
    request.cancel()
    paymentRequest.cancel()
    due.value = ''
    paying.value = false
    loading.value = false
    initialAmount()
  }
)
const valid = computed(
  () =>
    methods.value.some((m) => m.type === method.value) &&
    !methodUnavailable(method.value) &&
    (method.value === 'creem'
      ? products.value.some((p) => p.productId === product.value)
      : validTopupAmount(Number(amount.value), amounts.value))
)
watch([method, amount, product, submethod], () => {
  due.value = ''
  request.cancel()
  loading.value = false
})
onMounted(async () => {
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
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  info.value = result.value.value
  products.value = result.value.products
  emit('capabilities', info.value)
  if (!methods.value.some((m) => m.type === method.value))
    method.value =
      methods.value.find((item) => !methodUnavailable(item.type))?.type ??
      methods.value[0]?.type ??
      ''
  initialAmount()
  product.value = products.value[0]?.productId ?? ''
})
async function quote() {
  if (!valid.value) return
  loading.value = true
  error.value = ''
  const result = await request.run((signal) =>
    topupApi.quote(method.value, Number(amount.value), signal)
  )
  if (result.stale) return
  loading.value = false
  if (result.ok) due.value = result.value
  else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
async function pay() {
  if (!valid.value || paying.value) return
  paying.value = true
  error.value = ''
  const selectedMethod = method.value
  const result = await paymentRequest.run((signal) =>
    topupApi.pay(
      selectedMethod,
      method.value === 'creem'
        ? { product_id: product.value }
        : {
            amount: Number(amount.value),
            ...(method.value === 'waffo' && submethod.value !== ''
              ? { pay_method_index: Number(submethod.value) }
              : {}),
            ...(method.value === 'stripe'
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
  paying.value = false
  if (!result.ok) {
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  try {
    const target = checkoutTarget(selectedMethod, result.value)
    emit('done')
    if (target.form) {
      const form = document.createElement('form')
      form.method = 'POST'
      form.action = target.url
      for (const [name, value] of Object.entries(target.form)) {
        const input = document.createElement('input')
        input.type = 'hidden'
        input.name = name
        input.value = value
        form.append(input)
      }
      document.body.append(form)
      form.submit()
      form.remove()
    } else window.location.assign(target.url)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    paying.value = false
  }
}
</script>
<template>
  <ConsoleCard :title="t('nativePayment.title')"
    ><ErrorBanner
      v-if="error || unitError || quantityError"
      :message="error || unitError || quantityError"
    />
    <p v-if="info && !methods.length">{{ t('nativePayment.none') }}</p>
    <div v-else class="space-y-4">
      <label class="block"
        >{{ t('nativePayment.method')
        }}<select
          v-model="method"
          :disabled="paying"
          class="console-input mt-2 w-full"
        >
          <option
            v-for="item in methods"
            :key="item.type"
            :value="item.type"
            :disabled="methodUnavailable(item.type)"
          >
            {{ item.name
            }}{{
              methodUnavailable(item.type)
                ? ` · ${t('nativePayment.unavailable')}`
                : ''
            }}
          </option>
        </select></label
      >
      <p
        v-if="tokens && methods.some((item) => item.type === 'stripe')"
        class="text-sm text-[var(--text-secondary)]"
      >
        {{ t('nativePayment.stripeTokensUnavailable') }}
      </p>
      <label v-if="method === 'creem'" class="block"
        >{{ t('nativePayment.product')
        }}<select
          v-model="product"
          :disabled="paying"
          class="console-input mt-2 w-full"
        >
          <option
            v-for="item in products"
            :key="item.productId"
            :value="item.productId"
          >
            {{ item.name }} · {{ item.price }} {{ item.currency }}
          </option>
        </select></label
      >
      <template v-else
        ><label class="block"
          >{{ t(tokens ? 'nativePayment.tokenAmount' : 'nativePayment.amount')
          }}<input
            v-model="amount"
            :disabled="paying"
            type="number"
            :min="amounts?.minimum ?? 1"
            :step="amounts?.step ?? 1"
            class="console-input mt-2 w-full"
        /></label>
        <p class="text-sm text-[var(--text-secondary)]">
          {{
            tokens && amounts
              ? t('nativePayment.tokenUnit', { units: amounts.scale })
              : t('nativePayment.unit')
          }}
          {{ t('nativePayment.minimum', { amount: amounts?.minimum ?? '—' }) }}
        </p>
        <div class="flex flex-wrap gap-2">
          <ConsoleButton
            v-for="option in amounts?.presets ?? []"
            :key="option"
            :disabled="paying || option < (amounts?.minimum ?? 1)"
            variant="secondary"
            @click="amount = String(option)"
            >{{ option }}</ConsoleButton
          >
        </div>
        <label
          v-if="method === 'waffo' && info?.waffo_pay_methods?.length"
          class="block"
          >{{ t('nativePayment.methodDetail')
          }}<select
            v-model="submethod"
            :disabled="paying"
            class="console-input mt-2 w-full"
          >
            <option value="">{{ t('nativePayment.automatic') }}</option>
            <option
              v-for="(item, index) in info.waffo_pay_methods"
              :key="index"
              :value="String(index)"
            >
              {{ item.name }}
            </option>
          </select></label
        ><ConsoleButton
          variant="secondary"
          :disabled="!valid || loading || paying"
          @click="quote"
          >{{ t('nativePayment.quote') }}</ConsoleButton
        >
        <p v-if="due">{{ t('nativePayment.due') }}: {{ due }}</p></template
      >
      <ConsoleButton
        :disabled="!method || !valid || paying"
        :loading="paying"
        @click="pay"
        >{{ t('nativePayment.pay') }}</ConsoleButton
      >
    </div></ConsoleCard
  >
</template>
