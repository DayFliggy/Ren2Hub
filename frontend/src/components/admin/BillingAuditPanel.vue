<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  billingApi,
  type BillingCostCommand,
  type BillingDetail,
} from '@/api/billing'
import { ApiError } from '@/api/types'
import { newBillingOperationId } from '@/utils/billingOperation'
import {
  formatBillingEvidence,
  formatDecimal,
  isDecimal,
} from '@/utils/decimal'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

const props = defineProps<{ detail: BillingDetail }>()
const emit = defineEmits<{ updated: [detail: BillingDetail] }>()
const { t } = useI18n()
const attemptId = ref('')
const amount = ref('')
const source = ref<BillingCostCommand['source']>('invoice')
const reference = ref('')
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const pending = ref<{ fingerprint: string; eventId: string } | null>(null)
let disposed = false
onBeforeUnmount(() => {
  disposed = true
})
const unconfirmed = computed(() =>
  props.detail.audit.attempts.filter(
    (attempt) =>
      !props.detail.upstream_cost.records.some(
        (record) => record.attempt_id === attempt.id
      )
  )
)
const valid = computed(
  () =>
    unconfirmed.value.some((attempt) => attempt.id === attemptId.value) &&
    isDecimal(amount.value) &&
    amount.value.length <= 64 &&
    (amount.value.split('.')[1]?.length ?? 0) <= 18 &&
    reference.value.trim().length > 0 &&
    new TextEncoder().encode(reference.value.trim()).length <= 256
)
async function saveCost() {
  if (!valid.value || saving.value) return
  const requestId = props.detail.recovery.request_id
  const body = {
    attempt_id: attemptId.value,
    amount_usd: amount.value,
    source: source.value,
    reference: reference.value.trim(),
  }
  const fingerprint = JSON.stringify({ requestId, ...body })
  if (pending.value?.fingerprint !== fingerprint)
    pending.value = { fingerprint, eventId: newBillingOperationId() }
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    await billingApi.recordCost(requestId, {
      event_id: pending.value.eventId,
      ...body,
    })
    const detail = await billingApi.detail(requestId)
    if (disposed) return
    emit('updated', detail)
    pending.value = null
    saved.value = true
    attemptId.value = ''
    amount.value = ''
    reference.value = ''
  } catch (cause) {
    if (!disposed)
      error.value = t(
        cause instanceof ApiError && cause.status === 409
          ? 'billing.conflict'
          : 'billing.operationFailed'
      )
  } finally {
    if (!disposed) saving.value = false
  }
}
</script>

<template>
  <section
    class="space-y-4 rounded-lg border border-[var(--border-subtle)] p-3"
    data-billing-audit
  >
    <h3 class="font-semibold">{{ t('billing.auditTitle') }}</h3>
    <p :data-audit-status="detail.audit.status" class="break-words">
      {{ t(`billing.auditStatuses.${detail.audit.status}`)
      }}<span v-if="detail.audit.reason"> · {{ detail.audit.reason }}</span>
    </p>
    <dl class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 text-sm">
      <template
        v-for="key in [
          'customer_charge_usd',
          'quoted_total_usd',
          'charge_difference_usd',
          'upstream_cost_usd',
          'profit_usd',
        ] as const"
        :key="key"
      >
        <dt>{{ t(`billing.auditAmounts.${key}`) }}</dt>
        <dd class="break-all text-right font-mono" :data-audit-amount="key">
          {{
            (key === 'customer_charge_usd' &&
              !detail.audit.customer_charge_confirmed) ||
            detail.audit[key] === null
              ? t('billing.unknownAmount')
              : `${formatDecimal(detail.audit[key]!)} USD`
          }}
        </dd>
      </template>
    </dl>
    <p class="text-xs text-[var(--text-tertiary)]">
      {{
        t('billing.costCompleteness', {
          confirmed: detail.upstream_cost.confirmed_attempt_count,
          total: detail.upstream_cost.attempt_count,
        })
      }}
    </p>
    <details>
      <summary class="cursor-pointer focus-ring">
        {{ t('billing.auditEvidence') }}
      </summary>
      <pre
        class="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all text-xs"
        >{{ formatBillingEvidence(detail.audit) }}</pre>
    </details>
    <ul class="space-y-2 text-xs">
      <li
        v-for="record in detail.upstream_cost.records"
        :key="record.event_id"
        class="break-all rounded bg-[var(--surface-muted)] p-2"
        data-cost-record
      >
        {{ record.attempt_id }} · {{ formatDecimal(record.amount_usd) }} USD ·
        {{ t(`billing.costSources.${record.source}`) }} · {{ record.reference }}
      </li>
    </ul>
    <form
      v-if="unconfirmed.length"
      class="space-y-3 border-t border-[var(--border-subtle)] pt-3"
      @submit.prevent="saveCost"
    >
      <h4 class="text-sm font-semibold">{{ t('billing.recordCost') }}</h4>
      <p class="text-xs text-[var(--text-tertiary)]">
        {{ t('billing.recordCostHint') }}
      </p>
      <label class="block text-sm"
        >{{ t('billing.attempt')
        }}<select
          v-model="attemptId"
          :disabled="saving"
          data-cost-attempt
          class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
        >
          <option value="">{{ t('billing.selectAttempt') }}</option>
          <option
            v-for="attempt in unconfirmed"
            :key="attempt.id"
            :value="attempt.id"
          >
            {{ attempt.id }}
          </option>
        </select></label
      >
      <label class="block text-sm"
        >{{ t('billing.costAmount')
        }}<input
          v-model="amount"
          :disabled="saving"
          inputmode="decimal"
          data-cost-amount
          class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2 font-mono focus-ring"
      /></label>
      <label class="block text-sm"
        >{{ t('billing.costSource')
        }}<select
          v-model="source"
          :disabled="saving"
          data-cost-source
          class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
        >
          <option value="invoice">
            {{ t('billing.costSources.invoice') }}
          </option>
          <option value="provider_receipt">
            {{ t('billing.costSources.provider_receipt') }}
          </option>
        </select></label
      >
      <label class="block text-sm"
        >{{ t('billing.costReference')
        }}<input
          v-model="reference"
          :disabled="saving"
          maxlength="256"
          data-cost-reference
          class="mt-1 w-full rounded border bg-[var(--surface-muted)] p-2 focus-ring"
      /></label>
      <ConsoleButton
        type="submit"
        :disabled="!valid || saving"
        :loading="saving"
        data-cost-submit
        >{{ t('billing.recordCost') }}</ConsoleButton
      >
    </form>
    <p
      v-if="error"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <p
      v-if="saved"
      role="status"
      class="text-sm text-[var(--status-success-text)]"
    >
      {{ t('billing.operationSaved') }}
    </p>
  </section>
</template>
