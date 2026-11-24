<script setup lang="ts">
import ChannelBalanceDetails from '@/features/channels/components/ChannelBalanceDetails.vue'
import BalanceUpdateTask from '@/features/channels/components/BalanceUpdateTask.vue'
import { useStorage } from '@vueuse/core'
import {
  Activity,
  Gauge,
  AlertTriangle,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  ChevronDown,
  ChevronRight,
  LoaderCircle,
  Pencil,
  Plus,
  Power,
  PowerOff,
  Scale,
  RefreshCw,
  RotateCcw,
  Trash2,
  X,
} from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import CapacityMeter from '@/components/common/CapacityMeter.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import FieldVisibilityMenu from '@/components/common/FieldVisibilityMenu.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import ChannelModelSyncDialog from '@/components/console/channels/ChannelModelSyncDialog.vue'
import ChannelProviderStatus from '@/components/console/channels/ChannelProviderStatus.vue'
import ChannelStatusBadge from '@/components/console/channels/ChannelStatusBadge.vue'
import ChannelInlineNumber from '@/components/console/channels/ChannelInlineNumber.vue'
import ChannelMobileList from '@/components/console/channels/ChannelMobileList.vue'
import ChannelTestModal from '@/components/console/channels/ChannelTestModal.vue'
import ChannelTestPickerDialog from '@/components/console/channels/ChannelTestPickerDialog.vue'
import ChannelUpstreamRatioDialog from '@/components/console/channels/ChannelUpstreamRatioDialog.vue'
import UpstreamRatioSummary from '@/components/console/channels/UpstreamRatioSummary.vue'
import ChannelUpstreamBindingBadge from '@/components/console/channels/ChannelUpstreamBindingBadge.vue'
import { useChannelUpstreamBindings } from '@/composables/useChannelUpstreamBindings'
import { useChannelUpstreamRatios } from '@/composables/useChannelUpstreamRatios'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import {
  type ChannelModelTestResult,
  useAdminChannels,
} from '@/composables/useAdminChannels'
import { useAuthStore } from '@/stores/auth'
import {
  ADMIN_CHANNEL_DEFAULT_VISIBLE_FIELDS,
  ADMIN_CHANNEL_OPTIONAL_FIELDS,
  ADMIN_CHANNEL_SORT_FIELDS,
  ADMIN_CHANNEL_VISIBLE_FIELDS_STORAGE_KEY,
  type AdminChannelOptionalField,
  adminChannelResponseText,
  adminChannelResponseTone,
  adminChannelTypeMeta,
  canEditChannel,
  type ChannelLabGroup,
  groupChannelsByLab,
  hasInvalidModelMapping,
  isRetiredChannelType,
  labDisplayName,
  sanitizeAdminChannelVisibleFields,
  supportsBalanceQuery,
} from '@/constants/adminChannels'
import type {
  AdminChannel,
  AdminChannelCreateInput,
  AdminChannelCreateOptions,
  AdminChannelSortBy,
  AdminChannelSortOrder,
  AdminChannelUpdateInput,
} from '@/types/console'
import { formatQuota, relativeTime } from '@/utils/format'
import { formatDecimal } from '@/utils/decimal'

interface SupplierTableRow extends Record<string, unknown> {
  key: string | number
  kind: 'supplier'
  groupSlug: string
  supplier: string
  count: number
  channels: AdminChannel[]
}

interface ChannelTableRow extends Record<string, unknown> {
  key: number
  kind: 'channel'
  channel: AdminChannel
}

type AdminChannelTableRow = SupplierTableRow | ChannelTableRow
type DeleteTarget =
  | { kind: 'channel'; channel: AdminChannel }
  | {
      kind: 'supplier'
      supplier: string
      groupSlug: string
      channels: AdminChannel[]
    }
  | { kind: 'bulk'; channels: AdminChannel[] }

const { t, locale } = useI18n()
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
  searchText,
  searchScope,
  type,
  lab,
  labs,
  status,
  hasFilters,
  resetFilters,
  sortBy,
  sortOrder,
  loading,
  refreshing,
  initialError,
  batchProgress,
  balanceResponse,
  isBatchBusy,
  isCrudBusy,
  isBulkBusy,
  canMutate,
  canRunBatch,
  load,
  isBusy,
  isRowBusy,
  isCrudActionBusy,
  isBulkActionBusy,
  updateNumber,
  toggleStatus,
  refreshBalance,
  testChannelModel,
  removeChannelModels,
  createChannel,
  updateChannelDetails,
  deleteChannel,
  deleteSupplierChannels,
  fetchUpstreamModels,
  updateChannelsStatus,
  deleteSelectedChannels,
  runVisibleBalance,
  runSupplierBalance,
} = useAdminChannels()
const { ratios: upstreamRatios, update: updateUpstreamRatio } =
  useChannelUpstreamRatios(rows, canRead)
const editingUpstreamRatio = ref<AdminChannel | null>(null)

const route = useRoute()
const router = useRouter()
const canReadUpstream = computed(() => auth.hasPermission('upstream', 'read'))
const { bindings: upstreamBindings } = useChannelUpstreamBindings(
  rows,
  canReadUpstream
)
// Client-side filter over the loaded page; /api/channel/ params stay unchanged.
const upstreamAccountFilter = computed({
  get() {
    const raw = route.query.upstream_account
    return canReadUpstream.value &&
      typeof raw === 'string' &&
      /^[1-9]\d*$/.test(raw)
      ? raw
      : ''
  },
  set(value: string) {
    const query = { ...route.query }
    if (value) query.upstream_account = value
    else delete query.upstream_account
    void router.replace({ query })
  },
})
const upstreamAccountOptions = computed(() => {
  const ids = new Set(
    Object.values(upstreamBindings.value).map((item) => item.account_id)
  )
  if (upstreamAccountFilter.value) ids.add(Number(upstreamAccountFilter.value))
  return [
    { value: '', label: t('upstreams.channelLink.allAccounts') },
    ...[...ids]
      .sort((left, right) => left - right)
      .map((id) => ({
        value: String(id),
        label: t('upstreams.channelLink.account', { id }),
      })),
  ]
})
const visibleRows = computed(() => {
  const account = Number(upstreamAccountFilter.value)
  if (!account) return rows.value
  return rows.value.filter(
    (channel) => upstreamBindings.value[channel.id]?.account_id === account
  )
})
function upstreamBindingFor(channel: AdminChannel | null) {
  return channel ? (upstreamBindings.value[channel.id] ?? null) : null
}

const formOpen = ref(false)
const editing = ref<AdminChannel | null>(null)
const runtimeChannel = ref<AdminChannel | null>(null)
watch(canRead, (allowed) => {
  if (!allowed) runtimeChannel.value = null
})
function openRuntime(channel: AdminChannel) {
  if (isReadOnlyChannel(channel)) return
  if (canRead.value) runtimeChannel.value = channel
}
const deleting = ref<DeleteTarget | null>(null)
const testing = ref<AdminChannel | null>(null)
const pickingGroup = ref<ChannelLabGroup | null>(null)
/** Per-channel batch-test results shown in the response column until the
    background reload replaces them with the persisted values. */
