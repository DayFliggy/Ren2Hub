<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { upstreamErrorText } from '../lib/upstreamText'

const props = defineProps<{
  account: UpstreamAccount
  remoteKey: UpstreamRemoteKey
}>()
const emit = defineEmits<{ close: []; bound: [channelId: number] }>()
const { t, te } = useI18n()
const toast = useToast()

const form = reactive({ channelId: '', ratioSync: false, factor: '1' })
const busy = ref(false)
const error = ref('')
const channelId = computed(() =>
  /^[1-9]\d{0,9}$/.test(form.channelId.trim()) ? Number(form.channelId) : null
)

async function submit() {
  const id = channelId.value
  if (id === null) {
    error.value = t('upstreams.bindForm.channelIdInvalid')
    return
  }
  if (form.ratioSync && !isValidChannelRatio(form.factor.trim())) {
    error.value = t('upstreams.channelForm.factorInvalid')
    return
  }
  busy.value = true
  error.value = ''
  try {
    await upstreamsApi.bindChannel(props.account.id, props.remoteKey.id, id)
  } catch (cause) {
    error.value = upstreamErrorText(cause, t, te)
    busy.value = false
    return
  }
  if (form.ratioSync) {
    try {
      await upstreamsApi.updateBinding(props.account.id, id, {
        ratio_sync_enabled: true,
        ratio_sync_factor: form.factor.trim(),
      })
    } catch (cause) {
      toast.error(
        t('upstreams.channelForm.followUpFailed', {
          id,
          text: upstreamErrorText(cause, t, te),
        })
      )
    }
  }
  busy.value = false
  toast.success(t('upstreams.bindForm.bound', { id }))
  emit('bound', id)
}
</script>

<template>
  <ConsoleModal
    open
    :title="t('upstreams.bindForm.title', { name: remoteKey.name })"
    size="sm"
    :close-disabled="busy"
    @close="emit('close')"
  >
    <form class="space-y-4" novalidate @submit.prevent="submit">
      <FormField
        :label="t('upstreams.bindForm.channelId')"
        :hint="t('upstreams.bindForm.hint')"
      >
        <TextInput
          v-model="form.channelId"
          name="upstream-bind-channel-id"
          inputmode="numeric"
        />
      </FormField>
      <ConsoleToggle
        v-model="form.ratioSync"
        :label="t('upstreams.channelForm.ratioSync')"
        name="upstream-bind-ratio-sync"
      />
      <FormField
        v-if="form.ratioSync"
        :label="t('upstreams.channelForm.factor')"
      >
        <TextInput
          v-model="form.factor"
          name="upstream-bind-factor"
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
      <ConsoleButton :loading="busy" data-upstream-bind-submit @click="submit">
        {{ t('upstreams.bindForm.submit') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
</template>
