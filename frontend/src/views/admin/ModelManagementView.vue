<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  ArrowDownToLine,
  Check,
  Copy,
  ListPlus,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  ToggleLeft,
  ToggleRight,
  Trash2,
} from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import FieldVisibilityMenu from '@/components/common/FieldVisibilityMenu.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import MetadataSyncDialog from '@/components/admin/MetadataSyncDialog.vue'
import ModelMetadataDrawer from '@/components/admin/ModelMetadataDrawer.vue'
import ModelMetadataCell from '@/components/admin/ModelMetadataCell.vue'
import MissingModelsDialog from '@/components/admin/MissingModelsDialog.vue'
import ManagementDialog from '@/components/admin/ManagementDialog.vue'
import { runMetadataBatch } from '@/components/admin/modelMetadataForm'
import {
  metadataApi,
  type ModelMetadata,
  type ModelVendor,
} from '@/api/adminManagement'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import '@/components/admin/admin.css'

const { t } = useI18n({ useScope: 'local', messages: modelMetadataMessages })
const route = useRoute()
const router = useRouter()
const { loading, error, run } = useAdminRequest()
const vendorRequest = useAdminRequest()
const rows = ref<ModelMetadata[]>([])
const vendors = ref<ModelVendor[]>([])
const vendorCounts = ref<Record<string, number>>({})
const total = ref(0)
const search = ref('')
const vendorFilter = ref('')
const statusFilter = ref('')
const syncFilter = ref('')
const selection = ref<number[]>([])
const busy = ref(false)
const feedback = ref('')
const failures = ref<{ name: string; message: string }[]>([])
const operationError = ref('')
const deleteRows = ref<ModelMetadata[] | null>(null)
const drawerOpen = ref(false)
const editingModel = ref<ModelMetadata | null>(null)
const initialName = ref('')
const syncOpen = ref(false)
const missingOpen = ref(false)
const manageAction = ref('')
const fields = [
  'id',
  'model_name',
  'name_rule',
  'status',
  'vendor_id',
  'description',
  'tags',
  'endpoints',
  'bound_channels',
  'enable_groups',
  'quota_types',
  'sync_official',
  'created_time',
  'updated_time',
] as const
type Field = (typeof fields)[number]
const defaultFields: Field[] = [
  'model_name',
  'name_rule',
  'status',
  'vendor_id',
  'description',
  'tags',
  'sync_official',
]
const visibleFields = ref<Field[]>([...defaultFields])
const visibleColumns = computed(() =>
  fields.filter(
    (field) => field === 'model_name' || visibleFields.value.includes(field)
  )
)
const optionalFields = fields.filter((field) => field !== 'model_name')
const numberQuery = (key: string, fallback: number) => {
  const value = Number(route.query[key])
  return Number.isInteger(value) && value > 0 ? value : fallback
}
const page = computed(() => numberQuery('p', 1))
const pageSize = computed(() =>
  [10, 20, 50].includes(numberQuery('page_size', 20))
    ? numberQuery('page_size', 20)
    : 20
)
const queryText = (key: string) =>
  typeof route.query[key] === 'string' ? (route.query[key] as string) : ''
