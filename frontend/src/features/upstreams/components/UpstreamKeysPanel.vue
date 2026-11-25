<script setup lang="ts">
import { computed, ref } from 'vue'
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
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { formatTime } from '@/utils/format'
import {
  keyExpiryText,
  keyQuotaText,
  keyStatusText,
  keyTone,
  upstreamErrorText,
} from '../lib/upstreamText'
import UpstreamBindChannelModal from './UpstreamBindChannelModal.vue'
import UpstreamCreateChannelModal from './UpstreamCreateChannelModal.vue'
import UpstreamKeyFormModal from './UpstreamKeyFormModal.vue'

const props = defineProps<{
  account: UpstreamAccount
  keys: UpstreamRemoteKey[]
  bindings: UpstreamBinding[]
  loading: boolean
  error: string
}>()
const emit = defineEmits<{
  reload: []
  retry: []
  showBinding: [channelId: number]
}>()
const { t, te } = useI18n()
const auth = useAuthStore()
const toast = useToast()

type Dialog =
  | { kind: 'form'; key: UpstreamRemoteKey | null }
  | { kind: 'channel' | 'bind' | 'delete'; key: UpstreamRemoteKey }
// One column template for the header and every row in the wide layout.
const ROW_COLUMNS =
  '@3xl:grid-cols-[minmax(9rem,1.6fr)_minmax(5rem,1fr)_6rem_minmax(7rem,1fr)_7.5rem_6rem_2.75rem]'
const dialog = ref<Dialog | null>(null)
const busyKey = ref<number | null>(null)

const canWrite = computed(() => auth.hasPermission('upstream', 'write'))
const canSensitiveWrite = computed(() =>
  auth.hasPermission('upstream', 'sensitive_write')
)
const canLinkChannel = computed(
  () => canWrite.value && auth.hasPermission('channel', 'sensitive_write')
)
const inactive = computed(() => props.account.status !== 'active')
const boundChannel = computed(() => {
  const map = new Map<number, number>()
  for (const binding of props.bindings)
    map.set(binding.remote_key_id, binding.channel_id)
  return map
})

function groupText(key: UpstreamRemoteKey) {
  if (key.upstream_group) return key.upstream_group
  if (key.upstream_group_id) {
    const group = props.account.upstream_groups.find(
      (item) => item.id === key.upstream_group_id
    )
    return group?.name ?? `#${key.upstream_group_id}`
  }
  return t('upstreams.keys.defaultGroup')
}

function items(key: UpstreamRemoteKey): ActionMenuItem[] {
  const remote = !inactive.value && busyKey.value !== key.id
  const bound = boundChannel.value.has(key.id)
  const enabled = key.status === 'active'
  return [
    {
      key: 'edit',
      label: t('upstreams.keys.actions.edit'),
      disabled: !canWrite.value || !remote,
    },
    {
      key: 'toggle',
      label: enabled
        ? t('upstreams.keys.actions.disable')
        : t('upstreams.keys.actions.enable'),
      disabled: !canWrite.value || !remote || key.status === 'missing',
    },
    {
      key: 'channel',
      label: t('upstreams.keys.actions.createChannel'),
      disabled: !canLinkChannel.value || !remote || bound,
      separated: true,
    },
    {
      key: 'bind',
      label: t('upstreams.keys.actions.bind'),
      disabled: !canLinkChannel.value || bound,
    },
    {
      key: 'delete',
      label: t('upstreams.keys.actions.delete'),
      tone: 'danger',
      disabled: !canSensitiveWrite.value || !remote,
      separated: true,
    },
  ]
}

async function toggle(key: UpstreamRemoteKey) {
  busyKey.value = key.id
  try {
    await upstreamsApi.setKeyEnabled(
      props.account.id,
      key.id,
      key.status !== 'active'
    )
    toast.success(t('upstreams.keys.statusChanged'))
    emit('reload')
  } catch (error) {
    toast.error(upstreamErrorText(error, t, te))
  } finally {
    busyKey.value = null
  }
}

async function remove() {
  const current = dialog.value
  if (current?.kind !== 'delete') return
  busyKey.value = current.key.id
  try {
    await upstreamsApi.removeKey(props.account.id, current.key.id)
    toast.success(t('upstreams.keys.deleted'))
    dialog.value = null
    emit('reload')
  } catch (error) {
    toast.error(upstreamErrorText(error, t, te))
  } finally {
    busyKey.value = null
  }
}

function onSelect(key: UpstreamRemoteKey, action: string) {
  if (action === 'toggle') return void toggle(key)
  if (action === 'edit') dialog.value = { kind: 'form', key }
  if (action === 'channel' || action === 'bind' || action === 'delete')
    dialog.value = { kind: action, key }
}

function openBinding(key: UpstreamRemoteKey) {
  const channelId = boundChannel.value.get(key.id)
  if (channelId) emit('showBinding', channelId)
}

function done() {
  dialog.value = null
  emit('reload')
}
</script>

