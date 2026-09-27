<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  billingApi,
  type BillingMetrics,
  type BillingDetail,
  type BillingReservation,
  type BillingResolution,
} from '@/api/billing'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import PageHero from '@/components/console/PageHero.vue'
import BillingAuditPanel from '@/components/admin/BillingAuditPanel.vue'
import BillingAdjustments from '@/components/console/log-ui/BillingAdjustments.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatQuota, formatTime } from '@/utils/format'
import { formatBillingEvidence, formatDecimal } from '@/utils/decimal'
import { isQuota } from '@/utils/quota'
import { newBillingOperationId } from '@/utils/billingOperation'

const { t } = useI18n()
const metrics = ref<BillingMetrics | null>(null)
const metricsRequest = useLatestRequest()
const rows = ref<BillingReservation[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const status = ref('')
const loading = ref(false)
const error = ref('')
const detail = ref<BillingDetail | null>(null)
const detailOpen = ref(false)
const detailError = ref('')
const detailLoading = ref(false)
const saving = ref(false)
const resultMessage = ref('')
const action = ref<'settle' | 'release'>('settle')
const quota = ref('0')
const reason = ref('')
const pendingOperation = ref<{ id: string; fingerprint: string } | null>(null)
const listRequest = useLatestRequest()
const detailRequest = useLatestRequest()
const terminal = computed(
  () =>
    detail.value &&
    ['settled', 'refunded', 'released'].includes(detail.value.recovery.state)
)
const valid = computed(
  () =>
    !terminal.value &&
    reason.value.trim().length > 0 &&
    new TextEncoder().encode(reason.value.trim()).length <= 512 &&
    isQuota(quota.value) &&
    BigInt(quota.value) >= 0n &&
    BigInt(quota.value) <= 2_147_483_647n
)

async function loadMetrics() {
  metrics.value = null
  const result = await metricsRequest.run((signal) =>
    billingApi.metrics(signal)
  )
  if (!result.stale && result.ok) metrics.value = result.value
}

async function load() {
  void loadMetrics()
  loading.value = true
  error.value = ''
  const result = await listRequest.run((signal) =>
    billingApi.reconciliations(
      (page.value - 1) * pageSize.value,
      pageSize.value,
      status.value,
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    error.value = t('billing.unavailable')
    return
  }
  rows.value = result.value.items
  total.value = result.value.total
}

async function openDetail(id: string) {
  detailOpen.value = true
  detailLoading.value = true
  detailError.value = ''
  detail.value = null
  resultMessage.value = ''
  pendingOperation.value = null
  const result = await detailRequest.run((signal) =>
    billingApi.detail(id, signal)
  )
  if (result.stale) return
  detailLoading.value = false
  if (!result.ok) {
    detailError.value = t('billing.unavailable')
    return
  }
  detail.value = result.value
  quota.value = result.value.recovery.funding_amount
  reason.value = ''
}

async function submit(retry: boolean) {
  if (
    !detail.value ||
    saving.value ||
    (!retry && terminal.value) ||
    (!retry && !valid.value)
  )
    return
  const row = detail.value.recovery
  const body = {
    expected_version: row.version,
    action: action.value,
    quota: action.value === 'release' ? '0' : quota.value,
    reason: reason.value.trim(),
  }
  const fingerprint = JSON.stringify({
    request_id: row.request_id,
    retry,
    ...body,
  })
  if (pendingOperation.value?.fingerprint !== fingerprint)
    pendingOperation.value = { id: newBillingOperationId(), fingerprint }
  saving.value = true
  detailError.value = ''
  resultMessage.value = ''
  try {
    if (retry)
      await billingApi.retry(
        row.request_id,
        pendingOperation.value.id,
        row.version
      )
    else
      await billingApi.resolve(row.request_id, {
        operation_id: pendingOperation.value.id,
        ...body,
      } as BillingResolution)
    const updated = await billingApi.detail(row.request_id)
    detail.value = updated
    resultMessage.value = t('billing.operationSaved')
    pendingOperation.value = null
    await load()
  } catch (error) {
    detailError.value =
      error instanceof ApiError && error.status === 409
        ? t('billing.conflict')
        : t('billing.operationFailed')
  } finally {
    saving.value = false
  }
}

watch(status, () => {
  page.value = 1
})
watch([page, pageSize, status], load, { immediate: true })
</script>

<template>
  <div>
    <PageHero :title="t('billing.reconciliations')" />
    <p class="mb-5 text-sm text-[var(--text-tertiary)]">
      {{ t('billing.reconciliationHint') }}
    </p>
    <dl class="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
      <div
        v-for="key in [
          'price_versions',
          'quote_attempts',
          'pending',
          'reconciliations',
          'reserved_quota',
          'oldest_wait_seconds',
          'idempotency_conflicts',
          'recovery_failures',
          'debt_events',
        ] as const"
        :key="key"
        class="min-w-0 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t(`billing.metrics.${key}`) }}
        </dt>
        <dd class="mt-2 break-all font-mono text-sm">
          {{
            metrics
              ? key === 'reserved_quota'
                ? formatQuota(metrics[key], 3)
                : metrics[key]
              : t('billing.metricsUnavailable')
          }}
        </dd>
      </div>
    </dl>
    <ul
      v-if="metrics?.prices_by_origin.length"
      class="mb-5 flex flex-wrap gap-3 text-xs"
      data-prices-by-origin
    >
      <li
        v-for="entry in metrics.prices_by_origin"
        :key="`${entry.scope_kind}:${entry.origin}`"
      >
        {{ entry.scope_kind }} · {{ entry.origin }}: {{ entry.count }}
      </li>
    </ul>
    <details v-if="metrics?.operational" class="mb-5 text-xs">
      <summary class="cursor-pointer focus-ring">
        {{ t('billing.operationalMetrics') }}
      </summary>
      <pre class="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-all">{{
        JSON.stringify(metrics.operational, null, 2)
      }}</pre>
    </details>
    <section
      class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 sm:p-6"
      :aria-busy="loading"
    >
      <div class="mb-5 flex flex-wrap justify-between gap-3">
        <label class="flex items-center gap-3 text-sm"
          >{{ t('billing.state')
          }}<select
            v-model="status"
            class="rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 focus-ring"
          >
            <option value="">{{ t('billing.pending') }}</option>
            <option
              v-for="state in [
                'active',
                'settlement_pending',
                'refund_pending',
                'settled',
                'refunded',
              ]"
              :key="state"
              :value="state"
            >
              {{ t(`billing.states.${state}`) }}
            </option>
          </select></label
        >
        <ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
          t('billing.refresh')
        }}</ConsoleButton>
      </div>
      <p
        v-if="error"
        role="alert"
        class="py-8 text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
      <p
        v-else-if="!rows.length && !loading"
        class="py-8 text-[var(--text-tertiary)]"
      >
        {{ t('billing.empty') }}
      </p>
      <ul v-else class="divide-y divide-[var(--border-subtle)]">
        <li
          v-for="row in rows"
          :key="row.request_id"
          class="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between"
        >
          <div class="min-w-0">
            <p class="break-all font-mono text-sm">{{ row.request_id }}</p>
            <p class="mt-1 text-xs text-[var(--text-tertiary)]">
              {{ t('billing.account') }} {{ row.user_id }} ·
              {{ t('billing.token') }} {{ row.token_id }} ·
              {{ formatTime(row.updated_at) }}
            </p>
            <p
              v-if="row.manual_review_required"
              class="mt-1 text-xs text-[var(--status-warning-text)]"
            >
              {{ t('billing.needsReview') }}
            </p>
          </div>
          <div class="flex shrink-0 items-center justify-between gap-4">
            <div class="text-right">
              <p class="font-mono text-sm">
                {{ formatQuota(row.funding_amount, 3) }}
              </p>
              <p class="text-xs text-[var(--text-tertiary)]">{{ row.state }}</p>
            </div>
            <ConsoleButton
              variant="secondary"
              @click="openDetail(row.request_id)"
              >{{ t('billing.details') }}</ConsoleButton
            >
          </div>
        </li>
      </ul>
      <TablePagination
        v-if="!error && total > 0"
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
      />
    </section>
    <ConsoleModal
      :open="detailOpen"
      :title="t('billing.details')"
      size="xl"
      :close-disabled="saving"
      @close="detailOpen = false"
    >
      <div :aria-busy="detailLoading || saving" class="space-y-5">
        <p
          v-if="detailError"
          role="alert"
          class="text-sm text-[var(--status-danger-text)]"
        >
          {{ detailError }}
        </p>
        <p
          v-if="resultMessage"
          role="status"
          class="text-sm text-[var(--status-success-text)]"
        >
          {{ resultMessage }}
        </p>
        <template v-if="detail">
          <p class="break-all font-mono text-sm">
            {{ detail.recovery.request_id }} · {{ detail.recovery.state }} · v{{
              detail.recovery.version
            }}
          </p>
          <p
            v-if="detail.recovery.last_error"
            class="break-words text-sm text-[var(--status-warning-text)]"
          >
            {{ detail.recovery.last_error }}
          </p>
          <BillingAuditPanel
            :key="detail.recovery.request_id"
            :detail="detail"
            @updated="detail = $event"
          />
          <details
            v-for="section in ['quote', 'usage', 'attempts'] as const"
            :key="section"
            class="rounded-lg border border-[var(--border-subtle)] p-3"
          >
            <summary class="cursor-pointer text-sm focus-ring">
              {{ t(`billing.${section}`) }}
            </summary>
            <pre
              class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs"
              >{{ formatBillingEvidence(detail[section]) }}</pre>
          </details>
          <section
            class="space-y-3 rounded-lg border border-[var(--border-subtle)] p-3"
          >
            <h3 class="text-sm font-semibold">{{ t('billing.charge') }}</h3>
            <p
              v-if="!detail.charge"
              class="text-xs text-[var(--text-tertiary)]"
            >
              {{
                t(
                  detail.chargeUnavailable
                    ? 'billing.chargeUnavailable'
                    : 'billing.chargePending'
                )
              }}
            </p>
            <template v-else>
              <BillingAdjustments :charge="detail.charge" />
              <ul class="space-y-3">
                <li
                  v-for="(line, index) in detail.charge.lines"
                  :key="index"
                  class="grid gap-1 text-xs sm:grid-cols-2"
                >
                  <span class="break-all"
                    >{{ line.quantity }} · {{ formatDecimal(line.count) }} ·
                    {{ line.unit }}</span
                  ><span class="break-all font-mono sm:text-right"
                    >{{ formatDecimal(line.unit_price) }} USD →
                    {{ formatDecimal(line.subtotal_usd) }} USD</span
                  >
                </li>
              </ul>
              <dl
                class="space-y-2 border-t border-[var(--border-subtle)] pt-3 text-xs"
              >
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.unrounded') }}</dt>
                  <dd class="break-all font-mono">
                    {{ formatDecimal(detail.charge.unrounded_quota) }} quota
                  </dd>
                </div>
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.rounding') }}</dt>
                  <dd class="font-mono">
                    {{ formatDecimal(detail.charge.rounding_delta) }} quota
                  </dd>
                </div>
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.finalCharge') }}</dt>
                  <dd class="font-mono">
                    {{ formatQuota(detail.charge.final_quota, 3) }} ({{
                      detail.charge.final_quota
                    }}
                    quota)
                  </dd>
                </div>
              </dl>
              <p
                class="break-all font-mono text-xs text-[var(--text-tertiary)]"
              >
                {{ detail.charge.calculation_version }} ·
                {{ detail.charge.rounding_version }}
              </p>
            </template>
          </section>
          <h3 class="text-sm font-semibold">
            {{ t('billing.processingHistory') }}
          </h3>
          <ul class="space-y-2">
            <li
              v-for="event in detail.events"
              :key="event.event_id"
              class="rounded-lg bg-[var(--surface-muted)] p-3 text-xs"
            >
              <p class="break-all font-mono">{{ event.event_id }}</p>
              <p class="mt-1">
                {{ event.operation }} · {{ formatTime(event.created_at) }} ·
                {{ t('billing.operator') }} {{ event.actor_id }}
              </p>
              <p class="mt-1 break-words">{{ event.reason }}</p>
              <dl class="mt-2 grid gap-1">
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.availableChange') }}</dt>
                  <dd class="font-mono">
                    {{ formatQuota(event.available_delta, 3) }}
                  </dd>
                </div>
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.reservedChange') }}</dt>
                  <dd class="font-mono">
                    {{ formatQuota(event.reserved_delta, 3) }}
                  </dd>
                </div>
                <div class="flex flex-wrap justify-between gap-2">
                  <dt>{{ t('billing.usedChange') }}</dt>
                  <dd class="font-mono">
                    {{ formatQuota(event.used_delta, 3) }}
                  </dd>
                </div>
              </dl>
            </li>
          </ul>
          <div
            v-if="!terminal"
            class="space-y-3 border-t border-[var(--border-subtle)] pt-5"
          >
            <label class="block text-sm"
              >{{ t('billing.action')
              }}<select
                v-model="action"
                :disabled="saving"
                class="mt-2 block w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 focus-ring"
              >
                <option value="settle">{{ t('billing.settle') }}</option>
                <option value="release">{{ t('billing.release') }}</option>
              </select></label
            >
            <label v-if="action === 'settle'" class="block text-sm"
              >{{ t('billing.quotaAmount')
              }}<input
                v-model="quota"
                inputmode="numeric"
                :disabled="saving"
                class="mt-2 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono focus-ring"
            /></label>
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('billing.resolveHint') }}
            </p>
            <label class="block text-sm"
              >{{ t('billing.reason')
              }}<textarea
                v-model="reason"
                :disabled="saving"
                maxlength="512"
                rows="3"
                class="mt-2 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 focus-ring"
              />
            </label>
          </div>
        </template>
      </div>
      <template #footer
        ><div v-if="detail" class="flex flex-wrap justify-end gap-3">
          <ConsoleButton
            variant="secondary"
            :disabled="saving || detail.recovery.state === 'refunded'"
            @click="submit(true)"
            >{{ t('billing.retry') }}</ConsoleButton
          ><ConsoleButton
            v-if="!terminal"
            :disabled="!valid"
            :loading="saving"
            @click="submit(false)"
            >{{ t('billing.resolve') }}</ConsoleButton
          >
        </div></template
      >
    </ConsoleModal>
  </div>
</template>
