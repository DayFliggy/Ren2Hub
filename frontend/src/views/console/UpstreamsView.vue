<script setup lang="ts">
import { AlertTriangle, Plus, RefreshCw } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import {
  UPSTREAM_PLATFORMS,
  upstreamsApi,
  type UpstreamAccount,
} from '@/api/upstreams'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FilterSelect, {
  type SelectOption,
} from '@/components/common/FilterSelect.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import UpstreamAccountModal, {
  type UpstreamAccountModalMode,
} from '@/features/upstreams/components/UpstreamAccountModal.vue'
import UpstreamDetailDrawer from '@/features/upstreams/components/UpstreamDetailDrawer.vue'
import {
  accountStatusText,
  accountTone,
  balanceText,
  upstreamErrorText,
} from '@/features/upstreams/lib/upstreamText'
import { useAuthStore } from '@/stores/auth'
import { relativeTime } from '@/utils/format'

const { t, te, locale } = useI18n()
const auth = useAuthStore()
const toast = useToast()
const route = useRoute()
const router = useRouter()
const request = useLatestRequest()

const canOperate = computed(() => auth.hasPermission('upstream', 'operate'))
const canWrite = computed(() => auth.hasPermission('upstream', 'write'))
const canSensitiveWrite = computed(() =>
  auth.hasPermission('upstream', 'sensitive_write')
)

const accounts = ref<UpstreamAccount[]>([])
const loading = ref(true)
const refreshing = ref(false)
const loadError = ref('')
const keyword = ref('')
const platform = ref('')
const busyId = ref<number | null>(null)
const modal = ref<{
  mode: UpstreamAccountModalMode
  account: UpstreamAccount | null
} | null>(null)
const deleting = ref<UpstreamAccount | null>(null)
const deleteBusy = ref(false)

const errorText = (error: unknown) => upstreamErrorText(error, t, te)

const columns = computed<TableColumn[]>(() => [
  { key: 'name', label: t('upstreams.columns.name') },
  { key: 'platform', label: t('upstreams.columns.platform'), width: '110px' },
  { key: 'status', label: t('upstreams.columns.status'), width: '150px' },
  { key: 'balance', label: t('upstreams.columns.balance'), width: '160px' },
  { key: 'groups', label: t('upstreams.columns.groups'), width: '110px' },
  { key: 'lastSync', label: t('upstreams.columns.lastSync'), width: '130px' },
  {
    key: 'actions',
    label: t('upstreams.columns.actions'),
    width: '72px',
    align: 'right',
  },
])

const platformOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('upstreams.allPlatforms') },
  ...UPSTREAM_PLATFORMS.map((value) => ({
    value,
    label: t(`upstreams.platforms.${value}`),
  })),
])

const visibleAccounts = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return accounts.value.filter(
    (account) =>
      (!platform.value || account.platform === platform.value) &&
      (!query ||
        account.name.toLowerCase().includes(query) ||
        account.base_url.toLowerCase().includes(query))
  )
})

const emptyTitle = computed(() =>
  accounts.value.length ? t('upstreams.noMatch') : t('upstreams.empty')
)
const emptyHint = computed(() =>
  accounts.value.length ? '' : t('upstreams.emptyHint')
)

const detailId = computed(() => {
  const raw = route.query.account
  const id = typeof raw === 'string' && /^\d+$/.test(raw) ? Number(raw) : 0
  return id > 0 ? id : null
})
const detailAccount = computed(
  () => accounts.value.find((account) => account.id === detailId.value) ?? null
)

async function load(background = false) {
  if (background) refreshing.value = true
  else loading.value = true
  const result = await request.run((signal) => upstreamsApi.list(signal))
  if (result.stale) return
  loading.value = false
  refreshing.value = false
  if (result.ok) {
    accounts.value = result.value
    loadError.value = ''
    return
  }
  if (background) toast.error(errorText(result.error))
  else loadError.value = errorText(result.error)
}

function syncText(account: UpstreamAccount) {
  return account.last_sync_at
    ? relativeTime(account.last_sync_at, locale.value)
    : t('upstreams.neverSynced')
}

function actionItems(account: UpstreamAccount): ActionMenuItem[] {
  const busy = busyId.value === account.id
  return [
    { key: 'details', label: t('upstreams.actions.details') },
    {
      key: 'sync',
      label: t('upstreams.actions.sync'),
      disabled: !canOperate.value || busy,
    },
    {
      key: 'edit',
      label: t('upstreams.actions.edit'),
      disabled: !canWrite.value,
      separated: true,
    },
    {
      key: 'reauth',
      label: t('upstreams.actions.reauth'),
      disabled: !canSensitiveWrite.value,
    },
    {
      key: 'endpoint',
      label: t('upstreams.actions.endpoint'),
      disabled: !canSensitiveWrite.value,
    },
    {
      key: 'delete',
      label: t('upstreams.actions.delete'),
      tone: 'danger',
      disabled: !canSensitiveWrite.value,
      separated: true,
    },
  ]
}

