<script setup lang="ts">
import { ArrowLeft, RefreshCw } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamBinding,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { relativeTime } from '@/utils/format'
import {
  accountAlert,
  accountStatusText,
  accountTone,
  balanceText,
  nextSyncText,
  upstreamErrorText,
  type AccountAlertAction,
} from '../lib/upstreamText'
import UpstreamAccountModal from './UpstreamAccountModal.vue'
import UpstreamBindingsPanel from './UpstreamBindingsPanel.vue'
import UpstreamCreateChannelModal from './UpstreamCreateChannelModal.vue'
import UpstreamGroupsPanel from './UpstreamGroupsPanel.vue'
import UpstreamKeysPanel from './UpstreamKeysPanel.vue'

export type UpstreamWorkspaceTab = 'keys' | 'bindings' | 'groups'
type AccountModalMode = 'settings' | 'reauth' | 'endpoint'

const props = defineProps<{
  account: UpstreamAccount
  tab: UpstreamWorkspaceTab
}>()
const emit = defineEmits<{
  'update:tab': [tab: UpstreamWorkspaceTab]
  delete: [account: UpstreamAccount]
  /** Account data changed; the list should reload in the background. */
  changed: []
  back: []
}>()
const { t, te, locale } = useI18n()
const auth = useAuthStore()
const toast = useToast()
const keyRequest = useLatestRequest()
const bindingRequest = useLatestRequest()

const panelId = 'upstream-workspace-panel'
const keys = ref<UpstreamRemoteKey[]>([])
const bindings = ref<UpstreamBinding[]>([])
const keysLoading = ref(false)
const bindingsLoading = ref(false)
const keysError = ref('')
const bindingsError = ref('')
/** Account whose keys are on screen; null until a load succeeds. */
const keysLoadedFor = ref<number | null>(null)
/** Account whose keys request is in flight, so repeated triggers share it. */
let keysRequestedFor: number | null = null
const bindingsLoaded = ref(false)
const highlight = ref<number | null>(null)
const syncingId = ref<number | null>(null)
const creatingWithNewKey = ref(false)
const accountModal = ref<{ mode: AccountModalMode; id: number } | null>(null)

const canOperate = computed(() => auth.hasPermission('upstream', 'operate'))
const canWrite = computed(() => auth.hasPermission('upstream', 'write'))
const canSensitiveWrite = computed(() =>
  auth.hasPermission('upstream', 'sensitive_write')
)
const canCreateWithNewKey = computed(
  () =>
    canSensitiveWrite.value && auth.hasPermission('channel', 'sensitive_write')
)
const active = computed(() => props.account.status === 'active')
const syncing = computed(() => syncingId.value === props.account.id)
const banner = computed(() => accountAlert(props.account, t, te))
const ALERT_CLASSES: Record<string, string> = {
  danger: 'bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]',
  warning: 'bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]',
  neutral: 'bg-[var(--surface-muted)] text-[var(--text-secondary)]',
}
const alertAllowed = computed(() => {
  const action = banner.value?.action
  if (action === 'reauth') return canSensitiveWrite.value
  if (action === 'enable') return canWrite.value
  if (action === 'sync') return canOperate.value && !syncing.value
  return false
})
const balance = computed(() => balanceText(props.account, t))
const lastSync = computed(() =>
  props.account.last_sync_at
    ? t('upstreams.lastSyncAt', {
        time: relativeTime(props.account.last_sync_at, locale.value),
      })
    : t('upstreams.neverSynced')
)
const nextSync = computed(() =>
  nextSyncText(props.account, Math.floor(Date.now() / 1000), t, locale.value)
)

const keysCounted = computed(
  () => keysLoadedFor.value === props.account.id && !keysLoading.value
)
const tabs = computed(() =>
  (['keys', 'bindings', 'groups'] as const).map((key) => {
    const label = t(`upstreams.detail.tabs.${key}`)
    const count =
      key === 'keys'
        ? keysCounted.value
          ? keys.value.length
          : null
        : key === 'bindings'
          ? bindingsLoaded.value && !bindingsLoading.value
            ? bindings.value.length
            : null
          : props.account.upstream_groups.length
    return {
      key,
      label:
        count === null
          ? label
          : t('upstreams.detail.tabCount', { label, count }),
    }
  })
)
const currentTab = computed({
  get: () => props.tab,
  set: (value: string) => emit('update:tab', value as UpstreamWorkspaceTab),
})

