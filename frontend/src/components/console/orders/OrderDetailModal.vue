<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import { parseTopUpDetail } from '@/api/adminOrders'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { nativePaymentMessages } from '@/i18n/nativePayment'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  adminOrderMethodLabelKey,
  adminOrderPaymentRailLabelKey,
  adminOrderStatusLabelKey,
  adminOrderStatusTone,
  adminOrderTypeLabelKey,
  formatAdminOrderAmount,
  isAdminOrderPaymentRail,
} from '@/constants/adminOrders'
import type { AdminOrder } from '@/types/console'
import { formatQuota, formatTime } from '@/utils/format'

const props = defineProps<{
  order: AdminOrder | null
}>()

const emit = defineEmits<{
  close: []
  completed: []
}>()

const { t } = useI18n()
const { t: paymentT } = useI18n({
  useScope: 'local',
  messages: nativePaymentMessages,
})
const auth = useAuthStore()
const confirmComplete = ref(false),
  completing = ref(false),
  completeError = ref(''),
  completedOrder = ref('')
const canComplete = computed(
  () =>
    auth.isAdmin &&
    auth.hasPermission('billing', 'write') &&
    displayOrder.value?.status === 'pending' &&
    completedOrder.value !== displayOrder.value.order_no
)
watch(
  () => props.order?.order_no,
  () => {
    confirmComplete.value = false
    completeError.value = ''
    completedOrder.value = ''
  }
)
async function complete() {
  if (!canComplete.value || completing.value || !props.order) return
  const tradeNo = props.order.order_no
  completing.value = true
  completeError.value = ''
  try {
    await api.post('/api/user/topup/complete', { trade_no: tradeNo })
    if (props.order?.order_no === tradeNo) {
      completedOrder.value = tradeNo
      confirmComplete.value = false
      evidenceRetry.value++
    }
    emit('completed')
  } catch (error) {
    if (props.order?.order_no === tradeNo)
      completeError.value =
        error instanceof Error ? error.message : String(error)
  } finally {
    completing.value = false
  }
}
const detail = ref<ReturnType<typeof parseTopUpDetail> | null>(null)
const evidence = computed(() => detail.value?.evidence ?? null)
const displayOrder = computed(() =>
  props.order && detail.value
    ? { ...props.order, ...detail.value.state }
    : props.order
)
const evidenceLoading = ref(false)
const evidenceFailed = ref(false)
const evidenceRetry = ref(0)
const evidenceStatusLabels = {
  creating: 'orders.gmpay.creating',
  ready: 'orders.gmpay.ready',
  failed: 'orders.gmpay.failed',
  unknown: 'orders.gmpay.unknown',
  settled: 'orders.gmpay.settled',
} as const
const evidenceFields = computed(() => {
  const current = evidence.value
  if (!current) return []
  return [
    {
      label: 'orders.gmpay.status',
      value: t(evidenceStatusLabels[current.status]),
    },
    { label: 'orders.gmpay.failure', value: current.last_failure },
    { label: 'orders.gmpay.tradeNo', value: current.gateway_trade_no },
    { label: 'orders.gmpay.token', value: current.token.toUpperCase() },
    { label: 'orders.gmpay.requestedNetwork', value: current.network },
    {
      label: 'orders.gmpay.createdAmount',
      value: current.gateway_crypto_amount,
    },
    { label: 'orders.gmpay.createdAddress', value: current.receive_address },
    {
      label: 'orders.gmpay.settledAmount',
      value: current.settled_crypto_amount,
    },
    {
      label: 'orders.gmpay.settledAddress',
      value: current.settled_receive_address,
    },
    {
      label: 'orders.gmpay.transactionId',
      value: current.settled_transaction_id,
    },
  ]
})

watch(
  () => [props.order?.order_no, props.order?.method, evidenceRetry.value],
  async (_, __, onCleanup) => {
    detail.value = null
    evidenceFailed.value = false
    evidenceLoading.value = false
    const current = props.order
    if (!current) return
    const controller = new AbortController()
    onCleanup(() => controller.abort())
    evidenceLoading.value = true
    try {
      const response = await api.get<unknown>(
        `/api/user/topup/admin/detail/${encodeURIComponent(current.order_no)}`,
        undefined,
        { signal: controller.signal }
      )
      if (!controller.signal.aborted) {
        detail.value = parseTopUpDetail(response, current.order_no)
      }
    } catch {
      if (!controller.signal.aborted) evidenceFailed.value = true
    } finally {
      if (!controller.signal.aborted) evidenceLoading.value = false
    }
  },
  { immediate: true }
)

function paymentRailLabel(method: string): string {
  if (isAdminOrderPaymentRail(method)) {
    return t(adminOrderPaymentRailLabelKey(method))
  }
  return method || t('orders.notRecorded')
}
</script>

