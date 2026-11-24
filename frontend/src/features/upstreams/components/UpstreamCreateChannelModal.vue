<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamChannelInput,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { isValidChannelRatio } from '@/utils/channelRatio'
import {
  buildKeyInput,
  defaultKeyForm,
  groupOptions,
  type KeyForm,
} from '../lib/upstreamKeyForm'
import { isHttpUrl, upstreamErrorText } from '../lib/upstreamText'
import UpstreamKeyFields from './UpstreamKeyFields.vue'

// Sub2API group platforms the server maps to the account URL by itself.
const DEFAULT_URL_PLATFORMS = ['openai', 'anthropic', 'gemini', 'composite']

const props = defineProps<{
  account: UpstreamAccount
  /** null creates a new remote key together with the channel. */
  remoteKey: UpstreamRemoteKey | null
}>()
const emit = defineEmits<{
  close: []
  created: [channelId: number]
  /** The combined call failed; the remote key may exist and lists reload. */
  partial: []
}>()
const { t, te } = useI18n()
const toast = useToast()

const keyForm = ref<KeyForm>(defaultKeyForm(props.account.platform, null))
const form = reactive({
  name: props.remoteKey?.name ?? '',
  models: '',
  baseUrl: '',
  ratioSync: false,
  factor: '1',
})
const busy = ref(false)
const error = ref('')

const title = computed(() =>
  props.remoteKey
    ? t('upstreams.channelForm.fromKeyTitle', { name: props.remoteKey.name })
    : t('upstreams.channelForm.newKeyTitle')
)
const groups = computed(() => groupOptions(props.account, false, t))
const groupPlatform = computed(() => {
  if (props.account.platform !== 'sub2api') return null
  const id = props.remoteKey
    ? props.remoteKey.upstream_group_id
    : Number(keyForm.value.group)
  // An unknown group (the account default) is left to the server check.
  const group = props.account.upstream_groups.find((item) => item.id === id)
  return group ? group.platform : null
})
const baseUrlRequired = computed(
  () =>
    groupPlatform.value !== null &&
    groupPlatform.value !== 'antigravity' &&
    !DEFAULT_URL_PLATFORMS.includes(groupPlatform.value)
)
const baseUrlHint = computed(() => {
  if (groupPlatform.value === 'antigravity')
    return t('upstreams.channelForm.antigravityHint')
  if (baseUrlRequired.value)
    return t('upstreams.channelForm.unknownPlatformHint', {
      platform: groupPlatform.value || '?',
    })
  return t('upstreams.channelForm.baseUrlHint')
})

function channelInput(): UpstreamChannelInput | string {
  const name = form.name.trim()
  const models = form.models
    .split(',')
    .map((model) => model.trim())
    .filter(Boolean)
    .join(',')
  if (!name) return t('upstreams.keyForm.nameRequired')
  if (!models) return t('upstreams.channelForm.modelsRequired')
  const baseUrl = form.baseUrl.trim()
  if (!baseUrl && baseUrlRequired.value)
    return t('upstreams.channelForm.baseUrlRequired')
  if (baseUrl && !isHttpUrl(baseUrl)) return t('upstreams.account.invalidUrl')
  if (form.ratioSync && !isValidChannelRatio(form.factor.trim()))
    return t('upstreams.channelForm.factorInvalid')
  return baseUrl ? { name, models, base_url: baseUrl } : { name, models }
}

async function create(input: UpstreamChannelInput): Promise<number | null> {
  if (props.remoteKey)
    return upstreamsApi.createChannelFromKey(
      props.account.id,
      props.remoteKey.id,
      input
    )
  const built = buildKeyInput(props.account.platform, keyForm.value, null)
  if ('error' in built) {
    error.value = t(built.error, built.values ?? {})
    return null
  }
  try {
    return await upstreamsApi.createKeyAndChannel(props.account.id, {
      ...input,
      new_key: built.input,
    })
  } catch (cause) {
    error.value = t('upstreams.channelForm.partialFailure', {
      text: upstreamErrorText(cause, t, te),
    })
    emit('partial')
    return null
  }
}

async function enableRatioSync(channelId: number) {
  try {
    await upstreamsApi.updateBinding(props.account.id, channelId, {
      ratio_sync_enabled: true,
      ratio_sync_factor: form.factor.trim(),
    })
  } catch (cause) {
    toast.error(
      t('upstreams.channelForm.followUpFailed', {
        id: channelId,
        text: upstreamErrorText(cause, t, te),
      })
    )
  }
}

async function submit() {
  const input = channelInput()
  if (typeof input === 'string') {
    error.value = input
    return
  }
  busy.value = true
  error.value = ''
  try {
    const channelId = await create(input)
    if (channelId === null) return
    if (form.ratioSync) await enableRatioSync(channelId)
    toast.success(t('upstreams.channelForm.created', { id: channelId }))
    emit('created', channelId)
  } catch (cause) {
    error.value = upstreamErrorText(cause, t, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ConsoleModal
    open
    :title="title"
    size="md"
    :close-disabled="busy"
    @close="emit('close')"
  >
    <form class="space-y-4" novalidate @submit.prevent="submit">
      <template v-if="!remoteKey">
        <FormField :label="t('upstreams.channelForm.keyName')">
          <TextInput v-model="keyForm.name" name="upstream-new-key-name" />
        </FormField>
        <FormField :label="t('upstreams.channelForm.group')" group>
          <FilterSelect
            v-model="keyForm.group"
            :options="groups"
            :label="t('upstreams.channelForm.group')"
            class="w-full"
          />
        </FormField>
        <UpstreamKeyFields
          v-model:form="keyForm"
          :platform="account.platform"
          :editing="false"
        />
      </template>
      <FormField :label="t('upstreams.channelForm.name')">
        <TextInput v-model="form.name" name="upstream-channel-name" />
      </FormField>
      <FormField
        :label="t('upstreams.channelForm.models')"
        :hint="t('upstreams.channelForm.modelsHint')"
      >
        <TextInput
          v-model="form.models"
          name="upstream-channel-models"
          :placeholder="t('upstreams.channelForm.modelsPlaceholder')"
        />
      </FormField>
      <FormField
        :label="t('upstreams.channelForm.baseUrl')"
        :hint="baseUrlHint"
      >
        <TextInput
          v-model="form.baseUrl"
          name="upstream-channel-base-url"
          type="url"
          :placeholder="account.base_url"
        />
      </FormField>
      <ConsoleToggle
        v-model="form.ratioSync"
        :label="t('upstreams.channelForm.ratioSync')"
        name="upstream-channel-ratio-sync"
      />
      <p class="text-xs text-[var(--text-tertiary)]">
        {{ t('upstreams.channelForm.ratioSyncHint') }}
      </p>
      <FormField
        v-if="form.ratioSync"
        :label="t('upstreams.channelForm.factor')"
      >
        <TextInput
          v-model="form.factor"
          name="upstream-channel-factor"
          inputmode="decimal"
        />
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
      <ConsoleButton
        variant="secondary"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ t('common.cancel') }}
      </ConsoleButton>
      <ConsoleButton
        :loading="busy"
        data-upstream-channel-submit
        @click="submit"
      >
        {{ t('upstreams.channelForm.submit') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
</template>
