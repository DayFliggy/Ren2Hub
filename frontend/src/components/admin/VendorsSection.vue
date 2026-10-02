<script setup lang="ts">
/**
 * Vendors section of model management (NewAPI vendors-table). Keyword and
 * pagination live in the URL as vFilter / vPage / vPageSize so the section
 * survives reloads and tab switches; other query keys and the hash are kept.
 */
import { AlertTriangle, Pencil, Trash2 } from 'lucide-vue-next'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { metadataApi, type ModelVendor } from '@/api/adminManagement'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FieldVisibilityMenu from '@/components/common/FieldVisibilityMenu.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { useToast } from '@/composables/useToast'
import { adminManagementMessages } from '@/i18n/adminManagement'
import { formatTime } from '@/utils/format'
import MetadataIcon from './MetadataIcon.vue'
import VendorDrawer from './VendorDrawer.vue'

type VendorRow = ModelVendor & Record<string, unknown>

const PAGE_SIZES = [10, 20, 50]
const DEFAULT_PAGE_SIZE = 20
const SEARCH_DEBOUNCE_MS = 300
const OPTIONAL_FIELDS = [
  'description',
  'model_count',
  'id',
  'created_time',
  'updated_time',
] as const
type OptionalField = (typeof OPTIONAL_FIELDS)[number]
const DEFAULT_FIELDS: OptionalField[] = ['description', 'model_count']

const emit = defineEmits<{
  changed: []
  'open-model': [payload: { id: number }]
}>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const route = useRoute()
const router = useRouter()
const toast = useToast()
const { loading, error, run } = useAdminRequest()

function queryNumber(key: string): number {
  const value = Number(route.query[key])
  return Number.isInteger(value) && value > 0 ? value : 0
}
const page = computed(() => queryNumber('vPage') || 1)
const pageSize = computed(() => {
  const size = queryNumber('vPageSize')
  return PAGE_SIZES.includes(size) ? size : DEFAULT_PAGE_SIZE
})
const filter = computed(() =>
  typeof route.query.vFilter === 'string' ? route.query.vFilter.trim() : ''
)

function updateQuery(values: Record<string, string | undefined>) {
  return router.replace({
    query: { ...route.query, ...values },
    hash: route.hash,
  })
}
// Defaults are omitted from the URL, as upstream's table URL state does.
function setPage(value: number) {
  void updateQuery({ vPage: value > 1 ? String(value) : undefined })
}
function setPageSize(value: number) {
  void updateQuery({
    vPageSize: value === DEFAULT_PAGE_SIZE ? undefined : String(value),
    vPage: undefined,
  })
}

const keyword = ref(filter.value)
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(filter, (value) => {
  if (value !== keyword.value.trim()) keyword.value = value
})
watch(keyword, (value) => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    const next = value.trim()
    if (next === filter.value) return
    void updateQuery({ vFilter: next || undefined, vPage: undefined })
  }, SEARCH_DEBOUNCE_MS)
})
onBeforeUnmount(() => clearTimeout(searchTimer))

const rows = ref<VendorRow[]>([])
const total = ref(0)
const counts = ref<Record<string, number>>({})

async function load() {
  await run(async (signal) => {
    const [result, vendorCounts] = await Promise.all([
      metadataApi.vendors(
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: filter.value || undefined,
        },
        signal
      ),
      metadataApi.vendorCounts(signal),
    ])
    if (signal.aborted) return
    rows.value = result.items as VendorRow[]
    total.value = result.total
    counts.value = vendorCounts
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize.value))
    if (page.value > lastPage) setPage(lastPage)
  })
}
watch([page, pageSize, filter], () => void load(), { immediate: true })

const visibleFields = ref<OptionalField[]>([...DEFAULT_FIELDS])
const fieldLabels: Record<OptionalField, string> = {
  description: 'description',
  model_count: 'linkedModels',
  id: 'id',
  created_time: 'createdAt',
  updated_time: 'updatedAt',
}
const fieldLabel = (field: OptionalField) => t(fieldLabels[field])
const allColumns = computed<Array<TableColumn & { optional?: OptionalField }>>(
  () => [
    { key: 'name', label: t('vendor'), width: '250px' },
    ...OPTIONAL_FIELDS.map((field) => ({
      key: field,
      label: fieldLabel(field),
      width: field === 'description' ? '350px' : '160px',
      optional: field,
    })),
    { key: 'actions', label: t('actions'), width: '70px', align: 'right' },
  ]
)
const columns = computed<TableColumn[]>(() =>
  allColumns.value.filter(
    (col) => !col.optional || visibleFields.value.includes(col.optional)
  )
)
const minTableWidth = computed(() => {
  const width = columns.value.reduce(
    (sum, col) => sum + Number.parseInt(col.width ?? '120', 10),
    0
  )
  return `${Math.max(640, width + 40)}px`
})