function openDetail(account: UpstreamAccount | null) {
  const query = { ...route.query }
  if (account) query.account = String(account.id)
  else delete query.account
  void router.replace({ query })
}

async function sync(account: UpstreamAccount) {
  busyId.value = account.id
  try {
    await upstreamsApi.sync(account.id)
    toast.success(t('upstreams.synced'))
  } catch (error) {
    toast.error(errorText(error))
  } finally {
    busyId.value = null
  }
  await load(true)
}

function onAction(account: UpstreamAccount, key: string) {
  if (key === 'details') return openDetail(account)
  if (key === 'sync') return void sync(account)
  if (key === 'delete') {
    deleting.value = account
    return
  }
  if (key === 'edit' || key === 'reauth' || key === 'endpoint')
    modal.value = {
      mode: key === 'edit' ? 'settings' : key,
      account,
    }
}

async function confirmDelete() {
  const account = deleting.value
  if (!account) return
  deleteBusy.value = true
  try {
    await upstreamsApi.remove(account.id)
    toast.success(t('upstreams.deleted'))
    deleting.value = null
    if (detailId.value === account.id) openDetail(null)
  } catch (error) {
    toast.error(errorText(error))
  } finally {
    deleteBusy.value = false
  }
  await load(true)
}

async function onModalDone() {
  modal.value = null
  await load(true)
}

watch(detailId, (id) => {
  // A linked account that is not in the current list may be new; reload once.
  if (id && !loading.value && !detailAccount.value) void load(true)
})

