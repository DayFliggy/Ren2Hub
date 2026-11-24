<script setup lang="ts">
import {
  Eye,
  FileCode2,
  LoaderCircle,
  Pencil,
  Plus,
  Power,
  PowerOff,
  Route,
  Trash2,
  X,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { api } from '@/api/console'
import { parseTokenPage, parseUserModels } from '@/api/liveContracts'
import { ApiError, type PageResult } from '@/api/types'
import type { TokenSummary } from '@/types/console'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import KeyEndpointStrip from '@/components/console/keys/KeyEndpointStrip.vue'
import KeyIdentity from '@/components/console/keys/KeyIdentity.vue'
import KeyMobileList from '@/components/console/keys/KeyMobileList.vue'
import KeyQuotaPopover from '@/components/console/keys/KeyQuotaPopover.vue'
import KeyQuotaTrigger from '@/components/console/keys/KeyQuotaTrigger.vue'
import { useKeyQuotaDetail } from '@/components/console/keys/useKeyQuotaDetail'
import KeyRouteChip from '@/components/console/keys/KeyRouteChip.vue'
import KeyRevealModal from '@/components/console/keys/KeyRevealModal.vue'
import KeyClientConfigModal from '@/components/console/keys/KeyClientConfigModal.vue'
import { clientConfigMessages } from '@/i18n/clientConfig'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { tokenRoutingAccess } from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { tokenMeter } from '@/utils/tokenMeter'

const { t } = useI18n()
const { t: clientText } = useI18n({
  useScope: 'local',
  messages: clientConfigMessages,
})
const toast = useToast()
const app = useAppStore()
const auth = useAuthStore()
const routingEnabled = computed(
  () =>
    app.isFeatureEnabled(tokenRoutingAccess.feature) &&
    auth.hasPermission(
      tokenRoutingAccess.requiresPermission.resource,
      tokenRoutingAccess.requiresPermission.action
    )
)

const rows = ref<TokenSummary[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(true)
const keyword = ref('')
const selected = ref<Array<string | number>>([])

const models = ref<string[]>([])

const formOpen = ref(false)
const createdNotice = ref(false)
const editing = ref<TokenSummary | null>(null)
const deleting = ref<TokenSummary | null>(null)
const batchDeleting = ref(false)
const deleteLoading = ref(false)
const revealTokenId = ref<number | null>(null)
const clientConfigTokenId = ref<number | null>(null)

// The quota ring replaces the balance figures; status and expiry live in the
// ring and the note under each name. Figures are in the ring's popover.
const columns = computed<TableColumn[]>(() => [
  { key: 'name', label: t('keys.colName') },
  { key: 'type', label: t('keys.colType'), width: '184px' },
  { key: 'key', label: t('keys.colKey') },
  { key: 'quota', label: t('keys.colQuota'), align: 'center', width: '88px' },
  // Five actions: 32px mouse or 44px touch/tablet, plus gaps and cell padding (24px).
  {
    key: 'actions',
    label: t('keys.colActions'),
    align: 'right',
    width: '264px',
  },
])

// Mirror KeyMobileList: only keys that cannot serve fade back; spent keys stay
// at full strength so their red quota ring keeps calling for action.
function rowClass(row: TokenSummary): string | undefined {
  const state = tokenMeter(row, nowSec.value).state
  return state === 'disabled' || state === 'expired'
    ? 'bg-[var(--surface-muted)] opacity-80'
    : undefined
}

const allPageSelected = computed(
  () =>
    rows.value.length > 0 &&
    rows.value.every((row) => selected.value.includes(row.id))
)

function toggleAllSelected() {
  selected.value = allPageSelected.value ? [] : rows.value.map((row) => row.id)
}

function toggleSelected(row: TokenSummary) {
  selected.value = selected.value.includes(row.id)
    ? selected.value.filter((id) => id !== row.id)
    : [...selected.value, row.id]
}

const listRequest = useLatestRequest()

// One clock per load, so every ring on the page judges expiry alike.
const nowSec = ref(Math.floor(Date.now() / 1000))
const detail = useKeyQuotaDetail()
const detailId = useId()

/**
 * Mouse hover anywhere on a table row opens the detail by its ring — except
 * over the actions, which the popover hanging below the row would otherwise
 * cover for the rows underneath. Cards expand their detail inline instead.
 */
function hoverRow(event: PointerEvent) {
  if (event.pointerType !== 'mouse') return
  const target = event.target as Element
  if (target.closest('td[headers$="-actions"]')) {
    detail.leave(true)
    return
  }
  const row = target.closest('tbody tr')
  const ring = row?.querySelector<HTMLElement>('[data-key-quota-ring]')
  const token = ring
    ? rows.value.find((item) => String(item.id) === ring.dataset.tokenId)
    : undefined
  if (!ring || !token) {
    detail.leave()
    return
  }
  detail.hover(token, ring)
}

/** A mouse click keeps the hover popover; touch and keyboard toggle it. */
function openDetail(
  token: TokenSummary,
  anchor: HTMLElement,
  pointerType: string
) {
  if (pointerType === 'mouse') detail.show(token, anchor)
  else detail.toggle(token, anchor)
}

watch(rows, () => detail.hide())

async function load() {
  loading.value = true
  const result = await listRequest.run(async (signal) =>
    parseTokenPage(
      await api.get<PageResult<TokenSummary>>(
        keyword.value.trim() ? '/api/token/search' : '/api/token/',
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: keyword.value,
        },
        { signal }
      )
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    rows.value = []
    total.value = 0
    selected.value = []
    toast.error(
      result.error instanceof ApiError
        ? result.error.message
        : String(result.error)
    )
    return
  }
  const pageResult = result.value
  nowSec.value = Math.floor(Date.now() / 1000)
  rows.value = pageResult.items
  total.value = pageResult.total
  selected.value = []
}

let searchTimer = 0
// Reset to page 1 without firing load() twice (see LogsView for the rationale).
function reload() {
  window.clearTimeout(searchTimer)
  if (page.value === 1) load()
  else page.value = 1
}
watch(keyword, () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})
watch(pageSize, reload)
watch(page, load)

const togglingIds = ref<Set<number>>(new Set())

async function toggleStatus(row: TokenSummary) {
  if (togglingIds.value.has(row.id)) return // ignore double-clicks in flight
  togglingIds.value = new Set(togglingIds.value).add(row.id)
  try {
    await api.put('/api/token/?status_only=true', {
      id: row.id,
      status: row.status === 1 ? 2 : 1,
    })
    toast.success(t('keys.updated'))
    load()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    const next = new Set(togglingIds.value)
    next.delete(row.id)
    togglingIds.value = next
  }
}

function onSaved(created: boolean) {
  void load()
  // Creation stays upstream-shaped (no new id), so the hint names the row action.
  if (created && routingEnabled.value) createdNotice.value = true
}

function openCreate() {
  editing.value = null
  formOpen.value = true
}

function openEdit(row: TokenSummary) {
  editing.value = row
  formOpen.value = true
}

function copyKey(row: TokenSummary) {
  revealTokenId.value = row.id
}

function closeDelete() {
  deleting.value = null
  batchDeleting.value = false
}

async function confirmDelete() {
  deleteLoading.value = true
  try {
    if (batchDeleting.value) {
      await api.post('/api/token/batch', { ids: selected.value })
      toast.success(t('keys.deleted'))
    } else if (deleting.value) {
      await api.delete(`/api/token/${deleting.value.id}`)
      toast.success(t('keys.deleted'))
    }
    deleting.value = null
    batchDeleting.value = false
    load()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    deleteLoading.value = false
  }
}

onMounted(async () => {
  void load()
  try {
    const meta = await api.get<unknown>('/api/user/models')
    models.value = parseUserModels(meta)
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
})

onBeforeUnmount(() => {
  window.clearTimeout(searchTimer)
})
</script>

<template>
  <div data-key-page class="mx-auto max-w-[var(--console-list-width)]">
    <PageBreadcrumb :crumbs="[t('keys.breadcrumb.0'), t('keys.breadcrumb.1')]">
      <template #action>
        <KeyEndpointStrip />
      </template>
    </PageBreadcrumb>
    <ConsoleCard class="key-list" :padded="false">
      <!-- toolbar -->
      <div class="flex flex-wrap items-center gap-3 p-4">
        <SearchInput
          v-model="keyword"
          :placeholder="t('keys.searchPlaceholder')"
          :aria-label="t('keys.searchPlaceholder')"
          name="token-search"
          class="w-full sm:w-72"
        />
        <div class="flex w-full items-center gap-2.5 sm:ml-auto sm:w-auto">
          <ConsoleButton
            v-if="selected.length > 0"
            variant="danger"
            class="animate-fade-in"
            @click="batchDeleting = true"
          >
            {{ t('keys.batchDelete') }} ({{ selected.length }})
          </ConsoleButton>
          <ConsoleButton class="ml-auto sm:ml-0" @click="openCreate">
            <Plus :size="15" :stroke-width="2.5" aria-hidden="true" />
            {{ t('keys.createKey') }}
          </ConsoleButton>
        </div>
      </div>

      <div
        v-if="createdNotice"
        role="status"
        data-key-created-notice
        class="no-handdrawn mx-4 mb-4 flex items-start gap-2 rounded-xl border border-[var(--status-info)] bg-[var(--status-info-soft)] py-2 pl-4 pr-2 text-sm text-[var(--status-info-text)]"
      >
        <span class="min-w-0 flex-1 self-center">{{
          t('keys.createdRoutingHint')
        }}</span>
        <IconButton
          class="shrink-0"
          :label="t('keys.dismissHint')"
          @click="createdNotice = false"
        >
          <X :size="16" aria-hidden="true" />
        </IconButton>
      </div>

      <DataTable
        v-model:selected="selected"
        class="key-list-table"
        :columns="columns"
        :rows="rows"
        row-key="id"
        :row-class="rowClass"
        :loading="loading"
        :skeleton-rows="pageSize"
        adaptive-scroll
        :page-size="pageSize"
        min-table-width="896px"
        :scroll-region-label="t('keys.breadcrumb.1')"
        selectable
        checkbox-shape="round"
        :empty-title="t('keys.emptyTitle')"
        empty-illustration="empty-keys"
        @pointerover="hoverRow"
        @pointerleave="detail.leave()"
      >
        <template #cell-name="{ row }">
          <KeyIdentity :token="row as TokenSummary" :now="nowSec" />
        </template>

        <template #cell-type="{ row }">
          <KeyRouteChip :token="row as TokenSummary" />
        </template>

        <template #cell-key="{ row }">
          <div
            class="flex min-w-0 items-center gap-1.5"
            @click.stop
            @dblclick.stop
          >
            <span
              class="min-w-0 truncate font-mono text-xs text-[var(--text-secondary)]"
              :title="(row as TokenSummary).key_preview"
            >
              {{ (row as TokenSummary).key_preview }}
            </span>
            <IconButton
              class="shrink-0"
              :label="t('keys.viewKey')"
              @click="copyKey(row as TokenSummary)"
            >
              <Eye :size="15" aria-hidden="true" />
            </IconButton>
          </div>
        </template>

        <template #cell-quota="{ row }">
          <KeyQuotaTrigger
            :token="row as TokenSummary"
            :now="nowSec"
            :expanded="detail.token.value?.id === (row as TokenSummary).id"
            :detail-id="detailId"
            @detail="
              (anchor, pointerType) =>
                openDetail(row as TokenSummary, anchor, pointerType)
            "
            @blur="detail.hide(row as TokenSummary)"
          />
        </template>

        <template #cell-actions="{ row }">
          <!-- Action buttons must not trigger the inline panel. -->
          <div
            class="flex items-center justify-end gap-1"
            @click.stop
            @dblclick.stop
          >
            <RouterLink
              v-if="routingEnabled"
              :to="{
                name: 'token-routing',
                params: { id: (row as TokenSummary).id },
              }"
              class="inline-flex h-11 w-11 sm:h-8 sm:w-8 items-center justify-center rounded-lg text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
              :aria-label="t('keys.routing')"
              :title="t('keys.routing')"
              data-handdrawn="control"
            >
              <Route :size="16" aria-hidden="true" />
            </RouterLink>
            <IconButton
              :label="clientText('title')"
              @click="clientConfigTokenId = (row as TokenSummary).id"
            >
              <FileCode2 :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('keys.editKey')"
              @click="openEdit(row as TokenSummary)"
            >
              <Pencil :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('keys.toggleKey')"
              :disabled="togglingIds.has((row as TokenSummary).id)"
              @click="toggleStatus(row as TokenSummary)"
            >
              <LoaderCircle
                v-if="togglingIds.has((row as TokenSummary).id)"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              <PowerOff
                v-else-if="(row as TokenSummary).status === 1"
                :size="16"
                aria-hidden="true"
              />
              <Power v-else :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('keys.deleteKey')"
              tone="danger"
              @click="deleting = row as TokenSummary"
            >
              <Trash2 :size="16" aria-hidden="true" />
            </IconButton>
          </div>
        </template>
        <template #footer>
          <div class="border-t border-[var(--border-subtle)]">
            <TablePagination
              v-model:page="page"
              v-model:page-size="pageSize"
              :total="total"
            />
          </div>
        </template>
      </DataTable>

      <div class="key-list-cards">
        <div
          v-if="loading"
          data-key-mobile-loading
          class="sibling-divide-y sibling-divide-[var(--border-subtle)]"
          aria-hidden="true"
        >
          <div
            v-for="index in pageSize"
            :key="index"
            data-key-mobile-skeleton-row
            class="sibling-space-y-4 p-4"
          >
            <SkeletonBlock rounded="md" class="h-5 w-2/3" />
            <div class="grid grid-cols-2 gap-3">
              <SkeletonBlock rounded="md" class="h-14" />
              <SkeletonBlock rounded="md" class="h-14" />
            </div>
          </div>
        </div>
        <EmptyState
          v-else-if="rows.length === 0"
          :title="t('keys.emptyTitle')"
          illustration="empty-keys"
        />
        <KeyMobileList
          v-else
          :tokens="rows"
          :selected-ids="selected"
          :all-selected="allPageSelected"
          :toggle-all-selected="toggleAllSelected"
          :toggle-selected="toggleSelected"
          :is-toggling="(row: TokenSummary) => togglingIds.has(row.id)"
          :toggle-status="toggleStatus"
          :view-key="copyKey"
          :client-config="(row: TokenSummary) => (clientConfigTokenId = row.id)"
          :edit-key="openEdit"
          :delete-key="(row) => (deleting = row)"
          :routing-enabled="routingEnabled"
          :now="nowSec"
        />
        <div data-key-mobile-pagination :aria-busy="loading">
          <TablePagination
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="total"
          />
        </div>
      </div>
    </ConsoleCard>

    <KeyQuotaPopover
      :id="detailId"
      :token="detail.token.value"
      :anchor="detail.anchor.value"
      :pinned="detail.pinned.value"
      :now="nowSec"
      @close="detail.hide()"
    />

    <!-- create / edit -->

    <KeyFormModal
      :open="formOpen"
      :editing="editing"
      :models="models"
      @close="formOpen = false"
      @saved="onSaved"
    />

    <KeyRevealModal
      :open="revealTokenId !== null"
      :token-id="revealTokenId"
      @close="revealTokenId = null"
    />
    <KeyClientConfigModal
      v-if="clientConfigTokenId !== null"
      :token-id="clientConfigTokenId"
      @close="clientConfigTokenId = null"
    />

    <!-- delete confirm -->
    <ConfirmDialog
      :open="deleting !== null || batchDeleting"
      :title="t('keys.deleteTitle')"
      :message="
        batchDeleting
          ? t('keys.batchDeleteMessage', { count: selected.length })
          : t('keys.deleteMessage', { name: deleting?.name ?? '' })
      "
      :confirm-text="t('common.delete')"
      :cancel-text="t('keys.thinkAgain')"
      :loading="deleteLoading"
      @confirm="confirmDelete"
      @cancel="closeDelete"
    />
  </div>
</template>

<style scoped>
/* The card width, not the viewport, decides whether the table fits: the
   sidebar takes 0-220px of the same viewport. Below 60rem the table's columns
   wrap into tall rows, so the card list is used instead. */
.key-list {
  container-type: inline-size;
}
.key-list-table {
  display: none;
}
@container (min-width: 60rem) {
  .key-list-table {
    display: block;
  }
  .key-list-cards {
    display: none;
  }
}
</style>
