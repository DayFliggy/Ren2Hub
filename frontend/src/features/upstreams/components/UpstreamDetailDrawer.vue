<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamBinding,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useAuthStore } from '@/stores/auth'
import {
  accountStatusText,
  accountTone,
  balanceText,
  upstreamErrorText,
} from '../lib/upstreamText'
import UpstreamBindingsPanel from './UpstreamBindingsPanel.vue'
import UpstreamCreateChannelModal from './UpstreamCreateChannelModal.vue'
import UpstreamKeysPanel from './UpstreamKeysPanel.vue'

const props = defineProps<{ account: UpstreamAccount | null }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t, te } = useI18n()
const auth = useAuthStore()
const keyRequest = useLatestRequest()
const bindingRequest = useLatestRequest()

const panelId = 'upstream-detail-panel'
const tab = ref('groups')
const keys = ref<UpstreamRemoteKey[]>([])
const bindings = ref<UpstreamBinding[]>([])
const keysLoading = ref(false)
const bindingsLoading = ref(false)
const keysError = ref('')
const bindingsError = ref('')
const creatingWithNewKey = ref(false)

const canCreateWithNewKey = computed(
  () =>
    auth.hasPermission('upstream', 'sensitive_write') &&
    auth.hasPermission('channel', 'sensitive_write')
)
const inactive = computed(() => props.account?.status !== 'active')
const tabs = computed(() =>
  (['groups', 'keys', 'bindings'] as const).map((key) => ({
    key,
    label: t(`upstreams.detail.tabs.${key}`),
  }))
)

function ratioText(ratio: string) {
  if (ratio === 'auto') return t('upstreams.detail.ratioAuto')
  return ratio === '' ? t('upstreams.detail.ratioUnknown') : `${ratio}×`
}

function groupName(id: number) {
  const group = props.account?.upstream_groups.find((item) => item.id === id)
  return group?.name ?? t('upstreams.detail.subscriptionGroup', { id })
}

async function loadKeys() {
  const id = props.account?.id
  if (!id) return
  keysLoading.value = true
  const result = await keyRequest.run((signal) => upstreamsApi.keys(id, signal))
  if (result.stale) return
  keysLoading.value = false
  keysError.value = result.ok ? '' : upstreamErrorText(result.error, t, te)
  keys.value = result.ok ? result.value : []
}

async function loadBindings() {
  const id = props.account?.id
  if (!id) return
  bindingsLoading.value = true
  const result = await bindingRequest.run((signal) =>
    upstreamsApi.bindings(id, signal)
  )
  if (result.stale) return
  bindingsLoading.value = false
  bindingsError.value = result.ok ? '' : upstreamErrorText(result.error, t, te)
  bindings.value = result.ok ? result.value : []
}

function reload() {
  void loadKeys()
  void loadBindings()
  emit('changed')
}

function onChannelCreated() {
  creatingWithNewKey.value = false
  tab.value = 'bindings'
  reload()
}

