<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { publicApi, type PublicStatus } from '@/api/public'
import { completeOAuth } from '@/api/oauth'
import {
  securityApi,
  type SecurityOperation,
  type SecurityProof,
  type VerificationRequirements,
} from '@/api/security'
import { assertPasskey, type AssertionOptions } from '@/utils/passkey'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TextInput from '@/components/common/TextInput.vue'
import FormField from '@/components/common/FormField.vue'

const { t } = useI18n()
let oauthAbort: AbortController | null = null
const open = ref(false)
const busy = ref(false)
const error = ref('')
const value = ref('')
const method = ref('')
const provider = ref('')
const rpID = ref('')
const rpIDs = ref<string[]>([])
const requirements = ref<VerificationRequirements | null>(null)
let publicStatus: PublicStatus = {}
let operation: SecurityOperation = { scope: '' }
let requestVersion = 0
let pending: {
  resolve: (proof: string) => void
  reject: (reason: Error) => void
} | null = null

function cancel() {
  requestVersion++
  oauthAbort?.abort()
  pending?.reject(new DOMException(t('security.cancelled'), 'AbortError'))
  pending = null
  open.value = false
  busy.value = false
  value.value = ''
}

async function request(next: SecurityOperation): Promise<string> {
  cancel()
  const version = requestVersion
  operation = next
  const [policy, status] = await Promise.all([
    securityApi.requirements(next.scope),
    publicApi.status(),
  ])
  if (version !== requestVersion)
    throw new DOMException(t('security.cancelled'), 'AbortError')
  publicStatus = status
  requirements.value = policy
  rpIDs.value = status.passkey_rp_ids ?? []
  rpID.value = ''
  method.value = policy.methods.find((item) => item.available)?.method ?? ''
  provider.value = policy.oauth_providers[0]?.slug ?? ''
  error.value = ''
  value.value = ''
  open.value = true
  return new Promise<string>((resolve, reject) => {
    pending = { resolve, reject }
  })
}

async function verify() {
  if (busy.value || !method.value) return
  const version = requestVersion
  busy.value = true
  error.value = ''
  try {
    let result: SecurityProof
    if (method.value === 'passkey') {
      const begin = await api.post<{
        options: AssertionOptions
        flow_token: string
      }>('/api/user/passkey/verify/begin', { ...operation, rp_id: rpID.value })
      if (version !== requestVersion) return
      const credential = await assertPasskey(begin.options)
      if (version !== requestVersion) return
      result = await api.post<SecurityProof>(
        '/api/user/passkey/verify/finish',
        {
          flow_token: begin.flow_token,
          credential,
        }
      )
    } else if (method.value === 'oauth') {
      oauthAbort = new AbortController()
      result = await completeOAuth<SecurityProof>(
        publicStatus,
        provider.value,
        'verify',
        operation,
        undefined,
        oauthAbort.signal
      )
    } else {
      result = await securityApi.verify(operation, method.value, value.value)
    }
    if (version !== requestVersion) return
    if (
      !result.proof_token ||
      result.scope !== operation.scope ||
      result.expires_at <= Date.now() / 1000
    )
      throw new Error(t('security.invalidProof'))
    pending?.resolve(result.proof_token)
    pending = null
    open.value = false
    value.value = ''
  } catch (cause) {
    if (version === requestVersion)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (version === requestVersion) busy.value = false
  }
}
onBeforeUnmount(cancel)
defineExpose({ request })
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="t('security.verifyTitle')"
    size="sm"
    :z-index="110"
    :close-disabled="busy"
    @close="cancel"
  >
    <form class="space-y-4" @submit.prevent="verify">
      <p class="text-sm text-[var(--text-secondary)]">
        {{ t('security.verifyHint') }}
      </p>
      <FormField :label="t('security.method')">
        <select
          v-model="method"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 text-[var(--text-primary)]"
          :disabled="busy"
        >
          <option
            v-for="item in requirements?.methods"
            :key="item.method"
            :value="item.method"
            :disabled="!item.available"
          >
            {{ t(`security.methods.${item.method}`)
            }}{{ item.reason ? ` — ${item.reason}` : '' }}
          </option>
        </select>
      </FormField>
      <FormField
        v-if="method === 'password' || method === '2fa'"
        :label="
          t(method === 'password' ? 'security.password' : 'security.code')
        "
      >
        <TextInput
          v-model="value"
          :type="method === 'password' ? 'password' : 'text'"
          :autocomplete="
            method === 'password' ? 'current-password' : 'one-time-code'
          "
          :maxlength="method === 'password' ? 128 : 32"
        />
      </FormField>
      <FormField
        v-if="method === 'passkey' && rpIDs.length > 1"
        :label="t('security.passkeyDomain')"
      >
        <select
          v-model="rpID"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
        >
          <option value="">{{ t('security.automaticDomain') }}</option>
          <option v-for="id in rpIDs" :key="id" :value="id">{{ id }}</option>
        </select>
      </FormField>
      <FormField v-if="method === 'oauth'" :label="t('security.provider')">
        <select
          v-model="provider"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
        >
          <option
            v-for="item in requirements?.oauth_providers"
            :key="item.slug"
            :value="item.slug"
          >
            {{ item.name }}
          </option>
        </select>
      </FormField>
      <p
        v-if="!method"
        role="alert"
        class="text-sm text-[var(--text-secondary)]"
      >
        {{ t('security.unavailable') }}
      </p>
      <p
        v-if="error"
        role="alert"
        class="text-sm text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
      <ConsoleButton
        type="submit"
        block
        :loading="busy"
        :disabled="
          !method || ((method === 'password' || method === '2fa') && !value)
        "
        >{{ t('common.confirm') }}</ConsoleButton
      >
    </form>
  </ConsoleModal>
</template>