const params = computed(() => ({
  p: page.value,
  page_size: pageSize.value,
  keyword: queryText('keyword') || undefined,
  vendor: queryText('vendor') || undefined,
  status: ['0', '1'].includes(queryText('status'))
    ? queryText('status')
    : undefined,
  sync_official: ['0', '1'].includes(queryText('sync_official'))
    ? queryText('sync_official')
    : undefined,
}))
const manager = computed(() =>
  route.query.manage === 'vendors' || route.query.manage === 'prefill-groups'
    ? route.query.manage
    : null
)
const selectedRows = computed(() =>
  rows.value.filter((row) => selection.value.includes(row.id))
)
const pageSelected = computed(
  () =>
    rows.value.length > 0 &&
    rows.value.every((row) => selection.value.includes(row.id))
)
const vendorMap = computed(
  () => new Map(vendors.value.map((vendor) => [vendor.id, vendor]))
)
const vendorOptions = computed(() => [
  { value: '', label: t('allVendors') },
  ...vendors.value.map((vendor) => ({
    value: String(vendor.id),
    label: `${vendor.name} (${vendorCounts.value[String(vendor.id)] ?? 0})`,
  })),
])
const statusOptions = computed(() => [
  { value: '', label: t('allStatus') },
  { value: '1', label: t('enabled') },
  { value: '0', label: t('disabled') },
])
const syncOptions = computed(() => [
  { value: '', label: t('allSync') },
  { value: '1', label: t('syncEnabled') },
  { value: '0', label: t('syncDisabled') },
])

