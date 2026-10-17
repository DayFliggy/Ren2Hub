<script setup lang="ts">
import { Receipt } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { topupApi, type TopupOrder } from '@/api/topup'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatTime } from '@/utils/format'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import OrderStatCard from '@/components/console/orders/OrderStatCard.vue'
import { serializeOrders } from '@/components/console/orders/orderExport'
import { restoredOrderMessages } from '@/i18n/restoredOrders'
import { nativePaymentMessages } from '@/i18n/nativePayment'
const props = withDefaults(
  defineProps<{ refreshKey?: number; admin?: boolean }>(),
  { refreshKey: 0, admin: false }
)
const { t: globalT } = useI18n({ useScope: 'global' })
const { t } = useI18n({
    messages: {
      en: { ...nativePaymentMessages.en, ...restoredOrderMessages.en },
      'zh-CN': {
        ...nativePaymentMessages['zh-CN'],
        ...restoredOrderMessages['zh-CN'],
      },
    },
  }),
  rows = ref<TopupOrder[]>([]),
  total = ref(0),
  page = ref(1),
  pageSize = ref(10),
  keyword = ref(''),
  loading = ref(false),
  error = ref(''),
  target = ref<TopupOrder | null>(null),
  saving = ref(false),
  request = useLatestRequest()
