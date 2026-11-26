<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { getTopUps } from '@/api/adminOrders'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import {
  adminOrderMethodLabelKey,
  adminOrderStatusLabelKey,
  adminOrderStatusTone,
  formatAdminOrderAmount,
} from '@/constants/adminOrders'
import type { AdminOrder } from '@/types/console'
import { formatQuota, formatTime } from '@/utils/format'

const props = defineProps<{ refreshKey: number }>()

const { t } = useI18n()

const rows = ref<AdminOrder[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(6)
const loading = ref(true)
const loadError = ref('')
const listRequest = useLatestRequest()

async function load() {
  loading.value = true
  const result = await listRequest.run((signal) =>
    getTopUps({ p: page.value, page_size: pageSize.value }, signal, true)
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    loadError.value =
      result.error instanceof Error ? result.error.message : t('common.failed')
    return
  }
  loadError.value = ''
  rows.value = result.value.items
  total.value = result.value.total
}

function reloadFromFirstPage() {
  if (page.value === 1) void load()
  else page.value = 1
}

watch(pageSize, reloadFromFirstPage)
watch(page, load)
watch(() => props.refreshKey, load)
onMounted(load)
</script>

<template>
  <ConsoleCard :title="t('wallet.records')" :padded="false">
    <ErrorBanner
      v-if="!loading && loadError"
      class="m-4"
      :message="loadError"
      @retry="load"
    />
    <div
      v-if="loading"
      class="sibling-divide-y sibling-divide-[var(--border-subtle)] px-5"
      aria-busy="true"
    >
      <div
        v-for="i in 3"
        :key="i"
        class="flex items-center justify-between gap-4 py-3.5"
      >
        <div class="min-w-0 flex-1 sibling-space-y-2">
          <SkeletonBlock rounded="md" class="h-3.5 w-44 max-w-full" />
          <SkeletonBlock rounded="md" class="h-3 w-32 max-w-full" />
        </div>
        <div class="flex shrink-0 flex-col items-end gap-1.5">
          <SkeletonBlock rounded="md" class="h-4 w-16" />
          <SkeletonBlock rounded="md" class="h-5 w-14" />
        </div>
      </div>
    </div>
    <EmptyState
      v-else-if="!loadError && rows.length === 0"
      class="!py-8"
      :title="t('common.none')"
    />
    <ul
      v-else-if="!loadError"
      class="sibling-divide-y sibling-divide-[var(--border-subtle)] px-5"
    >
      <li
        v-for="row in rows"
        :key="row.id"
        class="-mx-5 flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 transition-colors hover:bg-[var(--state-hover-layer)]"
      >
        <div class="min-w-0">
          <p class="truncate font-mono text-xs text-[var(--text-secondary)]">
            {{ row.order_no }}
          </p>
          <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
            {{ t(adminOrderMethodLabelKey(row.method)) }} ·
            {{ formatTime(row.created) }}
          </p>
        </div>
        <div class="shrink-0 text-right">
          <p
            class="display-number font-mono text-sm text-[var(--text-primary)]"
          >
            {{ formatAdminOrderAmount(row.amount, row.currency) }}
          </p>
          <p
            v-if="row.status === 'completed'"
            class="font-mono text-[0.6875rem] tabular-nums text-[var(--text-tertiary)]"
          >
            {{
              BigInt(row.quota) > 0n
                ? formatQuota(row.quota)
                : t('orders.notCredited')
            }}
          </p>
          <p
            v-if="BigInt(row.bonus_quota) > 0n"
            class="font-mono text-[0.6875rem] tabular-nums text-[var(--accent-text)]"
            data-order-bonus
          >
            {{ t('orders.bonus', { bonus: formatQuota(row.bonus_quota) }) }}
          </p>
          <StatusChip :tone="adminOrderStatusTone(row.status)" class="mt-1">
            {{ t(adminOrderStatusLabelKey(row.status)) }}
          </StatusChip>
        </div>
      </li>
    </ul>
    <div class="border-t border-[var(--border-subtle)] px-3">
      <TablePagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[6, 12, 24]"
      />
    </div>
  </ConsoleCard>
</template>