<template>
  <ConsoleModal
    :open="order !== null"
    :title="t('orders.detailTitle')"
    :subtitle="order?.order_no"
    size="lg"
    @close="emit('close')"
  >
    <dl v-if="displayOrder" class="grid gap-x-6 gap-y-4 sm:grid-cols-2">
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colId') }}
        </dt>
        <dd class="mt-1 font-mono text-sm text-[var(--text-primary)]">
          #{{ displayOrder.id }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colStatus') }}
        </dt>
        <dd class="mt-1">
          <StatusChip :tone="adminOrderStatusTone(displayOrder.status)">
            {{ t(adminOrderStatusLabelKey(displayOrder.status)) }}
          </StatusChip>
        </dd>
      </div>
      <div class="min-w-0 sm:col-span-2">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colOrderNo') }}
        </dt>
        <dd class="mt-1 break-all font-mono text-sm text-[var(--text-primary)]">
          {{ displayOrder.order_no }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colUser') }}
        </dt>
        <dd class="mt-1 min-w-0 text-sm text-[var(--text-primary)]">
          <span class="block truncate" :title="displayOrder.email">
            {{
              displayOrder.email ||
              displayOrder.username ||
              `#${displayOrder.user_id}`
            }}
          </span>
          <span class="block truncate text-xs text-[var(--text-tertiary)]">
            {{ displayOrder.username || t('orders.notRecorded') }} · #{{
              displayOrder.user_id
            }}
          </span>
          <StatusChip v-if="detail?.userDeleted" tone="neutral" class="mt-1">
            {{ t('orders.userDeleted') }}
          </StatusChip>
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colAmount') }}
        </dt>
        <dd
          class="mt-1 text-lg font-bold tabular-nums text-[var(--text-primary)]"
        >
          {{
            formatAdminOrderAmount(displayOrder.amount, displayOrder.currency)
          }}
          <span v-if="displayOrder.currency">
            · {{ displayOrder.currency }}</span
          >
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colQuota') }}
        </dt>
        <dd class="mt-1 text-sm tabular-nums text-[var(--text-primary)]">
          {{
            BigInt(displayOrder.quota) > 0n
              ? formatQuota(displayOrder.quota)
              : t('orders.notCredited')
          }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colType') }}
        </dt>
        <dd class="mt-1 text-sm text-[var(--text-primary)]">
          {{ t(adminOrderTypeLabelKey(displayOrder.type)) }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colProvider') }}
        </dt>
        <dd class="mt-1 text-sm text-[var(--text-primary)]">
          {{ t(adminOrderMethodLabelKey(displayOrder.method)) }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colPaymentMethod') }}
        </dt>
        <dd class="mt-1 text-sm text-[var(--text-primary)]">
          {{ paymentRailLabel(displayOrder.payment_method) }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colCreated') }}
        </dt>
        <dd class="mt-1 text-sm tabular-nums text-[var(--text-secondary)]">
          {{ formatTime(displayOrder.created) }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('orders.colPaidAt') }}
        </dt>
        <dd class="mt-1 text-sm tabular-nums text-[var(--text-secondary)]">
          {{
            displayOrder.paid_at > 0
              ? formatTime(displayOrder.paid_at)
              : t('orders.notRecorded')
          }}
        </dd>
      </div>
    </dl>

    <section
      v-if="order?.method === 'gmpay'"
      class="mt-6 border-t border-[var(--border-subtle)] pt-5"
    >
      <h4 class="text-sm font-semibold text-[var(--text-primary)]">
        {{ t('orders.gmpay.title') }}
      </h4>
      <p
        v-if="evidenceLoading"
        role="status"
        class="mt-3 text-sm text-[var(--text-secondary)]"
      >
        {{ t('orders.gmpay.loading') }}
      </p>
      <div v-else-if="evidenceFailed" class="mt-3">
        <p role="alert" class="text-sm text-[var(--status-danger-text)]">
          {{ t('orders.gmpay.loadFailed') }}
        </p>
        <ConsoleButton
          class="mt-2"
          variant="secondary"
          size="sm"
          @click="evidenceRetry++"
          >{{ t('orders.gmpay.retry') }}</ConsoleButton
        >
      </div>
      <p
        v-else-if="!evidence"
        role="status"
        class="mt-3 text-sm text-[var(--text-secondary)]"
      >
        {{ t('orders.gmpay.missing') }}
      </p>
      <template v-else>
        <p class="mt-2 text-xs text-[var(--text-tertiary)]">
          {{ t('orders.gmpay.networkNote') }}
        </p>
        <dl class="mt-4 grid gap-x-6 gap-y-4 sm:grid-cols-2">
          <div
            v-for="field in evidenceFields"
            :key="field.label"
            class="min-w-0"
          >
            <dt class="text-xs text-[var(--text-tertiary)]">
              {{ t(field.label) }}
            </dt>
            <dd
              class="mt-1 break-all font-mono text-sm text-[var(--text-primary)]"
            >
              {{ field.value || t('orders.notRecorded') }}
            </dd>
          </div>
        </dl>
      </template>
    </section>

    <template #footer>
      <ConsoleButton
        v-if="canComplete"
        :disabled="completing || evidenceLoading"
        @click="confirmComplete = true"
        >{{ paymentT('nativePayment.complete') }}</ConsoleButton
      >
      <ConsoleButton
        variant="secondary"
        block
        :disabled="completing"
        @click="emit('close')"
      >
        {{ t('common.close') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
  <ConfirmDialog
    :open="confirmComplete"
    :title="paymentT('nativePayment.complete')"
    :message="paymentT('nativePayment.confirm')"
    :loading="completing"
    @confirm="complete"
    @cancel="confirmComplete = false"
  >
    <p
      v-if="completeError"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger-text)]"
    >
      {{ completeError }}
    </p>
  </ConfirmDialog>
</template>
