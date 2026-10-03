<script setup lang="ts">
/** NewAPI "Models" section: toolbar, metadata table, bulk actions. */
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useStorage } from '@vueuse/core'
import { AlertTriangle, Copy, Eye, EyeOff, Trash2, X } from 'lucide-vue-next'
import {
  metadataApi,
  type ModelMetadata,
  type ModelVendor,
} from '@/api/adminManagement'
import ActionMenu from '@/components/common/ActionMenu.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FieldVisibilityMenu from '@/components/common/FieldVisibilityMenu.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { useToast } from '@/composables/useToast'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import ModelMetadataCell, {
  type ModelMetadataField,
} from './ModelMetadataCell.vue'
import { runMetadataBatch } from './modelMetadataForm'

const props = defineProps<{ vendors: ModelVendor[] }>()
const emit = defineEmits<{ edit: [id: number] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMetadataMessages })
const route = useRoute()
const router = useRouter()
const toast = useToast()
const { loading, error, run } = useAdminRequest()

const OPTIONAL_FIELDS = [
  'status',
  'connections',
  'tags',
  'sync_official',
  'id',
  'vendor',
  'name_rule',
  'description',
  'endpoints',
  'created_time',
  'updated_time',
] as const satisfies readonly ModelMetadataField[]
type OptionalField = (typeof OPTIONAL_FIELDS)[number]
// NewAPI hides these by default; "status" stays visible here because the
// model-square state column it stood in for upstream does not exist.
const DEFAULT_FIELDS: OptionalField[] = [
  'status',
  'connections',
  'tags',
  'sync_official',
]
const FIELD_LABELS: Record<OptionalField, string> = {
  status: 'displayPolicy',
  connections: 'connections',
  tags: 'tags',
  sync_official: 'syncPolicy',
  id: 'id',
  vendor: 'vendor',
  name_rule: 'matchType',
  description: 'description',
  endpoints: 'customEndpoints',
  created_time: 'created',
  updated_time: 'updated',
}
const sanitize = (fields: readonly string[]) =>
  OPTIONAL_FIELDS.filter((field) => fields.includes(field))
const storedFields = useStorage<string[]>(
  'ren2hub_admin_model_metadata_fields',
  [...DEFAULT_FIELDS]
)
const visibleFields = computed<OptionalField[]>({
  get: () => sanitize(storedFields.value),
  set: (fields) => (storedFields.value = sanitize(fields)),
})

const rows = ref<ModelMetadata[]>([])
const total = ref(0)
const vendorCounts = ref<Record<string, number>>({})
const selection = ref<number[]>([])
const busy = ref(false)
const failures = ref<{ name: string; message: string }[]>([])
const deleteRows = ref<ModelMetadata[] | null>(null)

// URL keys follow NewAPI: filter, vendor, status, sync, page, pageSize.
const queryText = (key: string) =>
  typeof route.query[key] === 'string' ? (route.query[key] as string) : ''
const queryNumber = (key: string, fallback: number) => {
  const value = Number(route.query[key])
  return Number.isInteger(value) && value > 0 ? value : fallback
}
const page = computed(() => queryNumber('page', 1))
const pageSize = computed(() => {
  const size = queryNumber('pageSize', 20)
  return [10, 20, 50, 100].includes(size) ? size : 20
})
const filters = computed(() => ({
  keyword: queryText('filter'),
  vendor: queryText('vendor'),
  status: ['0', '1'].includes(queryText('status')) ? queryText('status') : '',
  sync: ['0', '1'].includes(queryText('sync')) ? queryText('sync') : '',
}))
const filtered = computed(() => Object.values(filters.value).some(Boolean))
const search = ref(filters.value.keyword)

const vendorMap = computed(
  () => new Map(props.vendors.map((vendor) => [vendor.id, vendor]))
)
const statusOptions = computed(() => [
  { value: '', label: t('all') },
  { value: '1', label: t('allowed') },
  { value: '0', label: t('notListed') },
])
const syncOptions = computed(() => [
  { value: '', label: t('all') },
  { value: '1', label: t('allowUpdates') },
  { value: '0', label: t('keepLocal') },
])
const vendorOptions = computed(() => [
  { value: '', label: t('allVendors') },
  ...props.vendors.map((vendor) => ({
    value: String(vendor.id),
    label: `${vendor.name} (${vendorCounts.value[String(vendor.id)] ?? 0})`,
  })),
])
const columns = computed<TableColumn[]>(() => [
  { key: 'model', label: t('model'), width: '16rem' },
  ...OPTIONAL_FIELDS.filter((field) => visibleFields.value.includes(field)).map(
    (field) => ({ key: field, label: t(FIELD_LABELS[field]) })
  ),
  { key: 'actions', label: t('actions'), width: '9rem', align: 'right' },
])
const minTableWidth = computed(
  () => `${Math.max(720, 400 + visibleFields.value.length * 130)}px`
)
const selectedRows = computed(() =>
  rows.value.filter((row) => selection.value.includes(row.id))
)