async function load() {
  await run(async (signal) => {
    const result = await metadataApi.models(params.value, signal)
    if (signal.aborted) return
    rows.value = result.items
    total.value = result.total
    vendorCounts.value = result.vendor_counts
    selection.value = selection.value.filter((id) =>
      result.items.some((row) => row.id === id)
    )
    if (page.value > Math.max(1, Math.ceil(result.total / pageSize.value)))
      void setPage(Math.max(1, Math.ceil(result.total / pageSize.value)))
  })
}
async function loadVendors() {
  await vendorRequest.run(async (signal) => {
    const result = await metadataApi.allVendors(signal)
    if (!signal.aborted) vendors.value = result
  })
}
watch(
  params,
  () => {
    search.value = params.value.keyword ?? ''
    vendorFilter.value = params.value.vendor ?? ''
    statusFilter.value = params.value.status ?? ''
    syncFilter.value = params.value.sync_official ?? ''
    selection.value = []
    void load()
  },
  { immediate: true }
)
onMounted(loadVendors)
async function updateQuery(values: Record<string, string | undefined>) {
  await router.replace({
    query: { ...route.query, ...values },
    hash: route.hash,
  })
}
function applyFilters() {
  void updateQuery({
    keyword: search.value.trim() || undefined,
    vendor: vendorFilter.value || undefined,
    status: statusFilter.value || undefined,
    sync_official: syncFilter.value || undefined,
    p: '1',
  })
}
function setPage(value: number) {
  return updateQuery({ p: String(value) })
}
function setPageSize(value: number) {
  void updateQuery({ page_size: String(value), p: '1' })
}
function togglePage() {
  selection.value = pageSelected.value ? [] : rows.value.map((row) => row.id)
}
function openCreate(name = '') {
  editingModel.value = null
  initialName.value = name
  drawerOpen.value = true
  missingOpen.value = false
}
function edit(row: ModelMetadata) {
  editingModel.value = row
  initialName.value = ''
  drawerOpen.value = true
}
function selectManagement() {
  if (manageAction.value) void updateQuery({ manage: manageAction.value })
  manageAction.value = ''
}
async function managerSaved() {
  await loadVendors()
  await load()
}
async function copyNames(names: string[]) {
  operationError.value = ''
  try {
    await navigator.clipboard.writeText(names.join('\n'))
    feedback.value = t('copied', { count: names.length })
  } catch (cause) {
    operationError.value =
      cause instanceof Error ? cause.message : String(cause)
  }
}
async function mutate(
  targets: ModelMetadata[],
  action: 'enable' | 'disable' | 'delete'
) {
  if (busy.value || !targets.length) return
  busy.value = true
  failures.value = []
  feedback.value = ''
  operationError.value = ''
  try {
    const result = await runMetadataBatch(targets, (row) =>
      action === 'delete'
        ? metadataApi.deleteModel(row.id)
        : metadataApi.setModelStatus(row.id, action === 'enable' ? 1 : 0)
    )
    selection.value = result.failed.map((failure) => failure.row.id)
    failures.value = result.failed.map((failure) => ({
      name: failure.row.model_name,
      message: failure.message,
    }))
    feedback.value = t('batchResult', {
      success: result.succeeded.length,
      failed: result.failed.length,
    })
    deleteRows.value = null
    await load()
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="admin-page metadata-page">
    <div class="admin-toolbar">
      <h1>{{ t('models') }}</h1>
      <ConsoleButton
        variant="ghost"
        :loading="loading"
        :disabled="busy"
        @click="load"
        ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="busy"
        @click="syncOpen = true"
        ><ArrowDownToLine :size="16" />{{ t('synchronize') }}</ConsoleButton
      ><ConsoleButton :disabled="busy" @click="openCreate()"
        ><Plus :size="16" />{{ t('create') }}</ConsoleButton
      >
    </div>
    <form class="metadata-filters" @submit.prevent="applyFilters">
      <label class="admin-field metadata-search"
        ><span class="sr-only">{{ t('search') }}</span
        ><input
          v-model="search"
          :placeholder="t('search')"
          :disabled="busy" /></label
      ><FilterSelect
        v-model="vendorFilter"
        :options="vendorOptions"
        :label="t('vendor_id')"
        :disabled="busy"
        @update:model-value="applyFilters"
      /><FilterSelect
        v-model="statusFilter"
        :options="statusOptions"
        :label="t('status')"
        :disabled="busy"
        @update:model-value="applyFilters"
      /><FilterSelect
        v-model="syncFilter"
        :options="syncOptions"
        :label="t('sync_official')"
        :disabled="busy"
        @update:model-value="applyFilters"
      /><IconButton type="submit" :label="t('searchAction')" :disabled="busy"
        ><Search :size="18"
      /></IconButton>
    </form>
    <div class="metadata-tools">
      <label class="admin-check"
        ><input
          type="checkbox"
          :checked="pageSelected"
          :indeterminate="selection.length > 0 && !pageSelected"
          :disabled="busy || loading || !rows.length"
          @change="togglePage"
        />{{ t('selectPage') }}</label
      >
      <div class="metadata-management">
        <ConsoleButton
          size="sm"
          variant="ghost"
          :disabled="busy"
          @click="missingOpen = true"
          ><ListPlus :size="16" />{{ t('missing') }}</ConsoleButton
        ><label class="metadata-manage-select admin-field"
          ><Settings2 :size="16" aria-hidden="true" /><span class="sr-only">{{
            t('manage')
          }}</span
          ><select
            v-model="manageAction"
            :aria-label="t('manage')"
            :disabled="busy"
            @change="selectManagement"
          >
            <option value="">{{ t('manage') }}</option>
            <option value="vendors">{{ t('vendors') }}</option>
            <option value="prefill-groups">{{ t('prefillGroups') }}</option>
          </select></label
        ><FieldVisibilityMenu
          v-model="visibleFields"
          :all-fields="optionalFields"
          :default-fields="defaultFields"
          :label-for="(field) => t(field)"
          :title="t('columns')"
          :reset-label="t('reset')"
        />
      </div>
    </div>
    <div v-if="selection.length" class="metadata-bulk">
      <span>{{ t('selected', { count: selection.length }) }}</span
      ><ConsoleButton
        size="sm"
        variant="ghost"
        :disabled="busy"
        @click="mutate(selectedRows, 'enable')"
        ><Check :size="15" />{{ t('enable') }}</ConsoleButton
      ><ConsoleButton
        size="sm"
        variant="ghost"
        :disabled="busy"
        @click="mutate(selectedRows, 'disable')"
        ><ToggleLeft :size="15" />{{ t('disable') }}</ConsoleButton
      ><IconButton
        :label="t('copy')"
        :disabled="busy"
        @click="copyNames(selectedRows.map((row) => row.model_name))"
        ><Copy :size="16" /></IconButton
      ><IconButton
        :label="t('remove')"
        tone="danger"
        :disabled="busy"
        @click="deleteRows = [...selectedRows]"
        ><Trash2 :size="16"
      /></IconButton>
    </div>
    <p v-if="error || operationError" class="admin-error" role="alert">
      {{ error || operationError }}
    </p>
    <div
      v-if="vendorRequest.error.value"
      class="metadata-vendor-error"
      role="alert"
    >
      <span class="admin-error">{{ vendorRequest.error.value }}</span
      ><ConsoleButton
        variant="ghost"
        size="sm"
        :loading="vendorRequest.loading.value"
        @click="loadVendors"
        ><RefreshCw :size="15" />{{ t('vendors') }}</ConsoleButton
      >
    </div>
    <p v-if="feedback" role="status" class="admin-muted">{{ feedback }}</p>
    <ul v-if="failures.length" class="admin-error" role="alert">
      <li v-for="failure in failures" :key="failure.name">
        {{ failure.name }}: {{ failure.message }}
      </li>
    </ul>
    <p v-if="loading" role="status" class="admin-muted">{{ t('loading') }}</p>
    <div
      class="admin-table-scroll metadata-table-scroll"
      :aria-busy="loading || busy"
    >
      <table
        class="admin-table metadata-table"
        :style="{
          '--metadata-table-width': `${Math.max(780, visibleColumns.length * 125 + 256)}px`,
        }"
      >
        <thead>
          <tr>
            <th class="metadata-selection">
              <span class="sr-only">{{ t('selectPage') }}</span>
            </th>
            <th
              v-for="field in visibleColumns"
              :key="field"
              :class="{ 'metadata-model-cell': field === 'model_name' }"
            >
              {{ t(field) }}
            </th>
            <th>{{ t('actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.id"
            :data-selected="selection.includes(row.id)"
          >
            <td class="metadata-selection">
              <input
                v-model="selection"
                type="checkbox"
                :value="row.id"
                :aria-label="t('selectModel', { name: row.model_name })"
                :disabled="busy || loading"
              />
            </td>
            <td
              v-for="field in visibleColumns"
              :key="field"
              :data-label="t(field)"
              :class="{ 'metadata-model-cell': field === 'model_name' }"
            >
              <ModelMetadataCell
                :row="row"
                :field="field"
                :vendor="vendorMap.get(row.vendor_id)"
                @copy="(name) => copyNames([name])"
              />
            </td>
            <td class="metadata-actions" :data-label="t('actions')">
              <div class="admin-row-actions">
                <IconButton
                  :label="t('edit')"
                  :disabled="busy || loading"
                  @click="edit(row)"
                  ><Pencil :size="15" /></IconButton
                ><IconButton
                  :label="t(row.status ? 'disable' : 'enable')"
                  :disabled="busy || loading"
                  @click="mutate([row], row.status ? 'disable' : 'enable')"
                  ><ToggleRight v-if="row.status" :size="16" /><ToggleLeft
                    v-else
                    :size="16" /></IconButton
                ><IconButton
                  :label="t('remove')"
                  tone="danger"
                  :disabled="busy || loading"
                  @click="deleteRows = [row]"
                  ><Trash2 :size="15"
                /></IconButton>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p
      v-if="!loading && !error && !rows.length"
      class="metadata-empty admin-muted"
    >
      {{ t('empty') }}
    </p>
    <TablePagination
      :page="page"
      :page-size="pageSize"
      :total="total"
      @update:page="setPage"
      @update:page-size="setPageSize"
    />
    <ModelMetadataDrawer
      v-if="drawerOpen"
      :model="editingModel"
      :initial-name="initialName"
      :vendors="vendors"
      @close="drawerOpen = false"
      @changed="load"
      @saved="drawerOpen = false"
    />
    <ConfirmDialog
      :open="Boolean(deleteRows)"
      :title="t('confirmDelete')"
      :message="t('deleteMessage', { count: deleteRows?.length ?? 0 })"
      :confirm-text="t('remove')"
      :loading="busy"
      @confirm="mutate(deleteRows ?? [], 'delete')"
      @cancel="deleteRows = null"
    />
    <MetadataSyncDialog
      :open="syncOpen"
      @close="syncOpen = false"
      @saved="managerSaved"
    />
    <MissingModelsDialog
      :open="missingOpen"
      @close="missingOpen = false"
      @create="openCreate"
    />
    <ManagementDialog
      v-if="manager"
      :open="true"
      :kind="manager"
      @close="updateQuery({ manage: undefined })"
      @saved="managerSaved"
    />
  </div>
</template>
<style scoped>
.metadata-filters {
  display: grid;
  grid-template-columns: minmax(10rem, 2fr) repeat(3, minmax(8rem, 1fr)) 2.5rem;
  gap: 0.75rem;
  align-items: center;
}
.metadata-tools,
.metadata-management,
.metadata-bulk,
.metadata-vendor-error {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
}
.metadata-tools {
  justify-content: space-between;
}
.metadata-manage-select {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}
.metadata-manage-select select {
  min-width: 7rem;
}
.metadata-bulk {
  min-height: 3rem;
  padding: 0.5rem 0.75rem;
  background: var(--accent-soft);
  color: var(--accent-text);
  border-block: 1px solid var(--border-default);
}
.metadata-bulk > span {
  margin-right: auto;
  font-size: 0.875rem;
}
.metadata-table-scroll {
  border-block: 1px solid var(--border-subtle);
}
.metadata-table {
  min-width: var(--metadata-table-width);
  table-layout: fixed;
}
.metadata-table th:first-child,
.metadata-table td:first-child {
  width: 2.75rem;
}
.metadata-table th:last-child,
.metadata-table td:last-child {
  width: 7rem;
}
.metadata-table td {
  vertical-align: top;
}
.metadata-model-cell {
  width: 14rem;
}
.metadata-table tr[data-selected='true'] {
  background: var(--accent-soft);
}
.metadata-empty {
  text-align: center;
  padding-block: 2rem;
}
.metadata-page input[type='checkbox'] {
  width: 1rem;
  height: 1rem;
  accent-color: var(--accent);
}
.metadata-page input[type='checkbox']:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
@media (max-width: 1100px) {
  .metadata-filters {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .metadata-search {
    grid-column: 1 / 3;
  }
  .metadata-filters > button {
    grid-column: 3;
    grid-row: 1;
    justify-self: end;
  }
}
@media (max-width: 640px) {
  .metadata-filters {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
  .metadata-search {
    grid-column: 1;
  }
  .metadata-filters > button {
    grid-column: 2;
  }
  .metadata-filters > :nth-child(4) {
    grid-column: 1 / -1;
  }
  .metadata-table,
  .metadata-table tbody {
    display: block;
    min-width: 0;
    width: 100%;
  }
  .metadata-table thead {
    display: none;
  }
  .metadata-table tr {
    display: grid;
    grid-template-columns: 2.5rem minmax(0, 1fr);
    border-bottom: 1px solid var(--border-default);
    padding-block: 0.5rem;
  }
  .metadata-table td {
    display: block;
    grid-column: 2;
    width: auto !important;
    max-width: none;
    border: 0;
    padding: 0.375rem 0.5rem;
  }
  .metadata-table td.metadata-selection {
    grid-row: 1;
    grid-column: 1;
  }
  .metadata-table
    td:not(.metadata-model-cell):not(.metadata-selection):not(
      .metadata-actions
    )::before {
    content: attr(data-label);
    display: block;
    margin-bottom: 0.25rem;
    color: var(--text-tertiary);
    font-size: 0.75rem;
  }
  .metadata-management {
    width: 100%;
    justify-content: space-between;
  }
  .metadata-tools {
    gap: 1rem;
  }
}
</style>
