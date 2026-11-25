<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamBinding,
  type UpstreamBindingUpdate,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { keyStatusText, keyTone, upstreamErrorText } from '../lib/upstreamText'

const props = defineProps<{
  account: UpstreamAccount
  bindings: UpstreamBinding[]
  /** Remote keys already loaded by the workspace, for display names only. */
  keys: UpstreamRemoteKey[]
  loading: boolean
  error: string
  /** Channel ID to scroll to and mark, chosen from the keys tab. */
  highlight: number | null
}>()
const emit = defineEmits<{ reload: []; retry: [] }>()

// One column template for the header and every row in the wide layout.
const ROW_COLUMNS =
  '@3xl:grid-cols-[minmax(6rem,0.8fr)_minmax(9rem,1.4fr)_6.5rem_auto_7rem_auto_auto]'
const root = ref<HTMLElement | null>(null)
const { t, te } = useI18n()
const auth = useAuthStore()
const toast = useToast()

const canEdit = computed(
  () =>
    auth.hasPermission('upstream', 'write') &&
    auth.hasPermission('channel', 'sensitive_write')
)
const drafts = reactive<Record<number, { enabled: boolean; factor: string }>>(
  {}
)
const busyChannel = ref<number | null>(null)
const unbinding = ref<UpstreamBinding | null>(null)
const keyById = computed(() => new Map(props.keys.map((key) => [key.id, key])))

function remoteKeyText(binding: UpstreamBinding) {
  const key = keyById.value.get(binding.remote_key_id)
  if (!key)
    return t('upstreams.bindings.remoteKey', { id: binding.remote_key_id })
  return t('upstreams.bindings.remoteKeyName', {
    name: key.name,
    prefix: key.remote_key_prefix,
  })
}

watch(
  () => [props.highlight, props.bindings] as const,
  async ([channelId]) => {
    if (!channelId) return
    await nextTick()
    root.value
      ?.querySelector(`[data-upstream-binding="${channelId}"]`)
      ?.scrollIntoView({ block: 'nearest' })
  },
  { immediate: true }
)

watch(
  () => props.bindings,
  (bindings) => {
    for (const key of Object.keys(drafts)) delete drafts[Number(key)]
    for (const binding of bindings)
      drafts[binding.channel_id] = {
        enabled: binding.ratio_sync_enabled,
        factor: binding.ratio_sync_factor,
      }
  },
  { immediate: true }
)

function draftUpdate(binding: UpstreamBinding): UpstreamBindingUpdate {
  const draft = drafts[binding.channel_id]
  const update: UpstreamBindingUpdate = {}
  if (!draft) return update
  if (draft.enabled !== binding.ratio_sync_enabled)
    update.ratio_sync_enabled = draft.enabled
  if (draft.factor.trim() !== binding.ratio_sync_factor)
    update.ratio_sync_factor = draft.factor.trim()
  return update
}

function factorValid(binding: UpstreamBinding) {
  const draft = drafts[binding.channel_id]
  return !!draft && isValidChannelRatio(draft.factor.trim())
}

function ratioErrorText(code: string) {
  for (const key of [
    `upstreams.bindings.ratioErrors.${code}`,
    `upstreams.errors.${code}`,
  ])
    if (te(key)) return t(key)
  return code
}

async function save(binding: UpstreamBinding) {
  const update = draftUpdate(binding)
  if (!Object.keys(update).length || !factorValid(binding)) return
  busyChannel.value = binding.channel_id
  try {
    await upstreamsApi.updateBinding(
      props.account.id,
      binding.channel_id,
      update
    )
    toast.success(t('upstreams.bindings.saved'))
    emit('reload')
  } catch (error) {
    toast.error(upstreamErrorText(error, t, te))
  } finally {
    busyChannel.value = null
  }
}

async function unbind() {
  const binding = unbinding.value
  if (!binding) return
  busyChannel.value = binding.channel_id
  try {
    await upstreamsApi.unbind(props.account.id, binding.channel_id)
    toast.success(t('upstreams.bindings.unbound'))
    unbinding.value = null
    emit('reload')
  } catch (error) {
    toast.error(upstreamErrorText(error, t, te))
  } finally {
    busyChannel.value = null
  }
}
</script>