<template>
  <section class="@container space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <p
        v-if="!loading && !error"
        class="text-xs tabular-nums text-[var(--text-tertiary)]"
      >
        {{ t('upstreams.keys.count', { count: keys.length }) }}
      </p>
      <ConsoleButton
        size="sm"
        class="ml-auto"
        :disabled="!canSensitiveWrite || inactive"
        data-upstream-key-create
        @click="dialog = { kind: 'form', key: null }"
      >
        {{ t('upstreams.keys.create') }}
      </ConsoleButton>
    </div>
    <div
      v-if="error"
      role="alert"
      class="flex flex-wrap items-start gap-3 rounded-lg bg-[var(--status-danger-soft)] px-3 py-2 text-sm text-[var(--status-danger-text)]"
    >
      <div class="min-w-0 flex-1">
        <p class="font-semibold">{{ t('upstreams.keys.loadFailed') }}</p>
        <p class="break-words">{{ error }}</p>
      </div>
      <ConsoleButton size="sm" variant="secondary" @click="emit('retry')">
        {{ t('upstreams.retry') }}
      </ConsoleButton>
    </div>
    <div
      v-else-if="loading && !keys.length"
      role="status"
      :aria-label="t('common.loading')"
      class="space-y-2"
      data-upstream-keys-loading
    >
      <SkeletonBlock
        v-for="index in 3"
        :key="index"
        rounded="md"
        class="h-14"
      />
    </div>
    <p v-else-if="!keys.length" class="text-sm text-[var(--text-tertiary)]">
      {{ t('upstreams.keys.empty') }}
    </p>
    <div
      v-else
      class="overflow-hidden rounded-xl border border-[var(--border-subtle)]"
    >
      <div
        aria-hidden="true"
        class="hidden items-center gap-x-2 bg-[var(--surface-container-high)] px-3 py-2 text-xs text-[var(--text-tertiary)] @3xl:grid"
        :class="ROW_COLUMNS"
      >
        <span>{{ t('upstreams.keys.name') }}</span>
        <span>{{ t('upstreams.keys.group') }}</span>
        <span>{{ t('upstreams.keys.status') }}</span>
        <span>{{ t('upstreams.keys.quota') }}</span>
        <span>{{ t('upstreams.keys.expiry') }}</span>
        <span>{{ t('upstreams.keys.channel') }}</span>
        <span />
      </div>
      <ul class="divide-y divide-[var(--border-subtle)]" data-upstream-keys>
        <li
          v-for="key in keys"
          :key="key.id"
          class="grid grid-cols-[minmax(0,1fr)_auto_auto] items-start gap-x-2 px-3 py-2.5 @3xl:items-center"
          :class="ROW_COLUMNS"
          data-upstream-key-row
        >
          <div class="min-w-0 @3xl:order-1">
            <p
              class="truncate text-sm font-semibold text-[var(--text-primary)]"
              :title="key.name"
            >
              {{ key.name }}
            </p>
            <p class="truncate font-mono text-xs text-[var(--text-tertiary)]">
              {{ key.remote_key_prefix }}
            </p>
          </div>
          <div class="@3xl:order-3">
            <StatusChip :tone="keyTone(key.status)">
              {{ keyStatusText(key.status, t, te) }}
            </StatusChip>
          </div>
          <div class="@3xl:order-7 @3xl:justify-self-end">
            <ActionMenu
              :items="items(key)"
              :label="t('upstreams.keys.actions.menu', { name: key.name })"
              @select="onSelect(key, $event)"
            />
          </div>
          <dl
            class="col-span-3 mt-1.5 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs text-[var(--text-secondary)] @3xl:contents"
          >
            <div class="contents @3xl:order-2 @3xl:block @3xl:min-w-0">
              <dt class="text-[var(--text-tertiary)] @3xl:sr-only">
                {{ t('upstreams.keys.group') }}
              </dt>
              <dd class="min-w-0 truncate" :title="groupText(key)">
                {{ groupText(key) }}
              </dd>
            </div>
            <div class="contents @3xl:order-4 @3xl:block @3xl:min-w-0">
              <dt class="text-[var(--text-tertiary)] @3xl:sr-only">
                {{ t('upstreams.keys.quota') }}
              </dt>
              <dd class="min-w-0 truncate tabular-nums">
                {{ keyQuotaText(key, t) }}
              </dd>
            </div>
            <div class="contents @3xl:order-5 @3xl:block @3xl:min-w-0">
              <dt class="text-[var(--text-tertiary)] @3xl:sr-only">
                {{ t('upstreams.keys.expiry') }}
              </dt>
              <dd class="min-w-0 truncate tabular-nums">
                {{ keyExpiryText(key, t, formatTime) }}
              </dd>
            </div>
            <div class="contents @3xl:order-6 @3xl:block @3xl:min-w-0">
              <dt class="text-[var(--text-tertiary)] @3xl:sr-only">
                {{ t('upstreams.keys.channel') }}
              </dt>
              <dd class="min-w-0">
                <button
                  v-if="boundChannel.has(key.id)"
                  type="button"
                  class="font-medium text-[var(--accent-text)] hover:underline focus-ring"
                  data-upstream-key-channel
                  @click="openBinding(key)"
                >
                  {{
                    t('upstreams.keys.boundTo', {
                      id: boundChannel.get(key.id),
                    })
                  }}
                </button>
                <template v-else>{{ t('upstreams.keys.unbound') }}</template>
              </dd>
            </div>
          </dl>
        </li>
      </ul>
    </div>
    <UpstreamKeyFormModal
      v-if="dialog?.kind === 'form'"
      :account="account"
      :remote-key="dialog.key"
      @close="dialog = null"
      @saved="done"
    />
    <UpstreamCreateChannelModal
      v-if="dialog?.kind === 'channel'"
      :account="account"
      :remote-key="dialog.key"
      @close="dialog = null"
      @created="done"
      @partial="emit('reload')"
    />
    <UpstreamBindChannelModal
      v-if="dialog?.kind === 'bind'"
      :account="account"
      :remote-key="dialog.key"
      @close="dialog = null"
      @bound="done"
    />
    <ConfirmDialog
      :open="dialog?.kind === 'delete'"
      :title="t('upstreams.keys.deleteTitle')"
      :message="
        t('upstreams.keys.deleteMessage', { name: dialog?.key?.name ?? '' })
      "
      :confirm-text="t('upstreams.keys.actions.delete')"
      :loading="busyKey !== null"
      @confirm="remove"
      @cancel="dialog = null"
    />
  </section>
</template>