const menuItems = computed<ActionMenuItem[]>(() => [
  {
    key: 'settings',
    label: t('upstreams.actions.edit'),
    disabled: !canWrite.value,
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
])

/** Remote keys load only for active accounts on a tab that shows them. */
async function ensureKeys() {
  const id = props.account.id
  if (!active.value || props.tab === 'groups') return
  if (keysLoadedFor.value === id || keysRequestedFor === id) return
  keysRequestedFor = id
  keysLoading.value = true
  keysError.value = ''
  const result = await keyRequest.run((signal) => upstreamsApi.keys(id, signal))
  if (result.stale) return
  keysRequestedFor = null
  keysLoading.value = false
  if (result.ok) {
    keys.value = result.value
    keysLoadedFor.value = id
    return
  }
  keys.value = []
  keysError.value = upstreamErrorText(result.error, t, te)
}

function clearKeys() {
  keyRequest.cancel()
  keysRequestedFor = null
  keysLoading.value = false
  keys.value = []
  keysError.value = ''
  keysLoadedFor.value = null
}

async function loadBindings() {
  const id = props.account.id
  bindingsLoading.value = true
  bindingsError.value = ''
  const result = await bindingRequest.run((signal) =>
    upstreamsApi.bindings(id, signal)
  )
  if (result.stale) return
  bindingsLoading.value = false
  bindingsLoaded.value = result.ok
  bindingsError.value = result.ok ? '' : upstreamErrorText(result.error, t, te)
  bindings.value = result.ok ? result.value : []
}

/** Single entry after any change made from the workspace. */
function reload() {
  keyRequest.cancel()
  keysRequestedFor = null
  keysLoadedFor.value = null
  void loadBindings()
  void ensureKeys()
  emit('changed')
}

/** Finish an account-level action started for `id`. */
function settle(id: number) {
  if (props.account.id === id) reload()
  else emit('changed')
}

async function sync() {
  const id = props.account.id
  syncingId.value = id
  try {
    await upstreamsApi.sync(id)
    toast.success(t('upstreams.synced'))
  } catch (error) {
    toast.error(upstreamErrorText(error, t, te))
  } finally {
    syncingId.value = null
  }
  settle(id)
}

function onMenu(key: string) {
  if (key === 'delete') return emit('delete', props.account)
  if (key === 'settings' || key === 'reauth' || key === 'endpoint')
    accountModal.value = { mode: key, id: props.account.id }
}

function onAlertAction(action: AccountAlertAction) {
  if (action === 'sync') return void sync()
  accountModal.value = {
    mode: action === 'enable' ? 'settings' : 'reauth',
    id: props.account.id,
  }
}

function onAccountModalDone() {
  const id = accountModal.value?.id
  accountModal.value = null
  if (id) settle(id)
}

function showBinding(channelId: number) {
  highlight.value = channelId
  emit('update:tab', 'bindings')
}

function onChannelCreated(channelId: number) {
  creatingWithNewKey.value = false
  showBinding(channelId)
  reload()
}

watch(
  () => props.account.id,
  () => {
    keyRequest.cancel()
    bindingRequest.cancel()
    keysRequestedFor = null
    keys.value = []
    bindings.value = []
    keysError.value = ''
    bindingsError.value = ''
    keysLoadedFor.value = null
    bindingsLoaded.value = false
    keysLoading.value = false
    highlight.value = null
    creatingWithNewKey.value = false
    accountModal.value = null
    void loadBindings()
    void ensureKeys()
  },
  { immediate: true }
)

// Same account, new status or tab: recovery loads keys, deactivation drops them.
watch(
  () => [active.value, props.tab] as const,
  ([isActive, tab]) => {
    if (tab !== 'bindings') highlight.value = null
    if (isActive) void ensureKeys()
    else clearKeys()
  }
)
</script>
<template>
  <div
    class="subtle-scroll @container min-h-0 flex-1 lg:overflow-y-auto"
    data-upstream-workspace
  >
    <header class="space-y-3 px-4 pt-4 sm:px-5">
      <div class="flex flex-wrap items-start gap-x-3 gap-y-2">
        <div class="flex min-w-0 flex-1 basis-64 items-start gap-2">
          <IconButton
            class="shrink-0 lg:hidden"
            :label="t('upstreams.backToList')"
            data-upstream-back
            @click="emit('back')"
          >
            <ArrowLeft :size="17" aria-hidden="true" />
          </IconButton>
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h2
                class="display-title min-w-0 truncate text-lg font-bold text-[var(--text-primary)]"
                :title="account.name"
              >
                {{ account.name }}
              </h2>
              <StatusChip :tone="accountTone(account.status)">
                {{ accountStatusText(account.status, t, te) }}
              </StatusChip>
              <StatusChip tone="neutral">
                {{ t(`upstreams.platforms.${account.platform}`) }}
              </StatusChip>
              <StatusChip v-if="account.has_proxy" tone="neutral">
                {{ t('upstreams.proxyBadge') }}
              </StatusChip>
            </div>
            <p class="mt-1 break-all text-xs text-[var(--text-tertiary)]">
              {{ account.base_url }}
            </p>
          </div>
        </div>
        <div
          class="flex w-full flex-wrap items-center gap-2 @xl:ml-auto @xl:w-auto"
          data-upstream-actions
        >
          <span class="@xl:hidden">
            <IconButton
              :label="t('upstreams.actions.sync')"
              :disabled="!canOperate || syncing"
              data-upstream-sync
              @click="sync"
            >
              <RefreshCw
                :size="16"
                :class="{ 'animate-spin': syncing }"
                aria-hidden="true"
              />
            </IconButton>
          </span>
          <span class="hidden @xl:inline-flex">
            <ConsoleButton
              variant="secondary"
              size="sm"
              :loading="syncing"
              :disabled="!canOperate"
              data-upstream-sync
              @click="sync"
            >
              <RefreshCw v-if="!syncing" :size="14" aria-hidden="true" />
              {{ t('upstreams.actions.sync') }}
            </ConsoleButton>
          </span>
          <ConsoleButton
            size="sm"
            class="min-w-0 flex-1 @xl:flex-none"
            :disabled="!canCreateWithNewKey || !active"
            :title="t('upstreams.detail.createKeyAndChannel')"
            data-upstream-create-key-channel
            @click="creatingWithNewKey = true"
          >
            <!-- The button never wraps; long labels or large text truncate. -->
            <span class="min-w-0 truncate">
              {{ t('upstreams.detail.createKeyAndChannel') }}
            </span>
          </ConsoleButton>
          <ActionMenu
            :items="menuItems"
            :label="t('upstreams.actions.menu', { name: account.name })"
            @select="onMenu"
          />
        </div>
      </div>
      <ul
        class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-[var(--text-secondary)]"
      >
        <li
          class="tabular-nums text-[var(--accent-text)]"
          :title="balance.hint"
          data-upstream-balance
        >
          {{ balance.text }}
        </li>
        <li v-if="account.site_meta.username">
          {{ t('upstreams.username', { name: account.site_meta.username }) }}
        </li>
        <li v-if="account.site_meta.upstream_group">
          {{
            t('upstreams.detail.userGroup', {
              name: account.site_meta.upstream_group,
            })
          }}
        </li>
        <li class="tabular-nums">{{ lastSync }}</li>
        <li v-if="nextSync" class="tabular-nums" data-upstream-next-sync>
          {{ nextSync }}
        </li>
      </ul>
      <div
        v-if="banner"
        role="status"
        class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg px-3 py-2 text-sm"
        :class="ALERT_CLASSES[banner.tone]"
        data-upstream-banner
      >
        <div class="min-w-0 flex-1 basis-56">
          <p class="font-semibold">{{ banner.title }}</p>
          <p v-if="banner.detail" class="break-words text-xs">
            {{ banner.detail }}
          </p>
          <p v-if="banner.action && !alertAllowed && !syncing" class="text-xs">
            {{ t('upstreams.alerts.needPermission') }}
          </p>
        </div>
        <ConsoleButton
          v-if="banner.action"
          size="sm"
          variant="secondary"
          class="w-full @xl:w-auto"
          :disabled="!alertAllowed"
          :loading="banner.action === 'sync' && syncing"
          data-upstream-alert-action
          @click="onAlertAction(banner.action)"
        >
          {{
            banner.action === 'reauth'
              ? t('upstreams.actions.reauth')
              : banner.action === 'enable'
                ? t('upstreams.alerts.enable')
                : t('upstreams.alerts.retrySync')
          }}
        </ConsoleButton>
      </div>
    </header>

    <div
      class="sticky top-0 z-10 bg-[var(--surface-solid)] px-4 pb-3 pt-4 sm:px-5"
    >
      <ConsoleTabs v-model="currentTab" :items="tabs" :panel-id="panelId" />
    </div>
    <div
      :id="panelId"
      role="tabpanel"
      :aria-labelledby="`${panelId}-tab-${tab}`"
      class="px-4 pb-5 sm:px-5"
    >
      <template v-if="tab === 'keys'">
        <p
          v-if="!active"
          class="rounded-lg bg-[var(--surface-muted)] px-3 py-2 text-sm text-[var(--text-secondary)]"
          data-upstream-keys-inactive
        >
          {{
            t('upstreams.keys.inactive', {
              status: accountStatusText(account.status, t, te),
            })
          }}
        </p>
        <UpstreamKeysPanel
          v-else
          :key="account.id"
          :account="account"
          :keys="keys"
          :bindings="bindings"
          :loading="keysLoading"
          :error="keysError"
          @reload="reload"
          @retry="ensureKeys"
          @show-binding="showBinding"
        />
      </template>
      <UpstreamBindingsPanel
        v-else-if="tab === 'bindings'"
        :key="account.id"
        :account="account"
        :bindings="bindings"
        :keys="keys"
        :loading="bindingsLoading"
        :error="bindingsError"
        :highlight="highlight"
        @reload="reload"
        @retry="loadBindings"
      />
      <UpstreamGroupsPanel v-else :account="account" />
    </div>

    <UpstreamCreateChannelModal
      v-if="creatingWithNewKey"
      :account="account"
      :remote-key="null"
      @close="creatingWithNewKey = false"
      @created="onChannelCreated"
      @partial="reload"
    />
    <UpstreamAccountModal
      v-if="accountModal"
      :mode="accountModal.mode"
      :account="account"
      @close="accountModal = null"
      @done="onAccountModalDone"
    />
  </div>
</template>
