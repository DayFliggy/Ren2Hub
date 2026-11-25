<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import { AlertTriangle, ArrowLeft, Plus } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'

import { upstreamsApi, type UpstreamAccount } from '@/api/upstreams'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import UpstreamAccountList from '@/features/upstreams/components/UpstreamAccountList.vue'
import UpstreamAccountModal from '@/features/upstreams/components/UpstreamAccountModal.vue'
import UpstreamWorkspace, {
  type UpstreamWorkspaceTab,
} from '@/features/upstreams/components/UpstreamWorkspace.vue'
import {
  accountAlert,
  upstreamErrorText,
} from '@/features/upstreams/lib/upstreamText'
import { useAuthStore } from '@/stores/auth'

const TABS: readonly UpstreamWorkspaceTab[] = ['keys', 'bindings', 'groups']

const { t, te } = useI18n()
const auth = useAuthStore()
const toast = useToast()
const route = useRoute()
const router = useRouter()
const request = useLatestRequest()
const isDesktop = useMediaQuery('(min-width: 1024px)')

const canSensitiveWrite = computed(() =>
  auth.hasPermission('upstream', 'sensitive_write')
)

const accounts = ref<UpstreamAccount[]>([])
const loading = ref(true)
const refreshing = ref(false)
const loadError = ref('')
const keyword = ref('')
const platform = ref('')
const abnormalOnly = ref(false)
const creating = ref(false)
const deleting = ref<UpstreamAccount | null>(null)
const deleteBusy = ref(false)
/** A missing ?account is reloaded once, never silently swapped. */
const reloadedFor = ref<number | null>(null)

const errorText = (error: unknown) => upstreamErrorText(error, t, te)

const tab = computed<UpstreamWorkspaceTab>(() => {
  const raw = route.query.tab
  return TABS.find((item) => item === raw) ?? 'keys'
})
const selectedId = computed(() => {
  const raw = route.query.account
  const id = typeof raw === 'string' && /^\d+$/.test(raw) ? Number(raw) : 0
  return id > 0 ? id : null
})
const selectedAccount = computed(
  () =>
    accounts.value.find((account) => account.id === selectedId.value) ?? null
)
const needsAttention = (account: UpstreamAccount) =>
  accountAlert(account, t, te) !== null