const groupTestOverrides = ref<
  Record<number, ChannelModelTestResult | 'running'>
>({})
const selectedIds = ref<number[]>([])
const collapsedSuppliers = ref<string[]>([])
const storedVisibleFields = useStorage<string[]>(
  ADMIN_CHANNEL_VISIBLE_FIELDS_STORAGE_KEY,
  [...ADMIN_CHANNEL_DEFAULT_VISIBLE_FIELDS]
)

watch(
  storedVisibleFields,
  (fields) => {
    const sanitized = sanitizeAdminChannelVisibleFields(fields)
    if (
      sanitized.length !== fields.length ||
      sanitized.some((field, index) => field !== fields[index])
    ) {
      storedVisibleFields.value = sanitized
    }
  },
  { immediate: true }
)

const visibleFields = computed<AdminChannelOptionalField[]>({
  get: () => sanitizeAdminChannelVisibleFields(storedVisibleFields.value),
  set: (fields) => {
    storedVisibleFields.value = sanitizeAdminChannelVisibleFields(fields)
  },
})

function isReadOnlyChannel(channel: AdminChannel): boolean {
  return isRetiredChannelType(channel.type)
}

const mutableRows = computed(() =>
  visibleRows.value.filter((channel) => !isReadOnlyChannel(channel))
)
const selectedChannels = computed(() =>
  mutableRows.value.filter((channel) => selectedIds.value.includes(channel.id))
)
const pageChannelIds = computed(() =>
  mutableRows.value.map((channel) => channel.id)
)
const allPageSelected = computed(
  () =>
    mutableRows.value.length > 0 &&
    mutableRows.value.every((channel) => selectedIds.value.includes(channel.id))
)
const resettableSelectedCount = computed(
  () => selectedChannels.value.filter((channel) => channel.status === 3).length
)

function updateSelected(keys: Array<string | number>) {
  selectedIds.value = keys.filter(
    (key): key is number =>
      typeof key === 'number' &&
      mutableRows.value.some((channel) => channel.id === key)
  )
}

function toggleSelected(channel: AdminChannel) {
  if (isReadOnlyChannel(channel)) return
  selectedIds.value = selectedIds.value.includes(channel.id)
    ? selectedIds.value.filter((id) => id !== channel.id)
    : [...selectedIds.value, channel.id]
}

function toggleAllSelected() {
  selectedIds.value = allPageSelected.value
    ? []
    : mutableRows.value.map((channel) => channel.id)
}

function clearSelection() {
  selectedIds.value = []
}

watch(rows, (nextRows) => {
  const ids = new Set(nextRows.map((channel) => channel.id))
  selectedIds.value = selectedIds.value.filter((id) => ids.has(id))
})

function isFieldVisible(field: AdminChannelOptionalField): boolean {
  return visibleFields.value.includes(field)
}

/** Field labels for the visibility menu; a few keys don't match 1:1. */
function fieldLabel(field: AdminChannelOptionalField): string {
  if (field === 'usage') return t('channels.usageAndRatio')
  if (field === 'upstream') return t('channels.upstreamAndRatio')
  if (field === 'rowUpstreamAction') return t('channels.rowUpstreamAction')
  if (field === 'rowResponseAction') return t('channels.rowResponseAction')
  return t(`channels.${field}`)
}

const allColumns = computed<
  Array<TableColumn & { optional?: AdminChannelOptionalField }>
>(() => [
  { key: 'id', label: t('channels.id'), width: '72px', optional: 'id' },
  { key: 'name', label: t('channels.name'), width: '190px' },
  {
    key: 'type',
    label: t('channels.type'),
    width: '145px',
    optional: 'type',
  },
  {
    key: 'status',
    label: t('channels.status'),
    width: '118px',
    optional: 'status',
  },
  {
    key: 'priority',
    label: t('channels.priority'),
    width: '96px',
    optional: 'priority',
  },
  {
    key: 'weight',
    label: t('channels.weight'),
    width: '96px',
    optional: 'weight',
  },
  {
    key: 'capacity',
    label: t('channels.capacity'),
    width: '130px',
    optional: 'capacity',
  },
  {
    key: 'usage',
    label: t('channels.usageAndRatio'),
    width: '158px',
    optional: 'usage',
  },
  {
    key: 'upstream',
    label: t('channels.upstreamAndRatio'),
    width: '178px',
    optional: 'upstream',
  },
  {
    key: 'response',
    label: t('channels.response'),
    width: '138px',
    optional: 'response',
  },
  {
    key: 'actions',
    label: t('channels.actions'),
    width: '120px',
    align: 'right',
  },
])

const columns = computed<TableColumn[]>(() =>
  allColumns.value.filter(
    (column) => !column.optional || isFieldVisible(column.optional)
  )
)

const minTableWidth = computed(() => {
  const width = columns.value.reduce(
    (totalWidth, column) =>
      totalWidth + Number.parseInt(column.width ?? '120', 10),
    0
  )
  return `${Math.max(860, width + 40)}px`
})

const supplierGroups = computed<ChannelLabGroup[]>(() =>
  groupChannelsByLab(visibleRows.value)
)

const tableRows = computed<AdminChannelTableRow[]>(() =>
  supplierGroups.value.flatMap((group) => {
    const groupRow: SupplierTableRow = {
      key: `lab:${group.groupSlug}`,
      kind: 'supplier',
      groupSlug: group.groupSlug,
      supplier: group.supplier,
      count: group.channels.length,
      channels: group.channels,
    }
    if (collapsedSuppliers.value.includes(group.supplier)) return [groupRow]
    return [
      groupRow,
      ...group.channels.map<ChannelTableRow>((channel) => ({
        key: channel.id,
        kind: 'channel',
        channel,
      })),
    ]
  })
)

const typeOptions = computed(() => {
  const ids = Object.entries(typeCounts.value)
    .filter(([, count]) => count > 0)
    .map(([id]) => Number(id))
  const selected = Number(type.value)
  if (type.value && !ids.includes(selected)) ids.push(selected)
  return [
    { value: '', label: t('channels.allTypes') },
    ...ids
      .sort((left, right) =>
        adminChannelTypeMeta(left).label.localeCompare(
          adminChannelTypeMeta(right).label
        )
      )
      .map((id) => ({
        value: String(id),
        label: adminChannelTypeMeta(id).label,
        count: typeCounts.value[id] ?? 0,
      })),
  ]
})

// The unknown and mixed labs arrive as lowercase ids; labDisplayName names them.
const labOptions = computed(() => [
  { value: '', label: t('channels.allLabs') },
  ...labs.value.map((item) => ({
    value: item.id,
    label: labDisplayName(item),
    count: item.count,
  })),
])

