<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import { ApiError } from '@/api/types'
import { api } from '@/api/console'
import { getUsageSummary } from '@/api/dashboard'
import { parseTopUpDetail } from '@/api/adminOrders'
import { rememberedGmpayOrder, type TopupInfo } from '@/api/topup'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import PageHero from '@/components/console/PageHero.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import FlowChart from '@/components/console/wallet/FlowChart.vue'
import RedeemPanel from '@/components/console/wallet/RedeemPanel.vue'
import TopupPanel from '@/components/console/wallet/TopupPanel.vue'
import TopupRecords from '@/components/console/wallet/TopupRecords.vue'
import type { DashboardStats, FlowPoint } from '@/composables/useDashboard'
import { useBalanceVisibility } from '@/composables/useDashboard'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { quotaToDecimal, sumQuota } from '@/utils/quota'
import { formatNumber, formatQuota } from '@/utils/format'

type WalletStats = DashboardStats

const { t } = useI18n()
const route = useRoute()
const toast = useToast()
const auth = useAuthStore()
const { hidden, toggle } = useBalanceVisibility()

const stats = ref<WalletStats | null>(null)
const flow = ref<FlowPoint[]>([])
const todayConsume = ref('0')
const refreshKey = ref(0)
const paymentMethod = ref('epay')
const capabilities = ref<TopupInfo | null>(null)
const trackedTradeNo = ref('')
const trackedPayment = ref<ReturnType<typeof parseTopUpDetail> | null>(null)
const paymentStatusError = ref('')
const paymentStatusLoading = ref(false)
let statusController: AbortController | null = null
const loading = ref(false)
const statsRequest = useLatestRequest()

const activePanel = computed<'topup' | 'redeem'>(() =>
  route.query.panel === 'redeem' ? 'redeem' : 'topup'
)

const balanceParts = computed(() =>
  quotaToDecimal(stats.value?.quota ?? '0', 2).split('.')
)
const balanceDollars = computed(() => balanceParts.value[0])
const balanceCents = computed(() => balanceParts.value[1])
const dayCount = computed(() => flow.value.length || 30)

const statCards = computed(() => {
  const current = stats.value
  if (!current) return []
  const average = flow.value.length
    ? (
        BigInt(sumQuota(flow.value.map((point) => point.consume))) /
        BigInt(flow.value.length)
      ).toString()
    : '0'
  return [
    {
      label: t('wallet.statMonthConsume'),
      value: formatQuota(current.used_quota),
      sub: t('wallet.statMonthConsumeSub'),
    },
    {
      label: t('wallet.statDailyAvg'),
      value: formatQuota(average),
      sub: t('wallet.statDailyAvgSub', { days: dayCount.value }),
    },
    {
      label: t('wallet.statTodayConsume'),
      value: formatQuota(todayConsume.value),
      sub: t('wallet.statTodayConsumeSub'),
    },
    {
      label: t('wallet.statMonthRequests'),
      value: formatNumber(current.total_requests),
      sub: t('wallet.statMonthRequestsSub'),
    },
  ]
})

async function loadStats(): Promise<void> {
  loading.value = true
  const now = new Date()
  const date = (day: number) =>
    `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
  const result = await statsRequest.run((signal) =>
    Promise.all([
      getUsageSummary(
        { range: 'custom', start: date(1), end: date(now.getDate()) },
        signal
      ),
      getUsageSummary({ range: 'today' }, signal),
    ])
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    toast.error(
      result.error instanceof ApiError
        ? result.error.message
        : String(result.error)
    )
    return
  }
  const [month, today] = result.value
  if (!month.meta.available || !today.meta.available) {
    stats.value = null
    toast.error(t('dashboard.stats.unavailable'))
    return
  }
  todayConsume.value = today.kpi.totalQuota
  flow.value = month.flow
  stats.value = {
    quota: String(auth.user?.quota ?? 0),
    used_quota: month.kpi.totalQuota,
    today_quota: today.kpi.totalQuota,
    month_quota: month.kpi.totalQuota,
    today_requests: today.kpi.totalRequests,
    total_requests: month.kpi.totalRequests,
    month_quota_delta: month.comparison?.quotaDelta ?? null,
    month_requests_delta: month.comparison?.requestsDelta ?? null,
  }
}

async function handlePaymentDone(): Promise<void> {
  refreshKey.value += 1
  try {
    await auth.fetchSelf()
    await loadStats()
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
  void loadStats()
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
  <div>
    <PageHero
      :title="t('wallet.title')"
      :title-accent="t('wallet.titleAccent')"
      :crumbs="[t('wallet.breadcrumb.0'), t('wallet.breadcrumb.1')]"
    >
      <template #actions>
        <div class="flex items-center gap-3" :aria-busy="loading">
          <p class="font-mono tracking-tight text-[var(--text-primary)]">
            <span class="text-lg text-[var(--text-tertiary)]">$</span>
            <span class="display-number text-5xl">
              {{ hidden ? '••••••' : stats ? balanceDollars : '—' }}
            </span>
            <span
              v-if="!hidden && stats"
              class="display-number text-xl text-[var(--text-secondary)]"
            >
              .{{ balanceCents }}
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

    <StatTileGrid class="mb-6" :tiles="statCards" :columns="4" />

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
        >{{ t('orders.refresh') }}</ConsoleButton
      >
    </ConsoleCard>

    <div class="mb-6 grid gap-6 xl:grid-cols-[1fr_23.75rem]">
      <TopupPanel
        v-model:payment-method="paymentMethod"
        :balance-quota="stats?.quota ?? null"
        @done="handlePaymentDone"
        @track-order="trackPayment"
        @capabilities="capabilities = $event"
      />
      <RedeemPanel
        v-if="capabilities?.enable_redemption"
        :auto-focus="activePanel === 'redeem'"
        :refresh-key="refreshKey"
        @done="handlePaymentDone"
      />
    </div>

    <div class="sibling-space-y-6">
      <FlowChart :flow="flow" />
      <TopupRecords :refresh-key="refreshKey" />
    </div>
  </div>
</template>