const abnormalCount = computed(
  () => accounts.value.filter(needsAttention).length
)
const visibleAccounts = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return accounts.value.filter(
    (account) =>
      (!platform.value || account.platform === platform.value) &&
      (!abnormalOnly.value || needsAttention(account)) &&
      (!query ||
        account.name.toLowerCase().includes(query) ||
        account.base_url.toLowerCase().includes(query))
  )
})
const selectedHidden = computed(
  () =>
    selectedAccount.value !== null &&
    !visibleAccounts.value.includes(selectedAccount.value)
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

/** Page query with `patch` applied; null removes a key, invalid tabs drop. */
function routeQuery(patch: Record<string, string | null>): LocationQueryRaw {
  const query: LocationQueryRaw = { ...route.query }
  delete query.tab
  if (tab.value !== 'keys') query.tab = tab.value
  Object.assign(query, patch)
  for (const key of Object.keys(query))
    if (query[key] === null) delete query[key]
  return query
}

function selectAccount(id: number | null) {
  void router.replace({
    query: routeQuery({ account: id === null ? null : String(id) }),
  })
}

/** The one place that decides which site the page shows. */
function ensureSelection() {
  if (loading.value || loadError.value) return
  if (route.query.account !== undefined && selectedId.value === null)
    return selectAccount(null)
  const id = selectedId.value
  if (id !== null) {
    if (!selectedAccount.value && reloadedFor.value !== id) {
      reloadedFor.value = id
      void load(true)
    }
    return
  }
  const first = visibleAccounts.value[0]
  if (isDesktop.value && first) selectAccount(first.id)
}

function setTab(next: UpstreamWorkspaceTab) {
  void router.replace({
    query: routeQuery({ tab: next === 'keys' ? null : next }),
  })
}

function showList() {
  void router.push({ query: routeQuery({ account: null }) })
}

function clearFilters() {
  keyword.value = ''
  platform.value = ''
  abnormalOnly.value = false
}

/** Next visible site after `id`, else the previous one. */
function neighbourOf(id: number) {
  const list = visibleAccounts.value
  const index = list.findIndex((account) => account.id === id)
  if (index < 0) return list[0]?.id ?? null
  return list[index + 1]?.id ?? list[index - 1]?.id ?? null
}

async function confirmDelete() {
  const account = deleting.value
  if (!account) return
  deleteBusy.value = true
  try {
    await upstreamsApi.remove(account.id)
    toast.success(t('upstreams.deleted'))
    deleting.value = null
    const next = isDesktop.value ? neighbourOf(account.id) : null
    // Drop it now so selection never falls back to the deleted site.
    accounts.value = accounts.value.filter((item) => item.id !== account.id)
    if (selectedId.value === account.id) selectAccount(next)
  } catch (error) {
    toast.error(errorText(error))
  } finally {
    deleteBusy.value = false
  }
  await load(true)
}

async function onCreated() {
  creating.value = false
  await load(true)
}

watch(
  () => [loading.value, route.query.account, isDesktop.value] as const,
  ensureSelection
)

onMounted(() => void load())
</script>
<template>
  <div>
    <ConsoleCard v-if="loadError">
      <div
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
    </ConsoleCard>

    <ConsoleCard v-else-if="!loading && !accounts.length">
      <div class="flex flex-col items-center gap-4 py-8">
        <EmptyState
          :title="t('upstreams.empty')"
          :hint="t('upstreams.emptyHint')"
        />
        <ConsoleButton
          :disabled="!canSensitiveWrite"
          data-upstream-add
          @click="creating = true"
        >
          <Plus :size="14" aria-hidden="true" />
          {{ t('upstreams.add') }}
        </ConsoleButton>
      </div>
    </ConsoleCard>

    <div
      v-else
      class="flex min-h-0 rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] lg:h-[calc(100dvh-9.5rem)] lg:min-h-[36rem] lg:overflow-hidden"
      data-handdrawn="surface"
      data-upstream-split
    >
      <aside
        class="min-w-0 flex-col lg:flex lg:w-72 lg:shrink-0 lg:border-r lg:border-[var(--border-subtle)] xl:w-80"
        :class="selectedId ? 'hidden' : 'flex w-full'"
        :aria-label="t('upstreams.sites')"
      >
        <UpstreamAccountList
          v-model:keyword="keyword"
          v-model:platform="platform"
          v-model:abnormal-only="abnormalOnly"
          :accounts="visibleAccounts"
          :total="accounts.length"
          :abnormal="abnormalCount"
          :selected-id="selectedId"
          :selected-hidden="selectedHidden"
          :tab="tab"
          :loading="loading"
          :refreshing="refreshing"
          :can-add="canSensitiveWrite"
          :replace="isDesktop"
          @refresh="load(true)"
          @add="creating = true"
          @clear-filters="clearFilters"
        />
      </aside>

      <section
        class="min-w-0 flex-1 flex-col lg:flex"
        :class="selectedId ? 'flex' : 'hidden'"
        :aria-label="t('upstreams.workspace')"
      >
        <UpstreamWorkspace
          v-if="selectedAccount"
          :account="selectedAccount"
          :tab="tab"
          @update:tab="setTab"
          @delete="deleting = $event"
          @changed="load(true)"
          @back="showList"
        />
        <div
          v-else-if="loading || (selectedId && refreshing)"
          class="space-y-4 p-5"
          aria-hidden="true"
          data-upstream-workspace-loading
        >
          <SkeletonBlock rounded="md" class="h-7 w-1/2" />
          <SkeletonBlock rounded="md" class="h-4 w-3/4" />
          <SkeletonBlock rounded="md" class="h-4 w-2/3" />
          <SkeletonBlock rounded="md" class="h-10" />
          <SkeletonBlock
            v-for="index in 4"
            :key="index"
            rounded="md"
            class="h-14"
          />
        </div>
        <div
          v-else-if="selectedId"
          class="flex flex-1 flex-col p-4"
          data-upstream-not-found
        >
          <IconButton
            class="lg:hidden"
            :label="t('upstreams.backToList')"
            data-upstream-back
            @click="showList"
          >
            <ArrowLeft :size="17" aria-hidden="true" />
          </IconButton>
          <div class="flex flex-1 items-center justify-center">
            <EmptyState
              :title="t('upstreams.notFound')"
              :hint="t('upstreams.notFoundHint')"
            />
          </div>
        </div>
        <div v-else class="hidden flex-1 items-center justify-center lg:flex">
          <EmptyState
            :title="t('upstreams.workspace')"
            :hint="t('upstreams.selectHint')"
          />
        </div>
      </section>
    </div>

    <UpstreamAccountModal
      v-if="creating"
      mode="create"
      :account="null"
      @close="creating = false"
      @done="onCreated"
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