const statusOptions = computed(() => [
  { value: '', label: t('channels.allStatuses') },
  {
    value: 'enabled',
    label: t('channels.enabledOnly'),
    tone: 'success' as const,
  },
  {
    value: 'disabled',
    label: t('channels.disabledOnly'),
    tone: 'danger' as const,
  },
])

const searchScopeOptions = computed(() => [
  { value: 'channel', label: t('channels.searchScopeChannel') },
  { value: 'model', label: t('channels.searchScopeModel') },
])
const searchScopeModel = computed({
  get: () => searchScope.value,
  set: (value: string) => {
    searchScope.value = value === 'model' ? 'model' : 'channel'
  },
})
const searchPlaceholder = computed(() =>
  searchScope.value === 'model'
    ? t('channels.modelSearchPlaceholder')
    : t('channels.searchPlaceholder')
)

const sortOptions = computed(() =>
  ADMIN_CHANNEL_SORT_FIELDS.map((field) => ({
    value: field,
    label: t(`channels.sort.${field}`),
  }))
)

const sortModel = computed({
  get: () => sortBy.value,
  set: (value: string) => {
    sortBy.value = value as AdminChannelSortBy
  },
})

function toggleSortOrder() {
  sortOrder.value = (
    sortOrder.value === 'asc' ? 'desc' : 'asc'
  ) as AdminChannelSortOrder
}

function isSupplierRow(row: AdminChannelTableRow): boolean {
  return row.kind === 'supplier'
}

function isSelectableRow(row: AdminChannelTableRow): boolean {
  return row.kind === 'channel' && !isReadOnlyChannel(row.channel)
}

function hasMutableChannels(channels: AdminChannel[]): boolean {
  return channels.some((channel) => !isReadOnlyChannel(channel))
}

function channelFromRow(row: AdminChannelTableRow): AdminChannel {
  if (row.kind !== 'channel') throw new Error('Expected a channel table row')
  return row.channel
}

function channelRowClass(row: AdminChannelTableRow): string | undefined {
  if (row.kind !== 'channel') return undefined
  if (row.channel.status === 1 && !isReadOnlyChannel(row.channel))
    return undefined
  return 'opacity-75'
}

function isSupplierCollapsed(supplier: string): boolean {
  return collapsedSuppliers.value.includes(supplier)
}

function toggleSupplier(supplier: string) {
  collapsedSuppliers.value = collapsedSuppliers.value.includes(supplier)
    ? collapsedSuppliers.value.filter((item) => item !== supplier)
    : [...collapsedSuppliers.value, supplier]
}

function balanceButtonLabel(): string {
  if (batchProgress.value?.scope === 'page') {
    return t('channels.batchProgressLabel', {
      action: t('channels.batchBalanceAction'),
      processed: batchProgress.value.processed,
      total: batchProgress.value.total,
    })
  }
  return t('channels.batchBalanceLabel', {
    count: rows.value.filter(supportsBalanceQuery).length,
  })
}

function isPageBalance(): boolean {
  return batchProgress.value?.scope === 'page'
}

function isSupplierBalance(supplier: string): boolean {
  return (
    batchProgress.value?.scope === 'supplier' &&
    batchProgress.value.supplier === supplier
  )
}

function batchProgressText(): string {
  const progress = batchProgress.value
  if (!progress) return ''
  return `${progress.processed}/${progress.total}`
}

function supplierBalanceLabel(supplier: string): string {
  if (isSupplierBalance(supplier) && batchProgress.value) {
    return t('channels.batchProgressLabel', {
      action: t('channels.batchBalanceAction'),
      processed: batchProgress.value.processed,
      total: batchProgress.value.total,
    })
  }
  return t('channels.syncSupplier', { supplier })
}

function openTest(channel: AdminChannel) {
  if (!canOperate.value || isReadOnlyChannel(channel)) return
  testing.value = channel
}

function openSupplierTest(group: ChannelLabGroup) {
  if (!canOperate.value) return
  groupTestOverrides.value = {}
  const channels = group.channels.filter(
    (channel) => !isReadOnlyChannel(channel)
  )
  if (channels.length === 0) return
  pickingGroup.value = { ...group, channels }
}

function closeSupplierTest() {
  pickingGroup.value = null
  groupTestOverrides.value = {}
}

function onGroupTestStart(channelId: number) {
  groupTestOverrides.value = {
    ...groupTestOverrides.value,
    [channelId]: 'running',
  }
}

function onGroupTestResult(channelId: number, result: ChannelModelTestResult) {
  groupTestOverrides.value = {
    ...groupTestOverrides.value,
    [channelId]: result,
  }
}

function onGroupTested() {
  groupTestOverrides.value = {}
  void load({ background: true })
}

/** Response cell content: batch-test override first, persisted value after. */
function responseCellDisplay(channel: AdminChannel): {
  tone: 'neutral' | 'success' | 'warning' | 'danger'
  text: string
  title: string
  running: boolean
} {
  const override = groupTestOverrides.value[channel.id]
  if (override === 'running') {
    return {
      tone: 'warning',
      text: t('channels.testStatusRunning'),
      title: t('channels.testStatusRunning'),
      running: true,
    }
  }
  if (override) {
    if (override.ok) {
      return {
        tone: 'success',
        text: adminChannelResponseText(
          override.timeMs ?? 0,
          t('channels.notTested')
        ),
        title: t('channels.testStatusSuccess'),
        running: false,
      }
    }
    return {
      tone: 'danger',
      text: t('channels.testStatusFailed'),
      title: override.message ?? t('channels.testStatusFailed'),
      running: false,
    }
  }
  return {
    tone: adminChannelResponseTone(channel.response_time),
    text: adminChannelResponseText(
      channel.response_time,
      t('channels.notTested')
    ),
    title: channel.test_time
      ? t('channels.testedAt', {
          time: relativeTime(channel.test_time, locale.value),
        })
      : t('channels.notTested'),
    running: false,
  }
}

function openCreate() {
  if (!canMutate.value || !canSensitiveWrite.value) return
  editing.value = null
  formOpen.value = true
}

function openEdit(channel: AdminChannel) {
  if (!canMutate.value || !canEditChannel(channel, canWrite.value)) return
  editing.value = channel
  formOpen.value = true
}
const syncingModels = ref<AdminChannel | null>(null)
function previewModelSync(channel: AdminChannel) {
  if (isReadOnlyChannel(channel)) return
  if (canOperate.value && canMutate.value) syncingModels.value = channel
}

function closeForm() {
  formOpen.value = false
}

function saveForm(
  input: AdminChannelCreateInput | AdminChannelUpdateInput,
  options?: AdminChannelCreateOptions
): Promise<boolean> {
  if (editing.value ? !canWrite.value : !canSensitiveWrite.value) {
    return Promise.resolve(false)
  }
  return editing.value
    ? updateChannelDetails(editing.value, input as AdminChannelUpdateInput)
    : createChannel(input as AdminChannelCreateInput, options)
}

