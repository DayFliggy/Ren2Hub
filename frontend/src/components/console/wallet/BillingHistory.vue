<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { billingApi, type BillingEvent } from '@/api/billing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatQuota, formatTime } from '@/utils/format'

const props = defineProps<{ tokenId?: number; refreshKey?: number }>()
const { t } = useI18n()
const rows = ref<BillingEvent[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const error = ref('')
const request = useLatestRequest()

async function load() {
  loading.value = true
  error.value = ''
  const result = await request.run((signal) =>
    billingApi.events(
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
watch([page, pageSize, () => props.tokenId, () => props.refreshKey], load, {
  immediate: true,
})
</script>

<template>
  <section
    class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 sm:p-6"
    :aria-busy="loading"
  >
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--text-primary)]">
        {{ t('billing.events') }}
      </h3>
      <ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('billing.refresh')
      }}</ConsoleButton>
    </div>
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
        :key="row.event_id"
        class="grid gap-2 py-4 sm:grid-cols-[minmax(0,1fr)_auto]"
      >
        <div class="min-w-0 space-y-1">
          <p class="break-all font-mono text-xs text-[var(--text-primary)]">
            {{ row.event_id }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ formatTime(row.created_at) }} · {{ row.operation }}
          </p>
          <p
            v-if="row.reason"
            class="break-words text-xs text-[var(--text-secondary)]"
          >
            {{ row.reason }}
          </p>
        </div>
        <dl class="space-y-1 text-xs">
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
                formatQuota(tokenId ? row.token_used_delta : row.used_delta, 3)
              }}
            </dd>
          </div>
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