const linkedCount = (vendor: ModelVendor) =>
  counts.value[String(vendor.id)] ?? 0
const modelsLink = (vendor: ModelVendor) => ({
  path: '/models/metadata',
  query: { vendor: String(vendor.id) },
})
const timeLabel = (value: number | null) => (value ? formatTime(value) : '—')
const rowActions = computed<ActionMenuItem[]>(() => [
  { key: 'edit', label: t('edit'), icon: Pencil },
  { key: 'delete', label: t('remove'), icon: Trash2, tone: 'danger' },
])
function onRowAction(vendor: ModelVendor, key: string) {
  if (key === 'edit') openEdit(vendor)
  else if (key === 'delete') requestDelete(vendor)
}

const drawerOpen = ref(false)
const editing = ref<ModelVendor | null>(null)
function openCreate() {
  editing.value = null
  drawerOpen.value = true
}
function openEdit(vendor: ModelVendor) {
  editing.value = vendor
  drawerOpen.value = true
}
function onSaved() {
  drawerOpen.value = false
  emit('changed')
  void load()
}
function onOpenModel(payload: { id: number }) {
  drawerOpen.value = false
  emit('open-model', payload)
}

const deleting = ref<ModelVendor | null>(null)
const deleteError = ref('')
const deleteBusy = ref(false)
function requestDelete(vendor: ModelVendor) {
  deleting.value = vendor
  deleteError.value = ''
}
function cancelDelete() {
  if (!deleteBusy.value) deleting.value = null
}
async function confirmDelete() {
  const vendor = deleting.value
  if (!vendor || deleteBusy.value) return
  deleteBusy.value = true
  deleteError.value = ''
  try {
    await metadataApi.deleteVendor(vendor.id)
    deleting.value = null
    toast.success(t('deleted', { name: vendor.name }))
    emit('changed')
    void load()
  } catch (cause) {
    deleteError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    deleteBusy.value = false
  }
}

defineExpose({ openCreate, reload: load })
</script>