function requestDelete(channel: AdminChannel) {
  if (!canMutate.value || !canSensitiveWrite.value) return
  deleting.value = { kind: 'channel', channel }
}

function requestBulkDelete() {
  if (
    !canMutate.value ||
    !canSensitiveWrite.value ||
    selectedChannels.value.length === 0
  )
    return
  deleting.value = {
    kind: 'bulk',
    channels: selectedChannels.value.map((channel) => ({ ...channel })),
  }
}

function requestClearSupplier(group: ChannelLabGroup) {
  if (!canMutate.value || !canSensitiveWrite.value) return
  const channels = group.channels.filter(
    (channel) => !isReadOnlyChannel(channel)
  )
  if (channels.length === 0) return
  deleting.value = {
    kind: 'supplier',
    supplier: group.supplier,
    groupSlug: group.groupSlug,
    channels: channels.map((channel) => ({ ...channel })),
  }
}

async function confirmDelete() {
  const target = deleting.value
  if (!target || !canSensitiveWrite.value) return
  const deleted =
    target.kind === 'channel'
      ? await deleteChannel(target.channel)
      : target.kind === 'supplier'
        ? await deleteSupplierChannels(
            target.supplier,
            target.channels,
            target.groupSlug
          )
        : await deleteSelectedChannels(target.channels)
  if (deleted) {
    deleting.value = null
    clearSelection()
  }
}

function cancelDelete() {
  if (!isCrudActionBusy('delete') && !isBulkActionBusy('delete')) {
    deleting.value = null
  }
}

const deleteDialogTitle = computed(() =>
  deleting.value?.kind === 'supplier'
    ? t('channels.clearSupplierTitle')
    : deleting.value?.kind === 'bulk'
      ? t('channels.bulkDeleteTitle')
      : t('channels.deleteTitle')
)

const deleteDialogMessage = computed(() => {
  const target = deleting.value
  if (!target) return ''
  if (target.kind === 'channel') {
    return t('channels.deleteMessage', { name: target.channel.name })
  }
  if (target.kind === 'bulk') {
    return t('channels.bulkDeleteMessage', { count: target.channels.length })
  }
  return t('channels.clearSupplierMessage', {
    supplier: target.supplier,
    count: target.channels.length,
    ids: channelIdSummary(target.channels),
  })
})

function channelIdSummary(channels: AdminChannel[]): string {
  const ids = channels.map((channel) => channel.id).sort((a, b) => a - b)
  if (ids.length === 0) return ''
  if (ids.length === 1) return `#${ids[0]}`
  return `#${ids[0]} - #${ids[ids.length - 1]}`
}

async function runBulkStatus(
  action: 'enable' | 'disable' | 'reset'
): Promise<void> {
  if (!canMutate.value || !canOperate.value) return
  if (await updateChannelsStatus(action, selectedChannels.value)) {
    clearSelection()
  }
}
function routeQualitySummary(row: AdminChannelTableRow) {
  const quality = channelFromRow(row).routing_quality
  return quality
    ? t('channels.qualitySummary', {
        ...quality,
        reason: t('channels.qualityReasons.' + quality.reason),
      })
    : ''
}
</script>