<template>
  <section ref="root" class="@container space-y-3">
    <p
      v-if="!loading && !error"
      class="text-xs tabular-nums text-[var(--text-tertiary)]"
    >
      {{ t('upstreams.bindings.count', { count: bindings.length }) }}
    </p>
    <div
      v-if="error"
      role="alert"
      class="flex flex-wrap items-start gap-3 rounded-lg bg-[var(--status-danger-soft)] px-3 py-2 text-sm text-[var(--status-danger-text)]"
    >
      <div class="min-w-0 flex-1">
        <p class="font-semibold">{{ t('upstreams.bindings.loadFailed') }}</p>
        <p class="break-words">{{ error }}</p>
      </div>
      <ConsoleButton size="sm" variant="secondary" @click="emit('retry')">
        {{ t('upstreams.retry') }}
      </ConsoleButton>
    </div>
    <div
      v-else-if="loading && !bindings.length"
      role="status"
      :aria-label="t('common.loading')"
      class="space-y-2"
      data-upstream-bindings-loading
    >
      <SkeletonBlock
        v-for="index in 3"
        :key="index"
        rounded="md"
        class="h-14"
      />
    </div>
    <p v-else-if="!bindings.length" class="text-sm text-[var(--text-tertiary)]">
      {{ t('upstreams.bindings.empty') }}
    </p>
    <div
      v-else
      class="@3xl:overflow-hidden @3xl:rounded-xl @3xl:border @3xl:border-[var(--border-subtle)]"
    >
      <div
        aria-hidden="true"
        class="hidden items-center gap-x-3 bg-[var(--surface-container-high)] px-3 py-2 text-xs text-[var(--text-tertiary)] @3xl:grid"
        :class="ROW_COLUMNS"
      >
        <span>{{ t('upstreams.keys.channel') }}</span>
        <span>{{ t('upstreams.keys.prefix') }}</span>
        <span>{{ t('upstreams.bindings.remoteStatus') }}</span>
        <span>{{ t('upstreams.bindings.ratioSync') }}</span>
        <span>{{ t('upstreams.bindings.factor') }}</span>
        <span />
        <span />
      </div>
      <ul
        class="space-y-3 @3xl:space-y-0 @3xl:divide-y @3xl:divide-[var(--border-subtle)]"
        data-upstream-bindings
      >
        <li
          v-for="binding in bindings"
          :key="binding.id"
          class="rounded-xl border p-3 transition-colors @3xl:rounded-none @3xl:border-0 @3xl:px-3 @3xl:py-2.5"
          :class="
            highlight === binding.channel_id
              ? 'border-[var(--accent)] bg-[var(--state-hover-layer)]'
              : 'border-[var(--border-subtle)]'
          "
          :data-upstream-binding="binding.channel_id"
          :data-highlighted="highlight === binding.channel_id || undefined"
        >
          <div
            class="@3xl:grid @3xl:items-center @3xl:gap-x-3"
            :class="ROW_COLUMNS"
          >
            <div class="flex flex-wrap items-center gap-2 @3xl:contents">
              <RouterLink
                :to="{
                  name: 'channels',
                  query: { upstream_account: String(account.id) },
                }"
                class="text-sm font-semibold text-[var(--accent)] hover:underline"
                :title="t('upstreams.bindings.openChannels')"
              >
                {{
                  t('upstreams.bindings.channel', { id: binding.channel_id })
                }}
              </RouterLink>
              <span
                class="min-w-0 truncate text-xs text-[var(--text-tertiary)]"
                :title="remoteKeyText(binding)"
              >
                {{ remoteKeyText(binding) }}
              </span>
              <span class="ml-auto @3xl:ml-0">
                <StatusChip :tone="keyTone(binding.remote_status)">
                  {{ keyStatusText(binding.remote_status, t, te) }}
                </StatusChip>
              </span>
            </div>
            <div
              v-if="drafts[binding.channel_id]"
              class="mt-3 flex flex-wrap items-end gap-3 @3xl:contents"
            >
              <ConsoleToggle
                v-model="drafts[binding.channel_id].enabled"
                :label="t('upstreams.bindings.ratioSync')"
                :disabled="!canEdit || busyChannel === binding.channel_id"
                :name="`upstream-ratio-sync-${binding.channel_id}`"
              />
              <label class="w-32 @3xl:w-auto">
                <span
                  class="mb-1 block text-xs text-[var(--text-tertiary)] @3xl:sr-only"
                >
                  {{ t('upstreams.bindings.factor') }}
                </span>
                <TextInput
                  v-model="drafts[binding.channel_id].factor"
                  :name="`upstream-ratio-factor-${binding.channel_id}`"
                  inputmode="decimal"
                  :readonly="!canEdit"
                />
              </label>
              <ConsoleButton
                size="sm"
                variant="secondary"
                :disabled="
                  !canEdit ||
                  !factorValid(binding) ||
                  !Object.keys(draftUpdate(binding)).length
                "
                :loading="busyChannel === binding.channel_id"
                @click="save(binding)"
              >
                {{ t('upstreams.bindings.save') }}
              </ConsoleButton>
              <ConsoleButton
                size="sm"
                variant="ghost"
                class="ml-auto @3xl:ml-0"
                :disabled="!canEdit || busyChannel === binding.channel_id"
                @click="unbinding = binding"
              >
                {{ t('upstreams.bindings.unbind') }}
              </ConsoleButton>
            </div>
          </div>
          <p
            v-if="drafts[binding.channel_id] && !factorValid(binding)"
            class="mt-1 text-xs text-[var(--status-danger-text)]"
          >
            {{ t('upstreams.channelForm.factorInvalid') }}
          </p>
          <p
            v-if="binding.stale"
            role="alert"
            class="mt-2 rounded-lg bg-[var(--status-warning-soft)] px-3 py-2 text-xs text-[var(--status-warning-text)]"
            data-upstream-binding-stale
          >
            {{ t('upstreams.bindings.stale') }}
          </p>
          <p
            v-if="binding.ratio_sync_error"
            role="alert"
            class="mt-2 rounded-lg bg-[var(--status-danger-soft)] px-3 py-2 text-xs text-[var(--status-danger-text)]"
          >
            {{
              t('upstreams.bindings.ratioError', {
                text: ratioErrorText(binding.ratio_sync_error),
              })
            }}
          </p>
        </li>
      </ul>
    </div>
    <ConfirmDialog
      :open="unbinding !== null"
      :title="t('upstreams.bindings.unbindTitle')"
      :message="
        t('upstreams.bindings.unbindMessage', {
          id: unbinding?.channel_id ?? '',
        })
      "
      :confirm-text="t('upstreams.bindings.unbind')"
      :loading="busyChannel !== null"
      @confirm="unbind"
      @cancel="unbinding = null"
    />
  </section>
</template>