<template>
  <div>
    <ConsoleCard :padded="false">
      <div
        class="flex items-center gap-3 border-b border-[var(--border-subtle)] p-4"
      >
        <SearchInput
          v-model="keyword"
          :placeholder="t('searchVendors')"
          :aria-label="t('searchVendors')"
          name="vendor-search"
          class="min-w-0 flex-1 xl:max-w-sm"
        />
        <FieldVisibilityMenu
          v-model="visibleFields"
          class="ml-auto"
          :all-fields="OPTIONAL_FIELDS"
          :default-fields="DEFAULT_FIELDS"
          :label-for="fieldLabel"
          :title="t('toggleColumns')"
          :reset-label="t('resetColumns')"
        />
      </div>

      <div
        v-if="error"
        class="flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t('loadVendorsFailed') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ error }}
        </p>
        <ConsoleButton class="mt-5" variant="secondary" @click="load">
          {{ t('retry') }}
        </ConsoleButton>
      </div>

      <template v-else>
        <DataTable
          class="hidden md:block"
          :columns="columns"
          :rows="rows"
          row-key="id"
          :loading="loading"
          :skeleton-rows="pageSize"
          adaptive-scroll
          :page-size="pageSize"
          :min-table-width="minTableWidth"
          :scroll-region-label="t('vendor')"
          :empty-title="t('noVendors')"
          :empty-hint="t('noVendorsHint')"
        >
          <template #cell-name="{ row }">
            <div class="flex min-w-0 items-center gap-2">
              <MetadataIcon
                :icon="(row as VendorRow).icon"
                :name="(row as VendorRow).name"
                :size="24"
              />
              <button
                type="button"
                class="min-w-0 truncate text-left font-mono text-sm text-[var(--text-primary)] underline-offset-4 hover:underline focus-ring"
                :title="(row as VendorRow).name"
                data-vendor-name
                @click="openEdit(row as VendorRow)"
              >
                {{ (row as VendorRow).name }}
              </button>
            </div>
          </template>
          <template #cell-description="{ row }">
            <p
              class="line-clamp-2 whitespace-normal break-words text-xs text-[var(--text-tertiary)]"
              :title="(row as VendorRow).description"
            >
              {{ (row as VendorRow).description || '—' }}
            </p>
          </template>
          <template #cell-model_count="{ row }">
            <RouterLink
              :to="modelsLink(row as VendorRow)"
              class="inline-flex items-center gap-1 rounded-md bg-[var(--accent-soft)] px-2 py-0.5 text-xs font-semibold tabular-nums text-[var(--accent-text)] transition-colors hover:bg-[var(--accent-soft)]/80 focus-ring"
            >
              {{ linkedCount(row as VendorRow) }}
            </RouterLink>
          </template>
          <template #cell-id="{ row }">
            <span class="font-mono text-xs text-[var(--text-tertiary)]">
              {{ (row as VendorRow).id }}
            </span>
          </template>
          <template #cell-created_time="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{ timeLabel((row as VendorRow).created_time) }}
            </span>
          </template>
          <template #cell-updated_time="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{ timeLabel((row as VendorRow).updated_time) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex justify-end">
              <ActionMenu
                :items="rowActions"
                :label="t('openMenu')"
                @select="onRowAction(row as VendorRow, $event)"
              />
            </div>
          </template>
          <template #footer>
            <TablePagination
              :page="page"
              :page-size="pageSize"
              :total="total"
              :page-sizes="PAGE_SIZES"
              @update:page="setPage"
              @update:page-size="setPageSize"
            />
          </template>
        </DataTable>

        <div class="md:hidden">
          <div
            v-if="loading"
            class="divide-y divide-[var(--border-subtle)]"
            aria-hidden="true"
          >
            <div v-for="i in 5" :key="i" class="space-y-3 p-4">
              <SkeletonBlock rounded="md" class="h-4 w-1/2" />
              <SkeletonBlock rounded="md" class="h-10" />
            </div>
          </div>
          <EmptyState
            v-else-if="rows.length === 0"
            :title="t('noVendors')"
            :hint="t('noVendorsHint')"
            illustration="empty-search"
          />
          <template v-else>
            <div
              v-for="vendor in rows"
              :key="vendor.id"
              class="flex items-start gap-3 border-b border-[var(--border-subtle)] px-4 py-3"
            >
              <MetadataIcon
                :icon="vendor.icon"
                :name="vendor.name"
                :size="28"
              />
              <div class="min-w-0 flex-1">
                <button
                  type="button"
                  class="max-w-full truncate text-left font-mono text-sm text-[var(--text-primary)] focus-ring"
                  @click="openEdit(vendor)"
                >
                  {{ vendor.name }}
                </button>
                <p
                  class="mt-0.5 line-clamp-2 break-words text-xs text-[var(--text-tertiary)]"
                >
                  {{ vendor.description || '—' }}
                </p>
                <RouterLink
                  :to="modelsLink(vendor)"
                  class="mt-1 inline-block text-xs text-[var(--text-secondary)] underline-offset-4 hover:underline focus-ring"
                >
                  {{ t('linkedModels') }}
                  <span class="tabular-nums text-[var(--accent-text)]">{{
                    linkedCount(vendor)
                  }}</span>
                </RouterLink>
              </div>
              <ActionMenu
                :items="rowActions"
                :label="t('openMenu')"
                @select="onRowAction(vendor, $event)"
              />
            </div>
            <TablePagination
              :page="page"
              :page-size="pageSize"
              :total="total"
              :page-sizes="PAGE_SIZES"
              @update:page="setPage"
              @update:page-size="setPageSize"
            />
          </template>
        </div>
      </template>
    </ConsoleCard>
    <VendorDrawer
      v-if="drawerOpen"
      :vendor="editing"
      :linked-count="editing ? linkedCount(editing) : 0"
      @close="drawerOpen = false"
      @saved="onSaved"
      @open-model="onOpenModel"
    />
    <ConfirmDialog
      :open="deleting !== null"
      :title="t('deleteVendorTitle')"
      :message="t('deleteVendorMessage', { name: deleting?.name ?? '' })"
      :confirm-text="t('remove')"
      :loading="deleteBusy"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    >
      <p
        v-if="deleteError"
        role="alert"
        class="mt-3 text-sm text-[var(--status-danger-text)]"
      >
        {{ deleteError }}
      </p>
    </ConfirmDialog>
  </div>
</template>