<template>
  <div>
    <ConsoleCard :padded="false">
      <!-- Toolbar row as in NewAPI: searches and filter chips on the left,
        list actions on the right. -->
      <div
        class="flex flex-wrap items-center gap-2 border-b border-[var(--border-subtle)] p-4"
        data-channel-toolbar
      >
        <!-- One search box; the scope picks what the native search matches. -->
        <div
          class="flex w-full min-w-0 items-center gap-2 sm:w-auto sm:max-w-[28rem] sm:grow sm:basis-72"
          data-channel-search
        >
          <FilterSelect
            v-model="searchScopeModel"
            :options="searchScopeOptions"
            :label="t('channels.searchScope')"
            size="compact"
            class="w-24 shrink-0"
          />
          <SearchInput
            v-model="searchText"
            :placeholder="searchPlaceholder"
            :aria-label="searchPlaceholder"
            name="admin-channel-search"
            size="compact"
            class="min-w-0 grow"
          />
        </div>
        <div class="flex flex-wrap items-center gap-2" data-channel-facets>
          <FilterSelect
            v-model="status"
            :options="statusOptions"
            :label="t('channels.statusFilter')"
            :prefix-label="t('channels.status')"
            variant="facet"
            size="compact"
          />
          <FilterSelect
            v-model="type"
            :options="typeOptions"
            :label="t('channels.typeFilter')"
            :prefix-label="t('channels.type')"
            variant="facet"
            size="compact"
          />
          <FilterSelect
            v-model="lab"
            :options="labOptions"
            :label="t('channels.labFilter')"
            :prefix-label="t('channels.lab')"
            variant="facet"
            size="compact"
          />
          <FilterSelect
            v-if="canReadUpstream && upstreamAccountOptions.length > 1"
            v-model="upstreamAccountFilter"
            :options="upstreamAccountOptions"
            :label="t('upstreams.channelLink.filter')"
            :prefix-label="t('upstreams.channelLink.filter')"
            variant="facet"
            size="compact"
            data-channel-upstream-filter
          />
          <ConsoleButton
            v-if="hasFilters"
            variant="ghost"
            size="sm"
            @click="resetFilters"
          >
            {{ t('channels.resetFilters') }}
            <X :size="14" aria-hidden="true" />
          </ConsoleButton>
        </div>
        <div
          class="ml-auto flex w-full items-center gap-2 md:w-auto"
          data-channel-toolbar-actions
        >
          <ConsoleButton
            v-if="canOperate"
            variant="secondary"
            size="sm"
            class="min-w-0 grow md:hidden"
            :disabled="!canRunBatch"
            :aria-label="balanceButtonLabel()"
            @click="runVisibleBalance()"
          >
            <LoaderCircle
              v-if="isPageBalance()"
              :size="14"
              class="animate-spin"
            />
            <RefreshCw v-else :size="14" />
            <span class="min-w-0 truncate">
              {{
                isPageBalance()
                  ? batchProgressText()
                  : t('channels.batchBalancePage')
              }}
            </span>
          </ConsoleButton>
          <BalanceUpdateTask
            v-if="canOperate"
            @complete="load({ background: true })"
          />
          <IconButton
            :label="t('channels.refreshList')"
            class="pencil-control ml-auto shrink-0 rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)]"
            data-handdrawn="control"
            :aria-busy="refreshing"
            :disabled="refreshing || isBatchBusy || isCrudBusy || isBulkBusy"
            @click="load({ background: true })"
          >
            <RefreshCw :size="16" :class="refreshing ? 'animate-spin' : ''" />
          </IconButton>
          <FieldVisibilityMenu
            v-model="visibleFields"
            :all-fields="ADMIN_CHANNEL_OPTIONAL_FIELDS"
            :default-fields="ADMIN_CHANNEL_DEFAULT_VISIBLE_FIELDS"
            :label-for="fieldLabel"
            :title="t('channels.fieldSettings')"
            :reset-label="t('channels.resetFields')"
          />
        </div>
        <div
          class="flex w-full items-center gap-2 md:w-auto"
          data-channel-header-actions
        >
          <span
            class="mr-auto whitespace-nowrap text-xs tabular-nums text-[var(--text-tertiary)] md:hidden 2xl:mr-1 2xl:inline"
            aria-live="polite"
          >
            {{ t('channels.resultCount', { count: total }) }}
          </span>
          <!-- Sort sits beside create as in NewAPI's channel header. -->
          <FilterSelect
            v-model="sortModel"
            :options="sortOptions"
            :label="t('channels.sortLabel')"
            :prefix-label="t('channels.sortPrefix')"
            size="compact"
            class="w-36"
          />
          <IconButton
            :label="
              sortOrder === 'asc'
                ? t('channels.sortAscending')
                : t('channels.sortDescending')
            "
            class="pencil-control shrink-0 rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)]"
            data-handdrawn="control"
            @click="toggleSortOrder"
          >
            <ArrowUpNarrowWide v-if="sortOrder === 'asc'" :size="16" />
            <ArrowDownNarrowWide v-else :size="16" />
          </IconButton>
          <ConsoleButton
            v-if="canSensitiveWrite"
            size="sm"
            :aria-label="t('channels.createChannel')"
            :disabled="!canMutate"
            @click="openCreate"
          >
            <Plus :size="14" />
            <span class="2xl:hidden">{{ t('channels.createShort') }}</span>
            <span class="hidden 2xl:inline">{{
              t('channels.createChannel')
            }}</span>
          </ConsoleButton>
        </div>
      </div>

      <div
        v-if="selectedIds.length > 0"
        data-channel-bulk-actions
        class="console-bulk-bar flex flex-wrap items-center gap-2 border-b border-[var(--border-subtle)] bg-[var(--surface-muted)] px-4 py-2.5"
      >
        <span
          class="mr-auto text-xs font-semibold text-[var(--text-secondary)]"
          aria-live="polite"
        >
          {{ t('channels.selectedCount', { count: selectedIds.length }) }}
        </span>
        <IconButton
          v-if="canOperate"
          :label="t('channels.bulkEnable')"
          :disabled="
            !canMutate ||
            selectedChannels.every((channel) => channel.status === 1)
          "
          :class="isBulkActionBusy('enable') ? 'text-[var(--accent)]' : ''"
          @click="runBulkStatus('enable')"
        >
          <LoaderCircle
            v-if="isBulkActionBusy('enable')"
            :size="16"
            class="animate-spin"
          />
          <Power v-else :size="16" />
        </IconButton>
        <IconButton
          v-if="canOperate"
          :label="t('channels.bulkDisable')"
          :disabled="
            !canMutate ||
            selectedChannels.every((channel) => channel.status === 2)
          "
          :class="isBulkActionBusy('disable') ? 'text-[var(--accent)]' : ''"
          @click="runBulkStatus('disable')"
        >
          <LoaderCircle
            v-if="isBulkActionBusy('disable')"
            :size="16"
            class="animate-spin"
          />
          <PowerOff v-else :size="16" />
        </IconButton>
        <IconButton
          v-if="canOperate"
          :label="t('channels.bulkReset')"
          :disabled="!canMutate || resettableSelectedCount === 0"
          :class="isBulkActionBusy('reset') ? 'text-[var(--accent)]' : ''"
          @click="runBulkStatus('reset')"
        >
          <LoaderCircle
            v-if="isBulkActionBusy('reset')"
            :size="16"
            class="animate-spin"
          />
          <RotateCcw v-else :size="16" />
        </IconButton>
        <IconButton
          v-if="canSensitiveWrite"
          :label="t('channels.bulkDelete')"
          tone="danger"
          :disabled="!canMutate"
          @click="requestBulkDelete"
        >
          <Trash2 :size="16" />
        </IconButton>
        <IconButton
          :label="t('channels.clearSelection')"
          :disabled="!canMutate"
          @click="clearSelection"
        >
          <X :size="16" />
        </IconButton>
      </div>

      <div
        v-if="initialError"
        class="flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t('channels.loadFailed') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ initialError }}
        </p>
        <ConsoleButton class="mt-5" variant="secondary" @click="load()">
          {{ t('channels.retry') }}
        </ConsoleButton>
      </div>

      <template v-else>
        <DataTable
          :columns="columns"
          :rows="tableRows"
          row-key="key"
          :selectable="canSelect"
          checkbox-shape="round"
          :selected="selectedIds"
          :selection-keys="pageChannelIds"
          :selection-disabled="!canMutate"
          :row-selectable="isSelectableRow"
          :is-group-row="isSupplierRow"
          :loading="loading"
          :skeleton-rows="pageSize"
          adaptive-scroll
          :page-size="pageSize"
          :min-table-width="minTableWidth"
          :scroll-region-label="t('channels.title')"
          :empty-title="t('channels.emptyTitle')"
          :empty-hint="t('channels.emptyHint')"
          :row-class="channelRowClass"
          class="hidden md:block"
          @update:selected="updateSelected"
        >
          <template #group-row="{ row }">
            <div class="flex min-w-0 items-center justify-between gap-3">
              <button
                type="button"
                class="flex min-w-0 flex-1 items-center gap-2 text-left focus-ring"
                :aria-label="
                  isSupplierCollapsed((row as SupplierTableRow).supplier)
                    ? t('channels.expandSupplier', {
                        supplier: (row as SupplierTableRow).supplier,
                      })
                    : t('channels.collapseSupplier', {
                        supplier: (row as SupplierTableRow).supplier,
                      })
                "
                :aria-expanded="
                  !isSupplierCollapsed((row as SupplierTableRow).supplier)
                "
                @click="toggleSupplier((row as SupplierTableRow).supplier)"
              >
                <ChevronRight
                  v-if="isSupplierCollapsed((row as SupplierTableRow).supplier)"
                  :size="15"
                  class="shrink-0 text-[var(--text-tertiary)]"
                />
                <ChevronDown
                  v-else
                  :size="15"
                  class="shrink-0 text-[var(--text-tertiary)]"
                />
                <VendorLogo
                  :vendor="(row as SupplierTableRow).supplier"
                  :size="24"
                />
                <span class="truncate font-semibold">
                  {{ (row as SupplierTableRow).supplier }}
                </span>
                <span
                  class="shrink-0 text-xs font-normal text-[var(--text-tertiary)]"
                >
                  {{
                    t('channels.supplierGroupCount', {
                      count: (row as SupplierTableRow).count,
                    })
                  }}
                </span>
              </button>
              <div class="flex shrink-0 items-center gap-0.5">
                <span
                  v-if="
                    batchProgress?.scope === 'supplier' &&
                    batchProgress.supplier ===
                      (row as SupplierTableRow).supplier
                  "
                  class="mr-1 font-mono text-[0.625rem] tabular-nums text-[var(--text-tertiary)]"
                >
                  {{ batchProgressText() }}
                </span>
                <IconButton
                  v-if="
                    canOperate &&
                    (row as SupplierTableRow).channels.some(
                      supportsBalanceQuery
                    )
                  "
                  :label="
                    supplierBalanceLabel((row as SupplierTableRow).supplier)
                  "
                  :disabled="!canRunBatch"
                  @click="
                    runSupplierBalance(
                      (row as SupplierTableRow).supplier,
                      (row as SupplierTableRow).channels,
                      (row as SupplierTableRow).groupSlug
                    )
                  "
                >
                  <LoaderCircle
                    v-if="isSupplierBalance((row as SupplierTableRow).supplier)"
                    :size="14"
                    class="animate-spin"
                  />
                  <RefreshCw v-else :size="14" />
                </IconButton>
                <IconButton
                  v-if="
                    canOperate &&
                    hasMutableChannels((row as SupplierTableRow).channels)
                  "
                  :label="
                    t('channels.testSupplier', {
                      supplier: (row as SupplierTableRow).supplier,
                    })
                  "
                  @click="
                    openSupplierTest({
                      groupSlug: (row as SupplierTableRow).groupSlug,
                      supplier: (row as SupplierTableRow).supplier,
                      channels: (row as SupplierTableRow).channels,
                    })
                  "
                >
                  <Activity :size="14" />
                </IconButton>
                <IconButton
                  v-if="
                    canSensitiveWrite &&
                    hasMutableChannels((row as SupplierTableRow).channels)
                  "
                  :label="
                    t('channels.clearSupplier', {
                      supplier: (row as SupplierTableRow).supplier,
                    })
                  "
                  tone="danger"
                  :disabled="!canMutate"
                  @click="
                    requestClearSupplier({
                      groupSlug: (row as SupplierTableRow).groupSlug,
                      supplier: (row as SupplierTableRow).supplier,
                      channels: (row as SupplierTableRow).channels,
                    })
                  "
                >
                  <Trash2 :size="14" />
                </IconButton>
              </div>
            </div>
          </template>

          <template #header-upstream="{ column }">
            <div class="flex items-center justify-between gap-1">
              <span aria-hidden="true">{{ column.label }}</span>
              <span class="flex shrink-0 items-center gap-0.5">
                <span
                  v-if="isPageBalance()"
                  class="font-mono text-[0.5625rem] tabular-nums text-[var(--text-tertiary)]"
                >
                  {{ batchProgressText() }}
                </span>
                <IconButton
                  v-if="canOperate"
                  :label="balanceButtonLabel()"
                  :disabled="!canRunBatch"
                  class="h-6 w-6 rounded-md"
                  @click="runVisibleBalance()"
                >
                  <LoaderCircle
                    v-if="isPageBalance()"
                    :size="13"
                    class="animate-spin"
                  />
                  <RefreshCw v-else :size="13" />
                </IconButton>
              </span>
            </div>
          </template>

          <template #cell-id="{ row }">
            <span class="font-mono text-xs text-[var(--text-secondary)]">
              #{{ channelFromRow(row as AdminChannelTableRow).id }}
            </span>
          </template>
          <template #cell-name="{ row }">
            <span
              class="block truncate font-semibold"
              :class="
                channelFromRow(row as AdminChannelTableRow).status === 1
                  ? 'text-[var(--text-primary)]'
                  : 'text-[var(--text-secondary)]'
              "
              :title="channelFromRow(row as AdminChannelTableRow).name"
            >
              {{ channelFromRow(row as AdminChannelTableRow).name }}
            </span>
            <StatusChip
              v-if="
                hasInvalidModelMapping(
                  channelFromRow(row as AdminChannelTableRow).lab
                )
              "
              class="mt-1"
              tone="warning"
              data-channel-invalid-mapping
            >
              {{ t('channels.labInvalidMapping') }}
            </StatusChip>
            <ChannelUpstreamBindingBadge
              v-if="
                upstreamBindingFor(channelFromRow(row as AdminChannelTableRow))
              "
              :binding="
                upstreamBindingFor(channelFromRow(row as AdminChannelTableRow))!
              "
            />
            <p
              v-if="
                isReadOnlyChannel(channelFromRow(row as AdminChannelTableRow))
              "
              class="mt-1 whitespace-normal text-xs text-[var(--text-tertiary)]"
            >
              {{ t('channels.retiredHint') }}
            </p>
          </template>
          <template #cell-type="{ row }">
            <div class="flex min-w-0 items-center gap-2">
              <VendorLogo
                :vendor="
                  labDisplayName(
                    channelFromRow(row as AdminChannelTableRow).lab
                  )
                "
                :size="28"
              />
              <span
                class="truncate text-xs text-[var(--text-secondary)]"
                :title="
                  adminChannelTypeMeta(
                    channelFromRow(row as AdminChannelTableRow).type
                  ).label
                "
              >
                {{
                  adminChannelTypeMeta(
                    channelFromRow(row as AdminChannelTableRow).type
                  ).label
                }}
              </span>
            </div>
          </template>
          <template #cell-status="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <ChannelStatusBadge
                :channel="channelFromRow(row as AdminChannelTableRow)"
              />
            </div>
          </template>
          <template #cell-priority="{ row }">
            <ChannelInlineNumber
              v-if="
                canWrite &&
                !isReadOnlyChannel(channelFromRow(row as AdminChannelTableRow))
              "
              :value="channelFromRow(row as AdminChannelTableRow).priority"
              :min="Number.MIN_SAFE_INTEGER"
              :max="Number.MAX_SAFE_INTEGER"
              :label="
                t('channels.priorityFor', {
                  name: channelFromRow(row as AdminChannelTableRow).name,
                })
              "
              :busy="isRowBusy(channelFromRow(row as AdminChannelTableRow).id)"
              :commit="
                (value) =>
                  updateNumber(
                    channelFromRow(row as AdminChannelTableRow),
                    'priority',
                    value
                  )
              "
            />
            <span v-else class="tabular-nums">
              {{ channelFromRow(row as AdminChannelTableRow).priority }}
            </span>
            <small
              v-if="channelFromRow(row as AdminChannelTableRow).routing_quality"
              class="block text-[var(--text-tertiary)]"
            >
              {{ routeQualitySummary(row as AdminChannelTableRow) }}
            </small>
          </template>
          <template #cell-weight="{ row }">
            <ChannelInlineNumber
              v-if="
                canWrite &&
                !isReadOnlyChannel(channelFromRow(row as AdminChannelTableRow))
              "
              :value="channelFromRow(row as AdminChannelTableRow).weight"
              :label="
                t('channels.weightFor', {
                  name: channelFromRow(row as AdminChannelTableRow).name,
                })
              "
              :busy="isRowBusy(channelFromRow(row as AdminChannelTableRow).id)"
              :commit="
                (value) =>
                  updateNumber(
                    channelFromRow(row as AdminChannelTableRow),
                    'weight',
                    value
                  )
              "
            />
            <span v-else class="tabular-nums">
              {{ channelFromRow(row as AdminChannelTableRow).weight }}
            </span>
          </template>
          <template #cell-capacity="{ row }">
            <CapacityMeter
              v-if="
                channelFromRow(row as AdminChannelTableRow).max_concurrency > 0
              "
              :used="channelFromRow(row as AdminChannelTableRow).in_flight"
              :total="
                channelFromRow(row as AdminChannelTableRow).max_concurrency
              "
            />
            <span
              v-else
              class="font-mono text-xs tabular-nums text-[var(--text-secondary)]"
            >
              {{ channelFromRow(row as AdminChannelTableRow).in_flight }} / ∞
            </span>
          </template>
          <template #cell-usage="{ row }">
            <div class="sibling-space-y-0.5 text-xs">
              <p class="font-semibold tabular-nums">
                {{
                  formatQuota(
                    channelFromRow(row as AdminChannelTableRow).used_quota
                  )
                }}
              </p>
              <p class="text-[0.6875rem] text-[var(--text-tertiary)]">
                {{
                  t('channels.channelRatioValue', {
                    ratio: formatDecimal(
                      channelFromRow(row as AdminChannelTableRow).channel_ratio
                    ),
                  })
                }}
              </p>
            </div>
          </template>
          <template #cell-upstream="{ row }">
            <div class="flex items-center justify-between gap-1">
              <div class="min-w-0 sibling-space-y-0.5 text-xs">
                <ChannelBalanceDetails
                  :channel="channelFromRow(row as AdminChannelTableRow)"
                />
                <button
                  v-if="
                    upstreamRatios[
                      channelFromRow(row as AdminChannelTableRow).id
                    ]
                  "
                  type="button"
                  class="flex flex-wrap items-center gap-1 text-left text-[0.6875rem] text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                  :disabled="!canRead"
                  @click="
                    editingUpstreamRatio = channelFromRow(
                      row as AdminChannelTableRow
                    )
                  "
                >
                  <Scale :size="12" aria-hidden="true" />
                  <UpstreamRatioSummary
                    :view="
                      upstreamRatios[
                        channelFromRow(row as AdminChannelTableRow).id
                      ]!
                    "
                  />
                </button>
              </div>
              <IconButton
                v-if="
                  canOperate &&
                  supportsBalanceQuery(
                    channelFromRow(row as AdminChannelTableRow)
                  ) &&
                  isFieldVisible('rowUpstreamAction')
                "
                :label="t('channels.refreshBalance')"
                :disabled="
                  isRowBusy(channelFromRow(row as AdminChannelTableRow).id)
                "
                class="shrink-0"
                @click="
                  refreshBalance(channelFromRow(row as AdminChannelTableRow))
                "
              >
                <LoaderCircle
                  v-if="
                    isBusy(
                      channelFromRow(row as AdminChannelTableRow).id,
                      'balance'
                    )
                  "
                  :size="14"
                  class="animate-spin"
                />
                <RefreshCw v-else :size="14" />
              </IconButton>
            </div>
          </template>
          <template #cell-response="{ row }">
            <div
              v-if="(row as AdminChannelTableRow).kind === 'channel'"
              class="flex items-center justify-between gap-1"
            >
              <StatusChip
                class="overflow-visible whitespace-nowrap"
                :tone="
                  responseCellDisplay(
                    channelFromRow(row as AdminChannelTableRow)
                  ).tone
                "
                :title="
                  responseCellDisplay(
                    channelFromRow(row as AdminChannelTableRow)
                  ).title
                "
              >
                <LoaderCircle
                  v-if="
                    responseCellDisplay(
                      channelFromRow(row as AdminChannelTableRow)
                    ).running
                  "
                  :size="12"
                  class="relative z-[1] shrink-0 animate-spin overflow-visible"
                  data-channel-response-spinner
                />
                {{
                  responseCellDisplay(
                    channelFromRow(row as AdminChannelTableRow)
                  ).text
                }}
              </StatusChip>
              <IconButton
                v-if="
                  canOperate &&
                  !isReadOnlyChannel(
                    channelFromRow(row as AdminChannelTableRow)
                  ) &&
                  isFieldVisible('rowResponseAction')
                "
                :label="t('channels.testChannel')"
                class="shrink-0"
                @click="openTest(channelFromRow(row as AdminChannelTableRow))"
              >
                <Activity :size="14" />
              </IconButton>
            </div>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-0.5">
              <IconButton
                v-if="
                  canOperate &&
                  !isReadOnlyChannel(
                    channelFromRow(row as AdminChannelTableRow)
                  )
                "
                :label="t('channels.syncModelsTitle')"
                :disabled="!canMutate"
                @click="
                  previewModelSync(channelFromRow(row as AdminChannelTableRow))
                "
                ><RefreshCw :size="15"
              /></IconButton>
              <IconButton
                v-if="
                  canRead &&
                  !isReadOnlyChannel(
                    channelFromRow(row as AdminChannelTableRow)
                  )
                "
                :label="t('gateway.channelRuntime')"
                @click="
                  openRuntime(channelFromRow(row as AdminChannelTableRow))
                "
                ><Gauge :size="15"
              /></IconButton>
              <IconButton
                v-if="
                  canEditChannel(
                    channelFromRow(row as AdminChannelTableRow),
                    canWrite
                  )
                "
                :label="t('channels.editChannel')"
                :disabled="
                  isRowBusy(channelFromRow(row as AdminChannelTableRow).id)
                "
                @click="openEdit(channelFromRow(row as AdminChannelTableRow))"
              >
                <Pencil :size="15" />
              </IconButton>
              <IconButton
                v-if="
                  canOperate &&
                  !isReadOnlyChannel(
                    channelFromRow(row as AdminChannelTableRow)
                  )
                "
                :label="
                  channelFromRow(row as AdminChannelTableRow).status === 1
                    ? t('channels.disableChannel')
                    : t('channels.enableChannel')
                "
                :tone="
                  channelFromRow(row as AdminChannelTableRow).status === 1
                    ? 'danger'
                    : 'default'
                "
                :disabled="
                  isRowBusy(channelFromRow(row as AdminChannelTableRow).id)
                "
                @click="
                  toggleStatus(channelFromRow(row as AdminChannelTableRow))
                "
              >
                <LoaderCircle
                  v-if="
                    isBusy(
                      channelFromRow(row as AdminChannelTableRow).id,
                      'status'
                    )
                  "
                  :size="15"
                  class="animate-spin"
                />
                <PowerOff
                  v-else-if="
                    channelFromRow(row as AdminChannelTableRow).status === 1
                  "
                  :size="15"
                />
                <Power v-else :size="15" />
              </IconButton>
              <IconButton
                v-if="canSensitiveWrite"
                :label="t('channels.deleteChannel')"
                tone="danger"
                :disabled="
                  isRowBusy(channelFromRow(row as AdminChannelTableRow).id)
                "
                @click="
                  requestDelete(channelFromRow(row as AdminChannelTableRow))
                "
              >
                <LoaderCircle
                  v-if="
                    isCrudActionBusy(
                      'delete',
                      channelFromRow(row as AdminChannelTableRow).id
                    )
                  "
                  :size="15"
                  class="animate-spin"
                />
                <Trash2 v-else :size="15" />
              </IconButton>
            </div>
          </template>
          <template #footer>
            <TablePagination
              v-model:page="page"
              v-model:page-size="pageSize"
              :total="total"
              :page-sizes="[10, 20, 50]"
            />
          </template>
        </DataTable>

        <div class="md:hidden">
          <div
            v-if="loading"
            class="sibling-divide-y sibling-divide-[var(--border-subtle)]"
            aria-hidden="true"
          >
            <div v-for="index in 5" :key="index" class="sibling-space-y-4 p-4">
              <SkeletonBlock rounded="md" class="h-5 w-2/3" />
              <div class="grid grid-cols-2 gap-3">
                <SkeletonBlock rounded="md" class="h-14" />
                <SkeletonBlock rounded="md" class="h-14" />
              </div>
            </div>
          </div>
          <EmptyState
            v-else-if="visibleRows.length === 0"
            :title="t('channels.emptyTitle')"
            :hint="t('channels.emptyHint')"
            illustration="empty-search"
          />
          <ChannelMobileList
            v-else
            :groups="supplierGroups"
            :visible-fields="visibleFields"
            :selected-ids="selectedIds"
            :all-selected="allPageSelected"
            :selection-disabled="!canMutate || !canSelect"
            :toggle-all-selected="toggleAllSelected"
            :toggle-selected="toggleSelected"
            :is-supplier-collapsed="isSupplierCollapsed"
            :toggle-supplier="toggleSupplier"
            :batch-progress="batchProgress"
            :group-test-overrides="groupTestOverrides"
            :can-run-batch="canRunBatch"
            :can-mutate="canMutate"
            :can-operate="canOperate"
            :can-write="canWrite"
            :can-sensitive-write="canSensitiveWrite"
            :run-supplier-balance="runSupplierBalance"
            :pick-supplier-test="openSupplierTest"
            :clear-supplier="requestClearSupplier"
            :is-busy="isBusy"
            :is-row-busy="isRowBusy"
            :update-number="updateNumber"
            :toggle-status="toggleStatus"
            :refresh-balance="refreshBalance"
            :open-test="openTest"
            :edit-channel="openEdit"
            :sync-models="previewModelSync"
            :show-runtime="openRuntime"
            :delete-channel="requestDelete"
            :upstream-bindings="upstreamBindings"
          />
          <TablePagination
            v-if="!loading"
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[10, 20, 50]"
          />
        </div>
      </template>
    </ConsoleCard>

    <ConsoleModal
      :open="runtimeChannel !== null && canRead"
      :title="runtimeChannel?.name ?? t('gateway.channelRuntime')"
      size="lg"
      @close="runtimeChannel = null"
    >
      <dl
        v-if="runtimeChannel"
        class="grid grid-cols-2 gap-4 text-sm"
        data-native-channel-runtime
      >
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.status') }}
          </dt>
          <dd class="mt-1"><ChannelStatusBadge :channel="runtimeChannel" /></dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.channelRatio') }}
          </dt>
          <dd class="mt-1">{{ runtimeChannel.channel_ratio }}×</dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.capacity') }}
          </dt>
          <dd class="mt-1">
            {{ runtimeChannel.in_flight }} /
            {{ runtimeChannel.max_concurrency || '∞' }}
          </dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">{{ t('channels.lab') }}</dt>
          <dd class="mt-1">{{ labDisplayName(runtimeChannel.lab) }}</dd>
        </div>
      </dl>
      <ChannelProviderStatus
        v-if="runtimeChannel && [62, 63].includes(runtimeChannel.type)"
        :channel="runtimeChannel"
      />
    </ConsoleModal>
    <ConsoleModal
      :open="balanceResponse !== null"
      :title="
        t('channels.balanceRawTitle', {
          name: balanceResponse?.channelName ?? '',
        })
      "
      size="lg"
      @close="balanceResponse = null"
    >
      <p class="mb-3 text-sm text-[var(--text-secondary)]">
        {{ t('channels.balanceRawHint') }}
      </p>
      <pre
        class="max-h-[55vh] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[var(--surface-hover)] p-4 text-xs text-[var(--text-primary)]"
        >{{ balanceResponse?.rawResponse }}</pre>
      <template #footer>
        <ConsoleButton variant="secondary" @click="balanceResponse = null">
          {{ t('common.close') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>
    <ChannelFormModal
      :open="formOpen"
      :editing="editing"
      :upstream-binding="upstreamBindingFor(editing)"
      :save="saveForm"
      :fetch-models="fetchUpstreamModels"
      @close="closeForm"
    />
    <ChannelModelSyncDialog
      :channel="syncingModels"
      :can-write="canWrite"
      @close="syncingModels = null"
      @applied="load({ background: true })"
    />

    <ChannelUpstreamRatioDialog
      :channel="editingUpstreamRatio"
      :binding="upstreamBindingFor(editingUpstreamRatio)"
      :can-operate="canOperate && canMutate"
      :can-write="canWrite && canMutate"
      @close="editingUpstreamRatio = null"
      @saved="updateUpstreamRatio"
    />

    <ChannelTestModal
      :open="testing !== null"
      :channel="testing"
      :can-write="canWrite"
      :test-model="testChannelModel"
      :remove-models="removeChannelModels"
      @close="testing = null"
    />

    <ChannelTestPickerDialog
      :open="pickingGroup !== null"
      :group-slug="pickingGroup?.groupSlug"
      :supplier="pickingGroup?.supplier ?? ''"
      :channels="pickingGroup?.channels ?? []"
      :test-model="testChannelModel"
      @close="closeSupplierTest"
      @start="onGroupTestStart"
      @result="onGroupTestResult"
      @tested="onGroupTested"
    />

    <ConfirmDialog
      :open="deleting !== null"
      :title="deleteDialogTitle"
      :message="deleteDialogMessage"
      :confirm-text="t('common.delete')"
      :loading="isCrudActionBusy('delete') || isBulkActionBusy('delete')"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </div>
</template>
