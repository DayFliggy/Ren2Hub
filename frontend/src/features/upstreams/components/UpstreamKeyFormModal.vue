<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamKeyInput,
  type UpstreamRemoteKey,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { upstreamErrorText } from '../lib/upstreamText'
import {
  buildKeyInput,
  defaultKeyForm,
  groupOptions,
  type KeyForm,
} from '../lib/upstreamKeyForm'
import UpstreamKeyFields from './UpstreamKeyFields.vue'

const props = defineProps<{
  account: UpstreamAccount
  /** null creates a new remote key. */
  remoteKey: UpstreamRemoteKey | null
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, te } = useI18n()
const toast = useToast()

const editing = computed(() => props.remoteKey !== null)
const form = ref<KeyForm>(
  defaultKeyForm(props.account.platform, props.remoteKey)
)
const busy = ref(false)
const error = ref('')

const title = computed(() =>
  props.remoteKey
    ? t('upstreams.keyForm.editTitle', { name: props.remoteKey.name })
    : t('upstreams.keyForm.createTitle')
)
const groups = computed(() => groupOptions(props.account, editing.value, t))

async function submit() {
  const built = buildKeyInput(
    props.account.platform,
    form.value,
    props.remoteKey
  )
  if ('error' in built) {
    error.value = t(built.error, built.values ?? {})
    return
  }
  const input: UpstreamKeyInput = built.input
  if (editing.value && !Object.keys(input).length) {
    error.value = t('upstreams.account.noChange')
    return
  }
  busy.value = true
  error.value = ''
  try {
    if (props.remoteKey)
      await upstreamsApi.updateKey(props.account.id, props.remoteKey.id, input)
    else await upstreamsApi.createKey(props.account.id, input)
    toast.success(
      t(editing.value ? 'upstreams.keyForm.saved' : 'upstreams.keyForm.created')
    )
    emit('saved')
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
      <FormField :label="t('upstreams.keyForm.name')">
        <TextInput v-model="form.name" name="upstream-key-name" />
      </FormField>
      <FormField :label="t('upstreams.keyForm.group')" group>
        <FilterSelect
          v-model="form.group"
          :options="groups"
          :label="t('upstreams.keyForm.group')"
          class="w-full"
        />
      </FormField>
      <UpstreamKeyFields
        v-model:form="form"
        :platform="account.platform"
        :editing="editing"
      />
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
      <ConsoleButton :loading="busy" data-upstream-key-submit @click="submit">
        {{ t('common.save') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
</template>