async function load() {
  loading.value = true
  error.value = ''
  rows.value = []
  total.value = 0
  detail.value = null
  const result = await request.run((signal) =>
    topupApi.orders(
      props.admin,
      { p: page.value, page_size: pageSize.value, keyword: keyword.value },
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (result.ok) {
    rows.value = result.value.items ?? []
    total.value = result.value.total
  } else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
async function complete() {
  if (!target.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    await topupApi.complete(target.value.trade_no)
    target.value = null
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
watch([page, pageSize, () => props.refreshKey], load)
watch(keyword, () => {
  if (page.value === 1) void load()
  else page.value = 1
})
onMounted(load)
const activeTab = ref('list')
const detail = ref<TopupOrder | null>(null)
const statusFilter = ref('')
const methodFilter = ref('')
const exportOpen = ref(false)
const exportFormat = ref('csv')
const tabs = computed(() => [
  { key: 'overview', label: t('restoredOrders.overview') },
  { key: 'list', label: t('restoredOrders.list') },
])
const visibleRows = computed(() =>
  rows.value.filter(
    (row) =>
      (!statusFilter.value || row.status === statusFilter.value) &&
      (!methodFilter.value || row.payment_method === methodFilter.value)
  )
)
const pageMethods = computed(() =>
  [...new Set(rows.value.map((row) => row.payment_method))].map((method) => ({
    method,
    count: rows.value.filter((row) => row.payment_method === method).length,
  }))
)
const methodOptions = computed(() => [
  { value: '', label: t('restoredOrders.all') },
  // Retain selected filters so a new page with no matches still explains its empty result.
  ...[
    ...new Set([
      methodFilter.value,
      ...pageMethods.value.map(({ method }) => method),
    ]),
  ]
    .filter(Boolean)
    .map((method) => ({ value: method, label: method })),
])
const statusOptions = computed(() => [
  { value: '', label: t('restoredOrders.all') },
  ...[...new Set([statusFilter.value, ...rows.value.map((row) => row.status)])]
    .filter(Boolean)
    .map((value) => ({
      value,
      label: statusLabel(value),
    })),
])
const overview = computed(() => [
  { key: 'total', value: total.value },
  {
    key: 'paid',
    value: rows.value.filter((row) => row.status === 'success').length,
  },
  {
    key: 'pending',
    value: rows.value.filter((row) => row.status === 'pending').length,
  },
  { key: 'users', value: new Set(rows.value.map((row) => row.user_id)).size },
])
function exportPage() {
  const [content, mime, extension] = serializeOrders(
    visibleRows.value,
    exportFormat.value
  )
  const url = URL.createObjectURL(new Blob([content], { type: mime }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `ren2hub-orders-page-${page.value}.${extension}`
  anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 0)
  exportOpen.value = false
}
const statusLabel = (value: string) =>
  ['pending', 'success', 'failed', 'expired'].includes(value)
    ? t(`nativePayment.${value}`)
    : value
</script>
<template>
  <div class="sibling-space-y-5">
    <ConsoleTabs
      v-if="admin"
      v-model="activeTab"
      :items="tabs"
      panel-id="order-records-panel"
    />
    <div
      id="order-records-panel"
      :role="admin ? 'tabpanel' : undefined"
      :aria-labelledby="
        admin ? `order-records-panel-tab-${activeTab}` : undefined
      "
    >
      <ErrorBanner v-if="error" :message="error" @retry="load" />
      <template v-if="admin && activeTab === 'overview'">
        <p class="mb-4 text-sm text-[var(--text-tertiary)]">
          {{ t('restoredOrders.scope') }}
        </p>
        <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <OrderStatCard
            v-for="item in overview"
            :key="item.key"
            :label="t(`restoredOrders.${item.key}`)"
            :value="error ? '--' : String(item.value)"
            :loading="loading"
            ><template #icon><Receipt :size="20" /></template
          ></OrderStatCard>
        </div>
        <div class="mt-5 grid gap-5 lg:grid-cols-2">
          <ConsoleCard :title="t('restoredOrders.trend')"
            ><EmptyState
              :title="t('restoredOrders.unavailable')"
              :hint="t('restoredOrders.unavailableHint')"
          /></ConsoleCard>
          <ConsoleCard :title="t('restoredOrders.methods')">
            <ul v-if="pageMethods.length" class="sibling-space-y-4">
              <li v-for="item in pageMethods" :key="item.method">
                <div class="flex justify-between gap-3 text-sm">
                  <span class="break-all">{{ item.method }}</span
                  ><strong>{{ item.count }}</strong>
                </div>
                <div class="mt-2 h-2 rounded bg-[var(--surface-muted)]">
                  <div
                    class="h-2 rounded bg-[var(--accent)]"
                    :style="{ width: `${(item.count / rows.length) * 100}%` }"
                  />
                </div>
              </li>
            </ul>
            <EmptyState
              v-else
              :title="t('restoredOrders.empty')"
              :hint="t('restoredOrders.emptyHint')"
            />
          </ConsoleCard>
          <ConsoleCard
            :title="t('restoredOrders.spenders')"
            class="lg:col-span-2"
            ><EmptyState
              :title="t('restoredOrders.unavailable')"
              :hint="t('restoredOrders.unavailableHint')"
          /></ConsoleCard>
        </div>
      </template>
      <ConsoleCard v-else :title="t('nativePayment.records')">
        <div class="mb-4 flex flex-wrap gap-2">
          <ConsoleButton
            variant="secondary"
            :disabled="loading || !visibleRows.length || !!error"
            @click="exportOpen = true"
            >{{ t('restoredOrders.export') }}</ConsoleButton
          >
          <ConsoleButton
            variant="secondary"
            :disabled="loading"
            @click="load"
            >{{ t('nativePayment.refresh') }}</ConsoleButton
          >
        </div>
        <div class="mb-4 flex flex-wrap gap-3">
          <SearchInput
            v-model="keyword"
            class="w-full sm:w-auto sm:flex-1"
            :placeholder="t('restoredOrders.search')"
            :aria-label="t('restoredOrders.search')"
          />
          <FilterSelect
            v-model="statusFilter"
            class="w-full sm:w-auto"
            :options="statusOptions"
            :label="t('restoredOrders.status')"
            :prefix-label="t('restoredOrders.status')"
          />
          <FilterSelect
            v-model="methodFilter"
            class="w-full sm:w-auto"
            :options="methodOptions"
            :label="t('restoredOrders.method')"
            :prefix-label="t('restoredOrders.method')"
          />
        </div>
        <p class="mb-3 text-xs text-[var(--text-tertiary)]">
          {{ t('restoredOrders.amountHint') }}
        </p>
        <div class="space-y-3 md:hidden" :aria-busy="loading">
          <article
            v-for="row in visibleRows"
            :key="row.id"
            data-order-row
            class="rounded-xl border border-[var(--border-subtle)] p-4 text-sm"
          >
            <div class="flex items-start justify-between gap-3">
              <span class="break-all font-mono">{{ row.trade_no }}</span
              ><span>{{ statusLabel(row.status) }}</span>
            </div>
            <dl
              class="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2"
            >
              <template v-if="admin"
                ><dt>{{ t('restoredOrders.user') }}</dt>
                <dd>{{ row.user_id }}</dd></template
              >
              <dt>{{ t('nativePayment.quantity') }}</dt>
              <dd>{{ row.amount }}</dd>
              <dt>{{ t('nativePayment.money') }}</dt>
              <dd>{{ row.money }}</dd>
              <dt>{{ t('nativePayment.provider') }}</dt>
              <dd class="break-all">
                {{ row.payment_provider }} / {{ row.payment_method }}
              </dd>
              <dt>{{ t('nativePayment.created') }}</dt>
              <dd>{{ formatTime(row.create_time) }}</dd>
            </dl>
            <div class="mt-4 flex flex-wrap gap-2">
              <ConsoleButton variant="secondary" @click="detail = row">{{
                t('restoredOrders.detail')
              }}</ConsoleButton>
              <ConsoleButton
                v-if="admin && row.status === 'pending'"
                variant="secondary"
                @click="target = row"
                >{{ t('nativePayment.complete') }}</ConsoleButton
              >
            </div>
          </article>
        </div>
        <div class="hidden overflow-x-auto md:block" :aria-busy="loading">
          <table class="w-full min-w-[720px] text-left text-sm">
            <thead>
              <tr>
                <th>ID</th>
                <th v-if="admin">{{ t('restoredOrders.user') }}</th>
                <th>{{ t('nativePayment.quantity') }}</th>
                <th>{{ t('nativePayment.money') }}</th>
                <th>{{ t('nativePayment.provider') }}</th>
                <th>{{ t('nativePayment.status') }}</th>
                <th>{{ t('nativePayment.created') }}</th>
                <th>{{ globalT('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in visibleRows"
                :key="row.id"
                data-order-row
                class="border-t border-[var(--border-subtle)]"
              >
                <td class="py-3">
                  <span>{{ row.id }}</span>
                  <p class="break-all font-mono text-xs">{{ row.trade_no }}</p>
                </td>
                <td v-if="admin">{{ row.user_id }}</td>
                <td>{{ row.amount }}</td>
                <td>{{ row.money }}</td>
                <td>{{ row.payment_provider }} / {{ row.payment_method }}</td>
                <td>{{ statusLabel(row.status) }}</td>
                <td>{{ formatTime(row.create_time) }}</td>
                <td>
                  <div class="flex flex-wrap gap-2">
                    <ConsoleButton variant="secondary" @click="detail = row">{{
                      t('restoredOrders.detail')
                    }}</ConsoleButton>
                    <ConsoleButton
                      v-if="admin && row.status === 'pending'"
                      variant="secondary"
                      @click="target = row"
                      >{{ t('nativePayment.complete') }}</ConsoleButton
                    >
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <EmptyState
          v-if="!loading && !error && !visibleRows.length"
          :title="t('restoredOrders.empty')"
          :hint="t('restoredOrders.emptyHint')"
        />
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
        />
      </ConsoleCard>
    </div>
    <ConfirmDialog
      :open="!!target"
      :title="t('nativePayment.complete')"
      :message="t('nativePayment.confirm')"
      :loading="saving"
      @confirm="complete"
      @cancel="target = null"
    />
    <ConsoleModal
      :open="!!detail"
      :title="t('restoredOrders.detail')"
      @close="detail = null"
    >
      <dl
        v-if="detail"
        class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm"
      >
        <dt>ID</dt>
        <dd>{{ detail.id }}</dd>
        <dt>{{ t('restoredOrders.user') }}</dt>
        <dd>{{ detail.user_id }}</dd>
        <dt>{{ globalT('orders.orderNo') }}</dt>
        <dd class="break-all font-mono">{{ detail.trade_no }}</dd>
        <dt>{{ t('nativePayment.quantity') }}</dt>
        <dd>{{ detail.amount }}</dd>
        <dt>{{ t('nativePayment.money') }}</dt>
        <dd>{{ detail.money }}</dd>
        <dt>{{ t('nativePayment.provider') }}</dt>
        <dd class="break-all">
          {{ detail.payment_provider }} / {{ detail.payment_method }}
        </dd>
        <dt>{{ t('nativePayment.status') }}</dt>
        <dd>{{ statusLabel(detail.status) }}</dd>
        <dt>{{ t('nativePayment.created') }}</dt>
        <dd>{{ formatTime(detail.create_time) }}</dd>
        <dt>{{ t('restoredOrders.completed') }}</dt>
        <dd>
          {{
            detail.complete_time > 0 ? formatTime(detail.complete_time) : '--'
          }}
        </dd>
      </dl>
      <template #footer
        ><ConsoleButton variant="secondary" @click="detail = null">{{
          globalT('common.close')
        }}</ConsoleButton></template
      >
    </ConsoleModal>
    <ConsoleModal
      :open="exportOpen"
      :title="t('restoredOrders.export')"
      @close="exportOpen = false"
    >
      <p class="mb-4 text-sm text-[var(--text-secondary)]">
        {{ t('restoredOrders.exportHint') }}
      </p>
      <FilterSelect
        v-model="exportFormat"
        :options="[
          { value: 'csv', label: 'CSV' },
          { value: 'excel', label: 'Excel' },
          { value: 'json', label: 'JSON' },
        ]"
        :label="globalT('orders.exportFormat')"
      />
      <template #footer
        ><ConsoleButton
          :disabled="loading || !visibleRows.length || !!error"
          @click="exportPage"
          >{{ t('restoredOrders.export') }}</ConsoleButton
        ></template
      >
    </ConsoleModal>
  </div>
</template>
