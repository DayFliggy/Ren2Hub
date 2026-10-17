<script setup lang="ts">
import { Plus, RefreshCw, ArrowDownUp } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import ChannelActions from '@/components/console/channels/ChannelActions.vue'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import ChannelModelSyncDialog from '@/components/console/channels/ChannelModelSyncDialog.vue'
import ChannelProviderStatus from '@/components/console/channels/ChannelProviderStatus.vue'
import ChannelStatusBadge from '@/components/console/channels/ChannelStatusBadge.vue'
import ChannelInlineNumber from '@/components/console/channels/ChannelInlineNumber.vue'
import ChannelMobileList from '@/components/console/channels/ChannelMobileList.vue'
import ChannelTestModal from '@/components/console/channels/ChannelTestModal.vue'
import { useAdminChannels } from '@/composables/useAdminChannels'
import { useAuthStore } from '@/stores/auth'
import {
  ADMIN_CHANNEL_SORT_FIELDS,
  adminChannelResponseText,
  adminChannelTypeMeta,
  isRetiredChannelType,
} from '@/constants/adminChannels'
import type {
  AdminChannel,
  AdminChannelCreateInput,
  AdminChannelCreateOptions,
  AdminChannelUpdateInput,
} from '@/types/console'
import { formatMoney, formatQuota } from '@/utils/format'

const { t } = useI18n()
const auth = useAuthStore()
const canRead = computed(() => auth.hasPermission('channel', 'read'))
const canOperate = computed(() => auth.hasPermission('channel', 'operate'))
const canWrite = computed(() => auth.hasPermission('channel', 'write'))
const canSensitiveWrite = computed(() =>
  auth.hasPermission('channel', 'sensitive_write')
)
const canSelect = computed(() => canOperate.value || canSensitiveWrite.value)
const {
  rows,
  total,
  typeCounts,
  page,
  pageSize,
  keyword,
  type,
  status,
  sortBy,
  sortOrder,
  loading,
  refreshing,
  initialError,
  batchProgress,
  balanceResponse,
  isCrudBusy,
  isBulkBusy,
  canMutate,
  canRunBatch,
  load,
  isRowBusy,
  updateNumber,
  toggleStatus,
  refreshBalance,
  testChannelModel,
  removeChannelModels,
  createChannel,
  updateChannelDetails,
  deleteChannel,
  fetchUpstreamModels,
  updateChannelsStatus,
  deleteSelectedChannels,
  runVisibleBalance,
} = useAdminChannels()
const selectedIds = ref<Array<string | number>>([])
const selectedChannels = computed(() =>
  rows.value.filter((row) => selectedIds.value.includes(row.id))
)
const editing = ref<AdminChannel | null>(null)
const formOpen = ref(false)
const testing = ref<AdminChannel | null>(null)
const syncing = ref<AdminChannel | null>(null)
const provider = ref<AdminChannel | null>(null)
const deleting = ref<AdminChannel[]>([])
const columns = computed<TableColumn[]>(() => [
  { key: 'name', label: t('channels.name'), width: '210px' },
  { key: 'type', label: t('channels.type'), width: '135px' },
  { key: 'group', label: t('channels.group'), width: '120px' },
  { key: 'status', label: t('channels.status'), width: '110px' },
  { key: 'priority', label: t('channels.priority'), width: '100px' },
  { key: 'weight', label: t('channels.weight'), width: '100px' },
  { key: 'used_quota', label: t('channels.usage'), width: '110px' },
  { key: 'balance', label: t('channels.upstreamBalance'), width: '110px' },
  { key: 'response_time', label: t('channels.response'), width: '100px' },
  { key: 'actions', label: t('channels.actions'), width: '230px' },
])
const typeOptions = computed(() =>
  Object.keys(typeCounts.value).map((id) => ({
    value: id,
    label: `${adminChannelTypeMeta(Number(id)).label} (${typeCounts.value[id]})`,
  }))
)
const statusOptions = computed(() => [
  { value: 'enabled', label: t('channels.enabledOnly') },
  { value: 'disabled', label: t('channels.disabledOnly') },
])
const sortOptions = computed(() =>
  ADMIN_CHANNEL_SORT_FIELDS.map((value) => ({
    value,
    label: t(
      `channels.${value === 'response_time' ? 'response' : value === 'balance' ? 'upstreamBalance' : value}`
    ),
  }))
)
watch(rows, () => {
  selectedIds.value = []
})
watch([canRead, canOperate, canWrite, canSensitiveWrite], () => {
  if (!canRead.value) {
    formOpen.value = false
    testing.value = null
    syncing.value = null
    provider.value = null
    deleting.value = []
    selectedIds.value = []
  }
  if (!canOperate.value) {
    testing.value = null
    syncing.value = null
  }
  if (
    formOpen.value &&
    !(editing.value ? canWrite.value : canSensitiveWrite.value)
  )
    formOpen.value = false
  if (!canSensitiveWrite.value) deleting.value = []
})
function openForm(channel: AdminChannel | null) {
  if (!canMutate.value || !(channel ? canWrite.value : canSensitiveWrite.value))
    return
  editing.value = channel
  formOpen.value = true
}
function saveForm(
  input: AdminChannelCreateInput | AdminChannelUpdateInput,
  options?: AdminChannelCreateOptions
) {
  return editing.value
    ? updateChannelDetails(editing.value, input as AdminChannelUpdateInput)
    : createChannel(input as AdminChannelCreateInput, options)
}
function closeTesting() {
  testing.value = null
  void load({ background: true })
}
function toggleSelected(channel: AdminChannel) {
  if (!canSelect.value || !canMutate.value) return
  selectedIds.value = selectedIds.value.includes(channel.id)
    ? selectedIds.value.filter((id) => id !== channel.id)
    : [...selectedIds.value, channel.id]
}
async function confirmDelete() {
  if (!canSensitiveWrite.value || deleting.value.length === 0) return
  const targets = deleting.value
  const success =
    targets.length === 1
      ? await deleteChannel(targets[0]!)
      : await deleteSelectedChannels(targets)
  if (success) deleting.value = []
}
</script>

