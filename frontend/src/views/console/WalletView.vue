<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import { ApiError } from '@/api/types'
import { api } from '@/api/console'
import { parseTopUpDetail } from '@/api/adminOrders'
import { rememberedGmpayOrder, type TopupInfo } from '@/api/topup'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import PageHero from '@/components/console/PageHero.vue'
import RedeemPanel from '@/components/console/wallet/RedeemPanel.vue'
import TopupPanel from '@/components/console/wallet/TopupPanel.vue'
import TopupRecords from '@/components/console/wallet/TopupRecords.vue'
import { useBalanceVisibility } from '@/composables/useDashboard'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { formatQuotaParts } from '@/utils/format'

const { t } = useI18n()
const route = useRoute()
const toast = useToast()
const auth = useAuthStore()
const { hidden, toggle } = useBalanceVisibility()

const refreshKey = ref(0)
const paymentMethod = ref('epay')
const capabilities = ref<TopupInfo | null>(null)
const capabilitiesState = ref<'idle' | 'ready' | 'failed'>('idle')
const redemptionEnabled = computed(
  () =>
    capabilitiesState.value === 'ready' && capabilities.value?.enable_redemption
)
const trackedTradeNo = ref('')
const trackedPayment = ref<ReturnType<typeof parseTopUpDetail> | null>(null)
const paymentStatusError = ref('')
const paymentStatusLoading = ref(false)
let statusController: AbortController | null = null

const activePanel = computed<'topup' | 'redeem'>(() =>
  route.query.panel === 'redeem' ? 'redeem' : 'topup'
)

const balanceQuota = computed(() =>
  auth.user?.quota == null ? null : String(auth.user.quota)
)
const balance = computed(() =>
  balanceQuota.value === null ? null : formatQuotaParts(balanceQuota.value, 2)
)

async function handlePaymentDone(): Promise<void> {
  refreshKey.value += 1
  try {
    await auth.fetchSelf()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t('common.failed'))
  }
}

async function trackPayment(tradeNo: string): Promise<void> {
  if (!tradeNo || tradeNo.length > 128) return
  trackedTradeNo.value = tradeNo
  statusController?.abort()
  const controller = new AbortController()
  statusController = controller
  paymentStatusLoading.value = true
  paymentStatusError.value = ''
  try {
    const response = await api.get<unknown>(
      `/api/user/topup/detail/${encodeURIComponent(tradeNo)}`,
      undefined,
      { signal: controller.signal }
    )
    if (controller.signal.aborted) return
    trackedPayment.value = parseTopUpDetail(response, tradeNo)
    if (trackedPayment.value.state.status === 'completed')
      await handlePaymentDone()
  } catch (error) {
    if (!controller.signal.aborted)
      paymentStatusError.value =
        error instanceof ApiError
          ? error.message
          : t('wallet.paymentStatusUnavailable')
  } finally {
    if (!controller.signal.aborted) paymentStatusLoading.value = false
  }
}
onMounted(() => {
  const returned =
    route.query.trade_no ?? route.query.out_trade_no ?? route.query.order_id
  const tradeNo =
    typeof returned === 'string'
      ? returned
      : auth.user
        ? rememberedGmpayOrder(auth.user.id)
        : ''
  if (tradeNo) void trackPayment(tradeNo)
})
onBeforeUnmount(() => statusController?.abort())
</script>

<template>
  <div class="mx-auto max-w-[var(--console-form-width)]">
    <PageHero
      :title="t('wallet.title')"
      :title-accent="t('wallet.titleAccent')"
      :crumbs="[t('wallet.breadcrumb.0'), t('wallet.breadcrumb.1')]"
    >
      <template #actions>
        <div class="flex items-center gap-3">
          <p class="font-mono tracking-tight text-[var(--text-primary)]">
            <span
              v-if="balance?.prefix"
              class="text-lg text-[var(--text-tertiary)]"
              >{{ balance.prefix }}</span
            >
            <span class="display-number text-5xl">
              {{ hidden ? '••••••' : balance ? balance.whole : '—' }}
            </span>
            <span
              v-if="!hidden && balance?.fraction"
              class="display-number text-xl text-[var(--text-secondary)]"
            >
              .{{ balance.fraction }}
            </span>
          </p>
          <button
            type="button"
            class="inline-flex h-11 w-11 sm:h-9 sm:w-9 items-center justify-center rounded-lg text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
            :aria-label="
              hidden ? t('wallet.showBalance') : t('wallet.hideBalance')
            "
            @click="toggle"
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"
            >
              <path
                v-if="hidden"
                d="M3 3l18 18M10.5 10.7a3 3 0 0 0 4.2 4.2M7.4 7.6C4.8 9.3 3 12 3 12s3.5 6 9 6c1.6 0 3-.4 4.3-1M12 6c5.5 0 9 6 9 6s-.6 1.1-1.8 2.3"
              />
              <template v-else>
                <path d="M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6Z" />
                <circle cx="12" cy="12" r="3" />
              </template>
            </svg>
          </button>
        </div>
      </template>
    </PageHero>

    <ConsoleCard
      v-if="trackedTradeNo"
      class="mb-6"
      :title="t('wallet.paymentStatus')"
    >
      <p class="break-all font-mono text-xs text-[var(--text-secondary)]">
        {{ trackedTradeNo }}
      </p>
      <p
        v-if="paymentStatusError"
        role="alert"
        class="mt-2 text-sm text-[var(--status-danger-text)]"
      >
        {{ paymentStatusError }}
      </p>
      <p
        v-else-if="trackedPayment"
        role="status"
        class="mt-2 text-sm text-[var(--text-primary)]"
      >
        {{ t(`orders.status.${trackedPayment.state.status}`) }}
        <span v-if="trackedPayment.evidence">
          · {{ t(`orders.gmpay.${trackedPayment.evidence.status}`) }}</span
        >
      </p>
      <p
        v-if="trackedPayment?.state.status === 'pending'"
        class="mt-2 text-xs text-[var(--text-secondary)]"
      >
        {{ t('wallet.creationUncertain') }}
      </p>
      <ConsoleButton
        class="mt-3"
        size="sm"
        variant="secondary"
        :loading="paymentStatusLoading"
        @click="trackPayment(trackedTradeNo)"
      >
        {{ t('orders.refresh') }}
      </ConsoleButton>
    </ConsoleCard>

    <div class="sibling-space-y-6">
      <TopupPanel
        v-model:payment-method="paymentMethod"
        :balance-quota="balanceQuota"
        @done="handlePaymentDone"
        @track-order="trackPayment"
        @capabilities="capabilities = $event"
        @capabilities-state="capabilitiesState = $event"
      />
      <RedeemPanel
        v-if="redemptionEnabled"
        :auto-focus="activePanel === 'redeem'"
        :refresh-key="refreshKey"
        @done="refreshKey += 1"
      />
      <TopupRecords :refresh-key="refreshKey" />
    </div>
  </div>
</template>
