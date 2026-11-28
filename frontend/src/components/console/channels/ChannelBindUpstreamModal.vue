<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { upstreamErrorText } from '@/features/upstreams/lib/upstreamText'
import type { AdminChannel } from '@/types/console'
import { isValidChannelRatio } from '@/utils/channelRatio'

const props = defineProps<{
  channel: AdminChannel | null
  accounts: UpstreamAccount[]
}>()
const emit = defineEmits<{ close: []; bound: [] }>()
const { t, te } = useI18n()
const toast = useToast()
const keysRequest = useLatestRequest()

const form = reactive({
  accountId: '',
  keyId: '',
  ratioSync: false,
  factor: '1',
})
const keys = ref<UpstreamRemoteKey[]>([])
const loadingKeys = ref(false)
const busy = ref(false)
const error = ref('')

const activeAccounts = computed(() =>
  props.accounts.filter((item) => item.status === 'active')
)
const accountOptions = computed(() =>
  activeAccounts.value.map((item) => ({
    value: String(item.id),
    label: `${item.name} (${t(`upstreams.platforms.${item.platform}`)})`,
  }))
)
const keyOptions = computed(() =>
  keys.value.map((key) => ({
    value: String(key.id),
    label: `${key.name} · ${key.remote_key_prefix}`,
  }))
)
const canSubmit = computed(
  () =>
    !busy.value &&
    !loadingKeys.value &&
    Boolean(form.accountId) &&
    Boolean(form.keyId) &&
    (!form.ratioSync || isValidChannelRatio(form.factor.trim()))
)

async function loadUnboundKeys(accountId: number) {
  loadingKeys.value = true
  error.value = ''
  keys.value = []
  form.keyId = ''
  const result = await keysRequest.run(async (signal) => {
    const [remoteKeys, bindings] = await Promise.all([
      upstreamsApi.keys(accountId, signal),
      upstreamsApi.bindings(accountId, signal),
    ])
    const boundKeyIds = new Set(bindings.map((item) => item.remote_key_id))
    return remoteKeys.filter((item) => !boundKeyIds.has(item.id))
  })
  if (result.stale) return
  loadingKeys.value = false
  if (!result.ok) {
    error.value = upstreamErrorText(result.error, t, te)
    return
  }
  keys.value = result.value
  if (result.value.length === 1) {
    form.keyId = String(result.value[0]!.id)
  }
}

watch(
  () => props.channel?.id,
  (id) => {
    keysRequest.cancel()
    error.value = ''
    keys.value = []
    form.keyId = ''
    form.ratioSync = false
    form.factor = '1'
    if (id && activeAccounts.value.length > 0) {
      form.accountId = String(activeAccounts.value[0]!.id)
    } else {
      form.accountId = ''
    }
  },
  { immediate: true }
)

watch(
  () => form.accountId,
  (raw) => {
    if (!props.channel || !raw) {
      keys.value = []
      form.keyId = ''
      return
    }
    void loadUnboundKeys(Number(raw))
  }
)

async function submit() {
  const channel = props.channel
  if (!channel || !canSubmit.value) return
  const accountId = Number(form.accountId)
  const keyId = Number(form.keyId)
  busy.value = true
  error.value = ''
  try {
    await upstreamsApi.bindChannel(accountId, keyId, channel.id)
  } catch (cause) {
    error.value = upstreamErrorText(cause, t, te)
    busy.value = false
    return
  }
  if (form.ratioSync) {
    try {
      await upstreamsApi.updateBinding(accountId, channel.id, {
        ratio_sync_enabled: true,
        ratio_sync_factor: form.factor.trim(),
      })
    } catch (cause) {
      toast.error(
        t('upstreams.channelForm.followUpFailed', {
          id: channel.id,
          text: upstreamErrorText(cause, t, te),
        })
      )
    }
  }
  busy.value = false
  toast.success(t('upstreams.bindForm.bound', { id: channel.id }))
  emit('bound')
}
</script>

<template>
  <ConsoleModal
    :open="channel !== null"
    :title="t('upstreams.channelLink.bindTitle', { name: channel?.name ?? '' })"
    size="sm"
    :close-disabled="busy"
    @close="emit('close')"
  >
    <p
      v-if="!activeAccounts.length"
      class="text-sm text-[var(--text-tertiary)]"
    >
      {{ t('upstreams.channelLink.noAccounts') }}
    </p>
    <form v-else class="space-y-4" novalidate @submit.prevent="submit">
      <FormField :label="t('upstreams.channelLink.selectAccount')">
        <FilterSelect
          v-model="form.accountId"
          :options="accountOptions"
          :label="t('upstreams.channelLink.selectAccount')"
          :placeholder="t('upstreams.channelLink.selectAccountPlaceholder')"
          :disabled="busy"
        />
      </FormField>
      <FormField
        :label="t('upstreams.channelLink.selectKey')"
        :hint="t('upstreams.bindForm.hint')"
      >
        <p
          v-if="loadingKeys"
          role="status"
          class="text-xs text-[var(--text-tertiary)]"
        >
          {{ t('common.loading') }}
        </p>
        <p
          v-else-if="form.accountId && !keys.length && !error"
          class="text-xs text-[var(--text-tertiary)]"
        >
          {{ t('upstreams.channelLink.noKeys') }}
        </p>
        <FilterSelect
          v-else
          v-model="form.keyId"
          :options="keyOptions"
          :label="t('upstreams.channelLink.selectKey')"
          :placeholder="t('upstreams.channelLink.selectKeyPlaceholder')"
          :disabled="busy || !keys.length"
        />
      </FormField>
      <ConsoleToggle
        v-model="form.ratioSync"
        :label="t('upstreams.channelForm.ratioSync')"
        name="channel-bind-upstream-ratio-sync"
        :disabled="busy"
      />
      <FormField
        v-if="form.ratioSync"
        :label="t('upstreams.channelForm.factor')"
      >
        <TextInput
          v-model="form.factor"
          name="channel-bind-upstream-factor"
          inputmode="decimal"
          :readonly="busy"
        />
        <p
          v-if="form.factor.trim() && !isValidChannelRatio(form.factor.trim())"
          class="mt-1 text-xs text-[var(--status-danger-text)]"
        >
          {{ t('upstreams.channelForm.factorInvalid') }}
        </p>
      </FormField>
      <p
        v-if="error"
        role="alert"
        class="break-words text-sm text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="busy"
          @click="emit('close')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="activeAccounts.length"
          :disabled="!canSubmit"
          :loading="busy"
          @click="submit"
        >
          {{ t('upstreams.bindForm.submit') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
