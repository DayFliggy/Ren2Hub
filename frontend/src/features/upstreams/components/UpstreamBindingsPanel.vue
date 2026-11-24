<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamBinding,
  type UpstreamBindingUpdate,
} from '@/api/upstreams'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { keyStatusText, keyTone, upstreamErrorText } from '../lib/upstreamText'

const props = defineProps<{
  account: UpstreamAccount
  bindings: UpstreamBinding[]
  loading: boolean
  error: string
}>()
const emit = defineEmits<{ reload: [] }>()
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
  <section class="space-y-3">
    <div
      v-if="error"
      role="alert"
      class="rounded-lg bg-[var(--status-danger-soft)] px-3 py-2 text-sm text-[var(--status-danger-text)]"
    >
      <p class="font-semibold">{{ t('upstreams.bindings.loadFailed') }}</p>
      <p class="break-words">{{ error }}</p>
    </div>
    <p
      v-else-if="loading && !bindings.length"
      role="status"
      class="text-sm text-[var(--text-tertiary)]"
    >
      {{ t('common.loading') }}
    </p>
    <p v-else-if="!bindings.length" class="text-sm text-[var(--text-tertiary)]">
      {{ t('upstreams.bindings.empty') }}
    </p>
    <ul v-else class="space-y-3" data-upstream-bindings>
      <li
        v-for="binding in bindings"
        :key="binding.id"
        class="rounded-xl border border-[var(--border-subtle)] p-3"
      >
        <div class="flex flex-wrap items-center gap-2">
          <RouterLink
            :to="{
              name: 'channels',
              query: { upstream_account: String(account.id) },
            }"
            class="text-sm font-semibold text-[var(--accent)] hover:underline"
            :title="t('upstreams.bindings.openChannels')"
          >
            {{ t('upstreams.bindings.channel', { id: binding.channel_id }) }}
          </RouterLink>
          <span class="text-xs text-[var(--text-tertiary)]">
            {{
              t('upstreams.bindings.remoteKey', { id: binding.remote_key_id })
            }}
          </span>
          <StatusChip :tone="keyTone(binding.remote_status)" class="ml-auto">
            {{ keyStatusText(binding.remote_status, t, te) }}
          </StatusChip>
        </div>
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
        <div
          v-if="drafts[binding.channel_id]"
          class="mt-3 flex flex-wrap items-end gap-3"
        >
          <ConsoleToggle
            v-model="drafts[binding.channel_id].enabled"
            :label="t('upstreams.bindings.ratioSync')"
            :disabled="!canEdit || busyChannel === binding.channel_id"
            :name="`upstream-ratio-sync-${binding.channel_id}`"
          />
          <label class="w-32">
            <span class="mb-1 block text-xs text-[var(--text-tertiary)]">
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
            class="ml-auto"
            :disabled="!canEdit || busyChannel === binding.channel_id"
            @click="unbinding = binding"
          >
            {{ t('upstreams.bindings.unbind') }}
          </ConsoleButton>
        </div>
        <p
          v-if="drafts[binding.channel_id] && !factorValid(binding)"
          class="mt-1 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('upstreams.channelForm.factorInvalid') }}
        </p>
      </li>
    </ul>
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