onMounted(() => void load())
</script>
<template>
  <div>
    <ConsoleCard :padded="false">
      <div
        class="flex flex-col gap-3 border-b border-[var(--border-subtle)] p-4 lg:flex-row lg:items-center"
      >
        <SearchInput
          v-model="keyword"
          :placeholder="t('upstreams.search')"
          :aria-label="t('upstreams.search')"
          name="upstream-search"
          class="w-full lg:w-72"
        />
        <FilterSelect
          v-model="platform"
          :options="platformOptions"
          :label="t('upstreams.platformFilter')"
          class="w-full lg:w-48"
        />
        <div
          class="order-first flex flex-wrap items-center gap-2 lg:order-none lg:ml-auto"
        >
          <span
            class="mr-auto text-xs tabular-nums text-[var(--text-tertiary)] lg:mr-1"
            aria-live="polite"
          >
            {{ t('upstreams.resultCount', { count: visibleAccounts.length }) }}
          </span>
          <ConsoleButton
            variant="secondary"
            size="sm"
            :loading="refreshing"
            @click="load(true)"
          >
            <RefreshCw v-if="!refreshing" :size="14" aria-hidden="true" />
            {{ t('upstreams.refresh') }}
          </ConsoleButton>
          <ConsoleButton
            size="sm"
            :disabled="!canSensitiveWrite"
            data-upstream-add
            @click="modal = { mode: 'create', account: null }"
          >
            <Plus :size="14" aria-hidden="true" />
            {{ t('upstreams.add') }}
          </ConsoleButton>
        </div>
      </div>

      <div
        v-if="loadError"
        class="flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center"
        role="alert"
      >
        <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
        <p class="mt-3 font-semibold text-[var(--text-primary)]">
          {{ t('upstreams.loadFailed') }}
        </p>
        <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
          {{ loadError }}
        </p>
        <ConsoleButton class="mt-5" variant="secondary" @click="load()">
          {{ t('upstreams.retry') }}
        </ConsoleButton>
      </div>

      <template v-else>
        <DataTable
          class="hidden md:block"
          :columns="columns"
          :rows="visibleAccounts"
          row-key="id"
          :loading="loading"
          adaptive-scroll
          min-table-width="860px"
          :scroll-region-label="t('upstreams.title')"
          :empty-title="emptyTitle"
          :empty-hint="emptyHint"
        >
          <template #cell-name="{ row }">
            <button
              type="button"
              class="block max-w-[280px] text-left"
              data-upstream-open
              @click="openDetail(row as UpstreamAccount)"
            >
              <span
                class="block truncate text-sm font-semibold text-[var(--text-primary)] hover:text-[var(--accent)]"
              >
                {{ (row as UpstreamAccount).name }}
              </span>
              <span
                class="block truncate text-xs text-[var(--text-tertiary)]"
                :title="(row as UpstreamAccount).base_url"
              >
                {{ (row as UpstreamAccount).base_url }}
              </span>
            </button>
          </template>
          <template #cell-platform="{ row }">
            <span class="text-xs font-medium text-[var(--text-secondary)]">
              {{
                t(`upstreams.platforms.${(row as UpstreamAccount).platform}`)
              }}
            </span>
            <StatusChip
              v-if="(row as UpstreamAccount).has_proxy"
              tone="neutral"
              class="ml-1 text-[0.625rem]"
            >
              {{ t('upstreams.proxyBadge') }}
            </StatusChip>
          </template>
          <template #cell-status="{ row }">
            <StatusChip :tone="accountTone((row as UpstreamAccount).status)">
              {{ accountStatusText((row as UpstreamAccount).status, t, te) }}
            </StatusChip>
            <p
              v-if="(row as UpstreamAccount).last_error"
              class="mt-1 max-w-[150px] truncate text-xs text-[var(--status-danger-text)]"
              :title="(row as UpstreamAccount).last_error"
            >
              {{ (row as UpstreamAccount).last_error }}
            </p>
          </template>
          <template #cell-balance="{ row }">
            <span
              class="text-sm tabular-nums text-[var(--text-primary)]"
              :title="balanceText(row as UpstreamAccount, t).hint"
              data-upstream-balance
            >
              {{ balanceText(row as UpstreamAccount, t).text }}
            </span>
          </template>
          <template #cell-groups="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{
                t('upstreams.groupsCount', {
                  count: (row as UpstreamAccount).upstream_groups.length,
                })
              }}
            </span>
          </template>
          <template #cell-lastSync="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{ syncText(row as UpstreamAccount) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <ActionMenu
              :items="actionItems(row as UpstreamAccount)"
              :label="
                t('upstreams.actions.menu', {
                  name: (row as UpstreamAccount).name,
                })
              "
              @select="onAction(row as UpstreamAccount, $event)"
            />
          </template>
        </DataTable>

        <div class="md:hidden" data-upstream-mobile-list>
          <EmptyState
            v-if="!loading && visibleAccounts.length === 0"
            :title="emptyTitle"
            :hint="emptyHint"
            illustration="empty-search"
          />
          <div
            v-for="account in visibleAccounts"
            :key="account.id"
            class="border-b border-[var(--border-subtle)] px-4 py-3"
          >
            <div class="flex items-start gap-2">
              <button
                type="button"
                class="min-w-0 flex-1 text-left"
                @click="openDetail(account)"
              >
                <span
                  class="block truncate text-sm font-semibold text-[var(--text-primary)]"
                >
                  {{ account.name }}
                </span>
                <span
                  class="block truncate text-xs text-[var(--text-tertiary)]"
                >
                  {{ account.base_url }}
                </span>
              </button>
              <ActionMenu
                :items="actionItems(account)"
                :label="t('upstreams.actions.menu', { name: account.name })"
                @select="onAction(account, $event)"
              />
            </div>
            <div class="mt-2 flex flex-wrap items-center gap-2 text-xs">
              <StatusChip :tone="accountTone(account.status)">
                {{ accountStatusText(account.status, t, te) }}
              </StatusChip>
              <span class="font-medium text-[var(--text-secondary)]">
                {{ t(`upstreams.platforms.${account.platform}`) }}
              </span>
              <span
                class="tabular-nums text-[var(--text-primary)]"
                :title="balanceText(account, t).hint"
              >
                {{ balanceText(account, t).text }}
              </span>
              <span class="text-[var(--text-tertiary)]">
                {{
                  t('upstreams.groupsCount', {
                    count: account.upstream_groups.length,
                  })
                }}
                · {{ syncText(account) }}
              </span>
            </div>
            <p
              v-if="account.last_error"
              class="mt-1 break-words text-xs text-[var(--status-danger-text)]"
            >
              {{ t('upstreams.lastError', { text: account.last_error }) }}
            </p>
          </div>
        </div>
      </template>
    </ConsoleCard>

    <UpstreamAccountModal
      v-if="modal"
      :mode="modal.mode"
      :account="modal.account"
      @close="modal = null"
      @done="onModalDone"
    />

    <UpstreamDetailDrawer
      :account="detailAccount"
      @close="openDetail(null)"
      @changed="load(true)"
    />

    <ConfirmDialog
      :open="deleting !== null"
      :title="t('upstreams.deleteTitle')"
      :message="t('upstreams.deleteMessage', { name: deleting?.name ?? '' })"
      :confirm-text="t('upstreams.actions.delete')"
      :loading="deleteBusy"
      @confirm="confirmDelete"
      @cancel="deleting = null"
    />
  </div>
</template>
