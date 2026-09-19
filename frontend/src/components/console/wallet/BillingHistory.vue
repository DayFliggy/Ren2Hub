<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  billingApi,
  type BillingReservation,
  type BillingEvent,
  type BillingPage,
} from '@/api/billing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatQuota, formatTime } from '@/utils/format'

const props = defineProps<{ tokenId?: number; refreshKey?: number }>()
const { t } = useI18n()
const tab = ref<'reservations' | 'events'>('reservations')
const rows = ref<Array<BillingReservation | BillingEvent>>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const error = ref('')
const request = useLatestRequest()

async function load() {
  loading.value = true
  error.value = ''
  const result = await request.run<
    BillingPage<BillingReservation | BillingEvent>
  >((signal) =>
    tab.value === 'reservations'
      ? billingApi.reservations(
          (page.value - 1) * pageSize.value,
          pageSize.value,
          signal,
          props.tokenId
        )
      : billingApi.events(
          (page.value - 1) * pageSize.value,
          pageSize.value,
          signal,
          props.tokenId
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
watch(tab, () => {
  page.value = 1
})
watch(
  [tab, page, pageSize, () => props.tokenId, () => props.refreshKey],
  load,
  { immediate: true }
)
</script>

<template>
  <section
    class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 sm:p-6"
    :aria-busy="loading"
  >
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="flex gap-2" role="tablist" :aria-label="t('billing.history')">
        <button
          v-for="name in ['reservations', 'events'] as const"
          :key="name"
          type="button"
          role="tab"
          :aria-selected="tab === name"
          class="rounded-lg px-3 py-2 text-sm focus-ring"
          :class="
            tab === name
              ? 'bg-[var(--accent-soft)] text-[var(--accent-text)]'
              : 'text-[var(--text-secondary)]'
          "
          @click="tab = name"
        >
          {{ t(`billing.${name}`) }}
        </button>
      </div>
      <ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('billing.refresh')
      }}</ConsoleButton>
    </div>
    <p class="mb-4 text-sm text-[var(--text-tertiary)]">
      {{ t('billing.reservationHint') }}
    </p>
    <p v-if="error" role="alert" class="py-6 text-[var(--status-danger-text)]">
      {{ error }}
    </p>
    <p
      v-else-if="!loading && !rows.length"
      class="py-6 text-sm text-[var(--text-tertiary)]"
    >
      {{ t('billing.empty') }}
    </p>
    <ul v-else class="divide-y divide-[var(--border-subtle)]">
      <li
        v-for="row in rows"
        :key="'event_id' in row ? row.event_id : row.request_id"
        class="grid gap-2 py-4 sm:grid-cols-[minmax(0,1fr)_auto]"
      >
        <div class="min-w-0 space-y-1">
          <p class="break-all font-mono text-xs text-[var(--text-primary)]">
            {{ 'event_id' in row ? row.event_id : row.request_id }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ formatTime(row.created_at) }} ·
            {{ 'state' in row ? row.state : row.operation }}
          </p>
          <p
            v-if="'state' in row && row.manual_review_required"
            class="text-xs text-[var(--status-warning-text)]"
          >
            {{ t('billing.needsReview') }}
          </p>
          <p
            v-if="'reason' in row && row.reason"
            class="break-words text-xs text-[var(--text-secondary)]"
          >
            {{ row.reason }}
          </p>
        </div>
        <dl class="space-y-1 text-xs">
          <template v-if="'funding_amount' in row">
            <div class="flex justify-between gap-4">
              <dt>{{ t('billing.reserved') }}</dt>
              <dd class="font-mono">
                {{
                  formatQuota(
                    tokenId ? row.token_amount : row.funding_amount,
                    3
                  )
                }}
              </dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt>{{ t('billing.token') }}</dt>
              <dd>{{ row.token_id }}</dd>
            </div>
          </template>
          <template v-else>
            <div class="flex justify-between gap-4">
              <dt>{{ t('billing.availableChange') }}</dt>
              <dd class="font-mono">
                {{
                  formatQuota(
                    tokenId ? row.token_available_delta : row.available_delta,
                    3
                  )
                }}
              </dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt>{{ t('billing.reservedChange') }}</dt>
              <dd class="font-mono">
                {{
                  formatQuota(
                    tokenId ? row.token_reserved_delta : row.reserved_delta,
                    3
                  )
                }}
              </dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt>{{ t('billing.usedChange') }}</dt>
              <dd class="font-mono">
                {{
                  formatQuota(
                    tokenId ? row.token_used_delta : row.used_delta,
                    3
                  )
                }}
              </dd>
            </div>
          </template>
        </dl>
      </li>
    </ul>
    <TablePagination
      v-if="!error && total > 0"
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="total"
    />
  </section>
</template>