async function load() {
  await run(async (signal) => {
    const result = await metadataApi.models(
      {
        p: page.value,
        page_size: pageSize.value,
        keyword: filters.value.keyword || undefined,
        vendor: filters.value.vendor || undefined,
        status: filters.value.status || undefined,
        sync_official: filters.value.sync || undefined,
      },
      signal
    )
    if (signal.aborted) return
    rows.value = result.items
    total.value = result.total
    vendorCounts.value = result.vendor_counts
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize.value))
    if (page.value > lastPage)
      void updateQuery({ page: lastPage > 1 ? String(lastPage) : undefined })
    selection.value = selection.value.filter((id) =>
      result.items.some((row) => row.id === id)
    )
  })
}

function updateQuery(values: Record<string, string | undefined>) {
  return router.replace({
    query: { ...route.query, ...values },
    hash: route.hash,
  })
}
function setFilter(key: 'vendor' | 'status' | 'sync', value: string) {
  void updateQuery({ [key]: value || undefined, page: undefined })
}
function resetFilters() {
  search.value = ''
  void updateQuery({
    filter: undefined,
    vendor: undefined,
    status: undefined,
    sync: undefined,
    page: undefined,
  })
}

async function copyNames(names: string[]) {
  try {
    await navigator.clipboard.writeText(names.join(','))
    toast.success(t('copied'))
  } catch {
    toast.error(t('copyFailed'))
  }
}

async function mutate(
  targets: ModelMetadata[],
  action: 'show' | 'hide' | 'delete'
) {
  if (busy.value || !targets.length) return
  busy.value = true
  failures.value = []
  try {
    const result = await runMetadataBatch(targets, (row) =>
      action === 'delete'
        ? metadataApi.deleteModel(row.id)
        : metadataApi.setModelStatus(row.id, action === 'show' ? 1 : 0)
    )
    // Failed rows stay selected so the action can be retried.
    selection.value = result.failed.map((failure) => failure.row.id)
    failures.value = result.failed.map((failure) => ({
      name: failure.row.model_name,
      message: failure.message,
    }))
    if (action === 'delete' && result.succeeded.length)
      toast.success(t('deleted', { count: result.succeeded.length }))
    else if (result.failed.length)
      toast.error(
        t('batchResult', {
          success: result.succeeded.length,
          failed: result.failed.length,
        })
      )
    deleteRows.value = null
    await load()
  } finally {
    busy.value = false
  }
}

function rowAction(row: ModelMetadata, key: string) {
  if (key === 'delete') deleteRows.value = [row]
  else void mutate([row], key as 'show' | 'hide')
}
function rowMenu(row: ModelMetadata) {
  return [
    row.status === 1
      ? { key: 'hide', label: t('hideFromSquare'), icon: EyeOff }
      : { key: 'show', label: t('showInSquare'), icon: Eye },
    {
      key: 'delete',
      label: t('delete'),
      icon: Trash2,
      tone: 'danger' as const,
      separated: true,
    },
  ]
}

watch(
  [filters, page, pageSize],
  () => {
    search.value = filters.value.keyword
    failures.value = []
    void load()
  },
  { immediate: true, deep: true }
)
// NewAPI debounces the model-name filter by 500 ms.
watch(search, (value, _previous, cleanup) => {
  const timer = setTimeout(() => {
    const keyword = value.trim()
    if (keyword !== filters.value.keyword)
      void updateQuery({ filter: keyword || undefined, page: undefined })
  }, 500)
  cleanup(() => clearTimeout(timer))
})