watch(
  () => props.account?.id,
  (id) => {
    keyRequest.cancel()
    bindingRequest.cancel()
    keys.value = []
    bindings.value = []
    keysError.value = ''
    bindingsError.value = ''
    creatingWithNewKey.value = false
    tab.value = 'groups'
    if (id) {
      void loadKeys()
      void loadBindings()
    }
  },
  { immediate: true }
)
</script>
<template>
  <ConsoleModal
    :open="account !== null"
    :title="t('upstreams.detail.title', { name: account?.name ?? '' })"
    :subtitle="account?.base_url ?? ''"
    presentation="drawer"
    size="lg"
    @close="emit('close')"
  >
    <template v-if="account">
      <div class="mb-4 flex flex-wrap items-center gap-2 text-xs">
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
        <span
          v-if="account.site_meta.username"
          class="text-[var(--text-tertiary)]"
        >
          {{ t('upstreams.username', { name: account.site_meta.username }) }}
        </span>
      </div>
      <p
        v-if="account.last_error"
        role="alert"
        class="mb-4 break-words rounded-lg bg-[var(--status-danger-soft)] px-3 py-2 text-xs text-[var(--status-danger-text)]"
      >
        {{ t('upstreams.lastError', { text: account.last_error }) }}
      </p>
      <ConsoleTabs
        v-model="tab"
        :items="tabs"
        :panel-id="panelId"
        class="mb-4"
      />
      <div
        :id="panelId"
        role="tabpanel"
        :aria-labelledby="`${panelId}-tab-${tab}`"
      >
        <section v-if="tab === 'groups'" class="space-y-5">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="text-sm text-[var(--text-secondary)]">
              {{
                account.site_meta.upstream_group
                  ? t('upstreams.detail.userGroup', {
                      name: account.site_meta.upstream_group,
                    })
                  : t('upstreams.detail.groups')
              }}
            </p>
            <ConsoleButton
              size="sm"
              :disabled="!canCreateWithNewKey || inactive"
              data-upstream-create-key-channel
              @click="creatingWithNewKey = true"
            >
              {{ t('upstreams.detail.createKeyAndChannel') }}
            </ConsoleButton>
          </div>
          <p
            v-if="!account.upstream_groups.length"
            class="text-sm text-[var(--text-tertiary)]"
          >
            {{ t('upstreams.detail.noGroups') }}
          </p>
          <ul
            v-else
            class="divide-y divide-[var(--border-subtle)] rounded-xl border border-[var(--border-subtle)]"
            data-upstream-groups
          >
            <li
              v-for="group in account.upstream_groups"
              :key="`${group.id ?? ''}:${group.name}`"
              class="flex items-center justify-between gap-3 px-3 py-2 text-sm"
            >
              <span class="min-w-0 truncate text-[var(--text-primary)]">
                {{ group.name }}
                <span
                  v-if="group.platform"
                  class="ml-1 text-xs text-[var(--text-tertiary)]"
                >
                  {{ group.platform }}
                </span>
              </span>
              <span class="shrink-0 tabular-nums text-[var(--text-secondary)]">
                {{ ratioText(group.ratio) }}
              </span>
            </li>
          </ul>
          <div>
            <h3 class="mb-2 text-sm font-semibold text-[var(--text-primary)]">
              {{ t('upstreams.detail.subscriptions') }}
            </h3>
            <p
              v-if="!account.subscriptions.length"
              class="text-sm text-[var(--text-tertiary)]"
            >
              {{ t('upstreams.detail.noSubscriptions') }}
            </p>
            <ul v-else class="space-y-1 text-sm">
              <li
                v-for="item in account.subscriptions"
                :key="item.id"
                class="flex flex-wrap items-center gap-2"
              >
                <span class="text-[var(--text-primary)]">
                  {{ groupName(item.upstream_group_id) }}
                </span>
                <StatusChip tone="neutral">{{ item.status }}</StatusChip>
                <span
                  v-if="item.expires_at"
                  class="text-xs text-[var(--text-tertiary)]"
                >
                  {{ t('upstreams.detail.expiresAt') }}
                  {{ item.expires_at }}
                </span>
              </li>
            </ul>
          </div>
        </section>
        <UpstreamKeysPanel
          v-else-if="tab === 'keys'"
          :account="account"
          :keys="keys"
          :bindings="bindings"
          :loading="keysLoading"
          :error="keysError"
          @reload="reload"
        />
        <UpstreamBindingsPanel
          v-else
          :account="account"
          :bindings="bindings"
          :loading="bindingsLoading"
          :error="bindingsError"
          @reload="reload"
        />
      </div>
      <UpstreamCreateChannelModal
        v-if="creatingWithNewKey"
        :account="account"
        :remote-key="null"
        @close="creatingWithNewKey = false"
        @created="onChannelCreated"
        @partial="reload"
      />
    </template>
  </ConsoleModal>
</template>
