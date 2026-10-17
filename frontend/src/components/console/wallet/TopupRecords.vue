<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
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
import { nativePaymentMessages } from '@/i18n/nativePayment'
const props = withDefaults(
  defineProps<{ refreshKey?: number; admin?: boolean }>(),
  { refreshKey: 0, admin: false }
)
const { t } = useI18n({ messages: nativePaymentMessages }),
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
    rows.value = result.value.items
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
const statusLabel = (value: string) =>
  ['pending', 'success', 'failed', 'expired'].includes(value)
    ? t(`nativePayment.${value}`)
    : value
</script>
<template>
  <ConsoleCard :title="t('nativePayment.records')"
    ><template #action
      ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('nativePayment.refresh')
      }}</ConsoleButton></template
    ><SearchInput v-model="keyword" class="mb-4" /><ErrorBanner
      v-if="error"
      :message="error" />
    <div class="overflow-x-auto">
      <table class="w-full min-w-[640px] text-left text-sm">
        <thead>
          <tr>
            <th>ID</th>
            <th>{{ t('nativePayment.quantity') }}</th>
            <th>{{ t('nativePayment.money') }}</th>
            <th>{{ t('nativePayment.provider') }}</th>
            <th>{{ t('nativePayment.status') }}</th>
            <th>{{ t('nativePayment.created') }}</th>
            <th v-if="admin" />
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.id"
            class="border-t border-[var(--border-subtle)]"
          >
            <td class="py-3">
              <span>{{ row.id }}</span>
              <p class="break-all font-mono text-xs">{{ row.trade_no }}</p>
            </td>
            <td>{{ row.amount }}</td>
            <td>{{ row.money }}</td>
            <td>{{ row.payment_provider }} / {{ row.payment_method }}</td>
            <td>{{ statusLabel(row.status) }}</td>
            <td>{{ formatTime(row.create_time) }}</td>
            <td v-if="admin">
              <ConsoleButton
                v-if="row.status === 'pending'"
                variant="secondary"
                @click="target = row"
                >{{ t('nativePayment.complete') }}</ConsoleButton
              >
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <TablePagination
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="total" /><ConfirmDialog
      :open="!!target"
      :title="t('nativePayment.complete')"
      :message="t('nativePayment.confirm')"
      :loading="saving"
      @confirm="complete"
      @cancel="target = null"
  /></ConsoleCard>
</template>