defineExpose({ reload: load })
</script>
<template>
  <ConsoleCard :padded="false">
    <div
      class="flex flex-col gap-3 border-b border-[var(--border-subtle)] p-4 xl:flex-row xl:items-center"
    >
      <SearchInput
        v-model="search"
        :placeholder="t('filterPlaceholder')"
        :aria-label="t('filterPlaceholder')"
        name="admin-model-filter"
        class="w-full xl:w-72"
      />
      <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-3 xl:flex-1">
        <FilterSelect
          :model-value="filters.status"
          :options="statusOptions"
          :label="t('displayPolicy')"
          :prefix-label="t('displayPolicy')"
          @update:model-value="setFilter('status', $event)"
        />
        <FilterSelect
          :model-value="filters.vendor"
          :options="vendorOptions"
          :label="t('vendor')"
          :prefix-label="t('vendor')"
          @update:model-value="setFilter('vendor', $event)"
        />
        <FilterSelect
          :model-value="filters.sync"
          :options="syncOptions"
          :label="t('syncPolicy')"
          :prefix-label="t('syncPolicy')"
          @update:model-value="setFilter('sync', $event)"
        />
      </div>
      <div class="flex items-center justify-end gap-2">
        <ConsoleButton
          v-if="filtered"
          variant="ghost"
          size="sm"
          @click="resetFilters"
          >{{ t('reset') }}<X :size="14" aria-hidden="true"
        /></ConsoleButton>
        <FieldVisibilityMenu
          v-model="visibleFields"
          :all-fields="OPTIONAL_FIELDS"
          :default-fields="DEFAULT_FIELDS"
          :label-for="(field) => t(FIELD_LABELS[field])"
          :title="t('toggleColumns')"
          :reset-label="t('reset')"
        />
      </div>
    </div>
    <div
      v-if="selection.length"
      class="flex flex-wrap items-center gap-2 border-b border-[var(--border-subtle)] bg-[var(--accent-soft)] px-4 py-2.5 transition-colors"
    >
      <IconButton
        :label="t('clearSelection')"
        :disabled="busy"
        @click="selection = []"
        ><X :size="16"
      /></IconButton>
      <span
        class="mr-auto inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--accent-text)]"
        aria-live="polite"
        ><span class="h-1.5 w-1.5 rounded-full bg-[var(--accent)]" />{{
          t('selected', { count: selection.length })
        }}</span
      >
      <IconButton
        :label="t('showSelected')"
        :disabled="busy"
        @click="mutate(selectedRows, 'show')"
        ><Eye :size="16"
      /></IconButton>
      <IconButton
        :label="t('hideSelected')"
        :disabled="busy"
        @click="mutate(selectedRows, 'hide')"
        ><EyeOff :size="16"
      /></IconButton>
      <IconButton
        :label="t('copyNames')"
        @click="copyNames(selectedRows.map((row) => row.model_name))"
        ><Copy :size="16"
      /></IconButton>
      <IconButton
        :label="t('deleteSelected')"
        tone="danger"
        :disabled="busy"
        @click="deleteRows = [...selectedRows]"
        ><Trash2 :size="16"
      /></IconButton>
    </div>
    <ul
      v-if="failures.length"
      role="alert"
      class="space-y-1 border-b border-[var(--border-subtle)] px-4 py-2.5 text-xs text-[var(--status-danger-text)]"
    >
      <li v-for="failure in failures" :key="failure.name">
        <span class="font-mono">{{ failure.name }}</span
        >: {{ failure.message }}
      </li>
    </ul>
    <div
      v-if="error && !rows.length"
      class="flex min-h-72 flex-col items-center justify-center px-6 py-12 text-center"
      role="alert"
    >
      <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
      <p class="mt-3 font-semibold text-[var(--text-primary)]">
        {{ t('loadFailed') }}
      </p>
      <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
        {{ error }}
      </p>
      <ConsoleButton class="mt-5" variant="secondary" size="sm" @click="load">
        {{ t('retry') }}
      </ConsoleButton>
    </div>
    <template v-else>
      <p
        v-if="error"
        role="alert"
        class="border-b border-[var(--border-subtle)] px-4 py-2.5 text-xs text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
      <DataTable
        class="hidden md:block"
        :columns="columns"
        :rows="rows"
        row-key="id"
        selectable
        checkbox-shape="round"
        :selected="selection"
        :selection-disabled="busy"
        :row-selection-label="
          (row) => t('selectModel', { name: (row as ModelMetadata).model_name })
        "
        :loading="loading"
        :skeleton-rows="pageSize"
        adaptive-scroll
        :page-size="pageSize"
        :min-table-width="minTableWidth"
        :scroll-region-label="t('modelManagement')"
        :empty-title="t('emptyTitle')"
        :empty-hint="filtered ? t('emptyFiltered') : t('emptyHint')"
        :class="{ 'opacity-60': busy }"
        @update:selected="selection = $event as number[]"
      >
        <template
          v-for="field in ['model', ...visibleFields]"
          :key="field"
          #[`cell-${field}`]="{ row }"
        >
          <ModelMetadataCell
            :row="row as ModelMetadata"
            :field="field as ModelMetadataField"
            :vendor="vendorMap.get((row as ModelMetadata).vendor_id)"
            @copy="(name) => copyNames([name])"
            @edit="emit('edit', (row as ModelMetadata).id)"
          />
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center justify-end gap-1">
            <ConsoleButton
              variant="ghost"
              size="sm"
              :disabled="busy"
              @click="emit('edit', (row as ModelMetadata).id)"
              >{{ t('edit') }}</ConsoleButton
            >
            <ActionMenu
              :items="rowMenu(row as ModelMetadata)"
              :label="t('openMenu')"
              :disabled="busy"
              @select="rowAction(row as ModelMetadata, $event)"
            />
          </div>
        </template>
        <template #footer>
          <TablePagination
            :page="page"
            :page-size="pageSize"
            :total="total"
            :page-sizes="[10, 20, 50, 100]"
            @update:page="updateQuery({ page: String($event) })"
            @update:page-size="
              updateQuery({ pageSize: String($event), page: undefined })
            "
          />
        </template>
      </DataTable>
      <div class="md:hidden">
        <div
          v-if="loading && !rows.length"
          class="divide-y divide-[var(--border-subtle)]"
          aria-hidden="true"
        >
          <div v-for="i in 5" :key="i" class="space-y-3 p-4">
            <SkeletonBlock rounded="md" class="h-4 w-1/2" />
            <SkeletonBlock rounded="md" class="h-10" />
          </div>
        </div>
        <EmptyState
          v-else-if="!rows.length"
          :title="t('emptyTitle')"
          :hint="filtered ? t('emptyFiltered') : t('emptyHint')"
          illustration="empty-search"
        />
        <template v-else>
          <article
            v-for="row in rows"
            :key="row.id"
            class="space-y-3 border-b border-[var(--border-subtle)] px-4 py-3"
          >
            <div class="flex items-start gap-3">
              <label
                class="flex min-h-[44px] min-w-[44px] cursor-pointer items-center justify-center -m-2.5 p-2.5 shrink-0"
              >
                <input
                  v-model="selection"
                  type="checkbox"
                  :value="row.id"
                  :aria-label="t('selectModel', { name: row.model_name })"
                  :disabled="busy"
                  class="checkbox-round shrink-0"
                />
              </label>
              <div class="min-w-0 flex-1">
                <ModelMetadataCell
                  :row="row"
                  field="model"
                  :vendor="vendorMap.get(row.vendor_id)"
                  @copy="(name) => copyNames([name])"
                  @edit="emit('edit', row.id)"
                />
              </div>
              <ModelMetadataCell :row="row" field="status" />
            </div>
            <dl class="grid grid-cols-2 gap-x-4 gap-y-2 pl-7 text-xs">
              <div>
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('connections') }}
                </dt>
                <dd><ModelMetadataCell :row="row" field="connections" /></dd>
              </div>
              <div>
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('syncPolicy') }}
                </dt>
                <dd><ModelMetadataCell :row="row" field="sync_official" /></dd>
              </div>
              <div class="col-span-2">
                <dt class="text-[var(--text-tertiary)]">{{ t('tags') }}</dt>
                <dd><ModelMetadataCell :row="row" field="tags" /></dd>
              </div>
            </dl>
            <div
              class="flex items-center justify-end gap-1 border-t border-[var(--border-subtle)] pt-2"
            >
              <ConsoleButton
                variant="ghost"
                size="sm"
                :disabled="busy"
                @click="emit('edit', row.id)"
                >{{ t('edit') }}</ConsoleButton
              >
              <ActionMenu
                :items="rowMenu(row)"
                :label="t('openMenu')"
                :disabled="busy"
                @select="rowAction(row, $event)"
              />
            </div>
          </article>
          <TablePagination
            :page="page"
            :page-size="pageSize"
            :total="total"
            :page-sizes="[10, 20, 50, 100]"
            @update:page="updateQuery({ page: String($event) })"
            @update:page-size="
              updateQuery({ pageSize: String($event), page: undefined })
            "
          />
        </template>
      </div>
    </template>
  </ConsoleCard>
  <ConfirmDialog
    :open="Boolean(deleteRows)"
    :title="t('deleteTitle')"
    :message="
      deleteRows?.length === 1
        ? t('deleteOne', { name: deleteRows[0]!.model_name })
        : t('deleteMany', { count: deleteRows?.length ?? 0 })
    "
    :confirm-text="t('delete')"
    :loading="busy"
    @confirm="mutate(deleteRows ?? [], 'delete')"
    @cancel="deleteRows = null"
  />
</template>