<template>
  <div class="sibling-space-y-6">
    <PageBreadcrumb :crumbs="[t('channels.title')]" />
    <template v-if="canRead">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold">{{ t('channels.title') }}</h1>
          <p class="mt-1 text-sm text-[var(--text-tertiary)]">
            {{ t('channels.resultCount', { count: total }) }}
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <ConsoleButton
            variant="secondary"
            :loading="refreshing"
            :disabled="loading"
            @click="load({ background: true })"
            ><RefreshCw :size="16" />{{
              t('channels.refreshList')
            }}</ConsoleButton
          >
          <ConsoleButton
            v-if="canOperate"
            variant="secondary"
            :disabled="!canRunBatch"
            @click="runVisibleBalance"
            >{{ t('channels.batchBalancePage') }}</ConsoleButton
          >
          <ConsoleButton
            v-if="canSensitiveWrite"
            :disabled="!canMutate"
            @click="openForm(null)"
            ><Plus :size="16" />{{ t('channels.createChannel') }}</ConsoleButton
          >
        </div>
      </div>
      <div class="flex flex-wrap gap-3">
        <SearchInput
          v-model="keyword"
          :placeholder="t('channels.searchPlaceholder')"
          class="min-w-0 flex-1"
        />
        <FilterSelect
          v-model="type"
          :options="typeOptions"
          :label="t('channels.typeFilter')"
          :placeholder="t('channels.allTypes')"
        />
        <FilterSelect
          v-model="status"
          :options="statusOptions"
          :label="t('channels.statusFilter')"
          :placeholder="t('channels.allStatuses')"
        />
        <FilterSelect
          v-model="sortBy"
          :options="sortOptions"
          :label="t('channels.sortLabel')"
        />
        <ConsoleButton
          variant="secondary"
          :aria-label="
            sortOrder === 'asc'
              ? t('channels.sortDescending')
              : t('channels.sortAscending')
          "
          @click="sortOrder = sortOrder === 'asc' ? 'desc' : 'asc'"
          ><ArrowDownUp :size="16" />{{
            sortOrder === 'asc'
              ? t('channels.sortAscending')
              : t('channels.sortDescending')
          }}</ConsoleButton
        >
      </div>
      <p v-if="batchProgress" role="status" class="text-sm">
        {{
          t('channels.batchProgressLabel', {
            action: t('channels.batchBalanceAction'),
            processed: batchProgress.processed,
            total: batchProgress.total,
          })
        }}
      </p>
      <div
        v-if="selectedChannels.length"
        class="flex flex-wrap items-center gap-2"
      >
        <span class="text-sm">{{
          t('channels.selectedCount', { count: selectedChannels.length })
        }}</span>
        <ConsoleButton
          v-if="canOperate"
          variant="secondary"
          :disabled="!canMutate"
          @click="updateChannelsStatus('enable', selectedChannels)"
          >{{ t('channels.bulkEnable') }}</ConsoleButton
        >
        <ConsoleButton
          v-if="canOperate"
          variant="secondary"
          :disabled="!canMutate"
          @click="updateChannelsStatus('disable', selectedChannels)"
          >{{ t('channels.bulkDisable') }}</ConsoleButton
        >
        <ConsoleButton
          v-if="canOperate"
          variant="secondary"
          :disabled="!canMutate"
          @click="updateChannelsStatus('reset', selectedChannels)"
          >{{ t('channels.bulkReset') }}</ConsoleButton
        >
        <ConsoleButton
          v-if="canSensitiveWrite"
          variant="danger"
          :disabled="!canMutate"
          @click="deleting = selectedChannels"
          >{{ t('channels.bulkDelete') }}</ConsoleButton
        >
        <ConsoleButton variant="ghost" @click="selectedIds = []">{{
          t('channels.clearSelection')
        }}</ConsoleButton>
      </div>
      <div
        v-if="initialError"
        role="alert"
        class="rounded-xl border border-[var(--status-danger)] p-4"
      >
        <p>{{ initialError }}</p>
        <ConsoleButton variant="secondary" @click="load()">{{
          t('common.retry')
        }}</ConsoleButton>
      </div>
      <ConsoleCard v-else class="overflow-hidden">
        <DataTable
          v-model:selected="selectedIds"
          class="hidden md:block"
          :columns="columns"
          :rows="rows"
          row-key="id"
          :loading="loading"
          :selectable="canSelect"
          :selection-disabled="!canMutate"
          :row-selection-label="
            (row) => t('channels.selectChannel', { name: row.name })
          "
          :page-size="pageSize"
          adaptive-scroll
          min-table-width="1300px"
        >
          <template #cell-name="{ row }"
            ><span class="font-medium">{{ row.name }}</span
            ><span class="ml-2 text-xs text-[var(--text-tertiary)]"
              >#{{ row.id }}</span
            ></template
          >
          <template #cell-type="{ row }">{{
            adminChannelTypeMeta(row.type).label
          }}</template>
          <template #cell-status="{ row }"
            ><ChannelStatusBadge :channel="row"
          /></template>
          <template #cell-priority="{ row }"
            ><ChannelInlineNumber
              :value="row.priority"
              :label="t('channels.priorityFor', { name: row.name })"
              :busy="!canWrite || !canMutate || isRetiredChannelType(row.type)"
              :commit="(value) => updateNumber(row, 'priority', value)"
          /></template>
          <template #cell-weight="{ row }"
            ><ChannelInlineNumber
              :value="row.weight"
              :label="t('channels.weightFor', { name: row.name })"
              :busy="!canWrite || !canMutate || isRetiredChannelType(row.type)"
              :commit="(value) => updateNumber(row, 'weight', value)"
          /></template>
          <template #cell-used_quota="{ row }">{{
            formatQuota(row.used_quota)
          }}</template>
          <template #cell-balance="{ row }">{{
            formatMoney(row.balance)
          }}</template>
          <template #cell-response_time="{ row }">{{
            adminChannelResponseText(row.response_time, t('channels.notTested'))
          }}</template>
          <template #cell-actions="{ row }"
            ><ChannelActions
              :channel="row"
              :can-operate="canOperate"
              :can-write="canWrite"
              :can-sensitive-write="canSensitiveWrite"
              :busy="isRowBusy(row.id) || !canMutate"
              @test="testing = row"
              @balance="refreshBalance(row)"
              @status="toggleStatus(row)"
              @edit="openForm(row)"
              @delete="deleting = [row]"
              @sync="syncing = row"
              @provider="provider = row"
          /></template>
        </DataTable>
        <div class="md:hidden">
          <p v-if="loading" role="status" class="p-4">
            {{ t('common.loading') }}
          </p>
          <EmptyState v-else-if="!rows.length" :title="t('common.none')" />
          <ChannelMobileList
            v-else
            :rows="rows"
            :selected="selectedIds.map(Number)"
            :selectable="canSelect"
            :disabled="!canMutate"
            @select="toggleSelected"
          >
            <template #actions="{ channel }"
              ><ChannelActions
                :channel="channel"
                :can-operate="canOperate"
                :can-write="canWrite"
                :can-sensitive-write="canSensitiveWrite"
                :busy="isRowBusy(channel.id) || !canMutate"
                @test="testing = channel"
                @balance="refreshBalance(channel)"
                @status="toggleStatus(channel)"
                @edit="openForm(channel)"
                @delete="deleting = [channel]"
                @sync="syncing = channel"
                @provider="provider = channel"
            /></template>
          </ChannelMobileList>
        </div>
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50]"
        />
      </ConsoleCard>
    </template>
    <ChannelFormModal
      :open="formOpen"
      :editing="editing"
      :save="saveForm"
      :fetch-models="fetchUpstreamModels"
      @close="formOpen = false"
    />
    <ChannelTestModal
      :open="testing !== null"
      :channel="testing"
      :can-write="canWrite"
      :test-model="testChannelModel"
      :remove-models="removeChannelModels"
      @close="closeTesting"
    />
    <ChannelModelSyncDialog
      :channel="syncing"
      :can-write="canWrite"
      @close="syncing = null"
      @applied="load({ background: true })"
    />
    <ConfirmDialog
      :open="deleting.length > 0"
      :title="t('channels.deleteChannel')"
      :message="deleting.map((channel) => channel.name).join(', ')"
      :loading="isCrudBusy || isBulkBusy"
      @confirm="confirmDelete"
      @cancel="deleting = []"
    />
    <ConsoleModal
      :open="balanceResponse !== null"
      :title="balanceResponse?.channelName ?? ''"
      @close="balanceResponse = null"
    >
      <pre class="overflow-auto whitespace-pre-wrap break-all text-sm">{{
        balanceResponse?.rawResponse
      }}</pre>
    </ConsoleModal>
    <ConsoleModal
      :open="provider !== null"
      :title="t('channels.providerStatus')"
      @close="provider = null"
      ><ChannelProviderStatus v-if="provider" :channel="provider"
    /></ConsoleModal>
  </div>
</template>
