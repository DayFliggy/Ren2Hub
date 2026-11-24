<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  UPSTREAM_PLATFORMS,
  upstreamsApi,
  type UpstreamAccount,
  type UpstreamAuthInput,
  type UpstreamAuthResult,
  type UpstreamCapabilities,
  type UpstreamEndpointUpdate,
  type UpstreamPlatform,
} from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { isHttpUrl, upstreamErrorText } from '../lib/upstreamText'

export type UpstreamAccountModalMode =
  'create' | 'reauth' | 'settings' | 'endpoint'
type AuthMethod = 'password' | 'pat' | 'refresh_token'
type ProxyMode = 'keep' | 'set' | 'clear'

const props = defineProps<{
  mode: UpstreamAccountModalMode
  account: UpstreamAccount | null
}>()
const emit = defineEmits<{ close: []; done: [] }>()
const { t, te } = useI18n()
const toast = useToast()

const busy = ref(false)
const error = ref('')
const probeResult = ref<UpstreamCapabilities | null>(null)
const challenge = ref('')
const code = ref('')
const form = reactive({
  platform: (props.account?.platform ?? 'newapi') as UpstreamPlatform,
  method: 'password' as AuthMethod,
  name: props.account?.name ?? '',
  baseUrl: props.account?.base_url ?? '',
  proxyUrl: '',
  proxyMode: 'keep' as ProxyMode,
  enabled: props.account ? props.account.status !== 'disabled' : true,
})
// Secrets live only in this component and are cleared on every exit path.
const secret = reactive({ username: '', password: '', token: '' })

const platform = computed(() => props.account?.platform ?? form.platform)
const isAuth = computed(
  () => props.mode === 'create' || props.mode === 'reauth'
)
const title = computed(() => {
  const name = props.account?.name ?? ''
  if (props.mode === 'create') return t('upstreams.account.createTitle')
  return t(`upstreams.account.${props.mode}Title`, { name })
})
const platformOptions = computed(() =>
  UPSTREAM_PLATFORMS.map((value) => ({
    value,
    label: t(`upstreams.platforms.${value}`),
  }))
)
const tokenMethod = computed<AuthMethod>(() =>
  platform.value === 'newapi' ? 'pat' : 'refresh_token'
)
const methodOptions = computed(() =>
  (['password', tokenMethod.value] as AuthMethod[]).map((value) => ({
    value,
    label: t(`upstreams.account.methods.${value}`),
  }))
)
const proxyOptions = computed(() =>
  (['keep', 'set', 'clear'] as ProxyMode[]).map((value) => ({
    value,
    label: t(`upstreams.account.proxyModes.${value}`),
  }))
)
const codeValid = computed(() => /^\d{6}$/.test(code.value))
const endpointProxyChanges = computed(() => form.proxyMode !== 'keep')

function clearSecrets() {
  secret.username = ''
  secret.password = ''
  secret.token = ''
  code.value = ''
  challenge.value = ''
}

function close() {
  clearSecrets()
  emit('close')
}

onBeforeUnmount(clearSecrets)

function setPlatform(value: string) {
  form.platform = value as UpstreamPlatform
  if (form.method !== 'password') form.method = tokenMethod.value
  probeResult.value = null
}

function fail(cause: unknown) {
  error.value = upstreamErrorText(cause, t, te)
}

async function guarded(task: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await task()
  } catch (cause) {
    fail(cause)
  } finally {
    busy.value = false
  }
}

function connection() {
  const input: {
    platform: UpstreamPlatform
    base_url: string
    proxy_url?: string
  } = { platform: form.platform, base_url: form.baseUrl.trim() }
  if (form.proxyUrl.trim()) input.proxy_url = form.proxyUrl.trim()
  return input
}

function connectionError(): string {
  if (!isHttpUrl(form.baseUrl.trim())) return t('upstreams.account.invalidUrl')
  if (form.proxyUrl.trim() && !isHttpUrl(form.proxyUrl.trim()))
    return t('upstreams.account.invalidUrl')
  return ''
}

async function probe() {
  error.value = connectionError()
  if (error.value) return
  await guarded(async () => {
    probeResult.value = await upstreamsApi.probe(connection())
  })
}

function authInput(): UpstreamAuthInput {
  const input: UpstreamAuthInput =
    props.mode === 'reauth' && props.account
      ? { account_id: props.account.id }
      : { ...connection(), name: form.name.trim() }
  if (form.method === 'password') {
    input.username = secret.username.trim()
    input.password = secret.password
  } else if (form.method === 'pat') input.pat = secret.token.trim()
  else input.refresh_token = secret.token.trim()
  return input
}

function finishAuth(result: UpstreamAuthResult) {
  if (result.kind === 'challenge') {
    secret.password = ''
    challenge.value = result.challenge
    return
  }
  clearSecrets()
  toast.success(
    t(
      props.mode === 'reauth'
        ? 'upstreams.account.reauthorized'
        : 'upstreams.account.created'
    )
  )
  emit('done')
}

async function authorize() {
  if (props.mode === 'create') {
    error.value = form.name.trim()
      ? connectionError()
      : t('upstreams.keyForm.nameRequired')
    if (error.value) return
  }
  const missing =
    form.method === 'password'
      ? !secret.username.trim() || !secret.password
      : !secret.token.trim()
  if (missing) {
    error.value = t('upstreams.errors.invalid_credentials')
    return
  }
  await guarded(async () => finishAuth(await upstreamsApi.auth(authInput())))
}

async function verify() {
  if (!codeValid.value) {
    error.value = t('upstreams.account.codeInvalid')
    return
  }
  await guarded(async () =>
    finishAuth(
      await upstreamsApi.verify({
        challenge: challenge.value,
        code: code.value,
      })
    )
  )
}

function backFromChallenge() {
  challenge.value = ''
  code.value = ''
  error.value = ''
}

async function saveSettings() {
  const account = props.account
  if (!account) return
  const input: { name?: string; enabled?: boolean } = {}
  if (form.name.trim() !== account.name) input.name = form.name.trim()
  if (form.enabled !== (account.status !== 'disabled'))
    input.enabled = form.enabled
  if (!Object.keys(input).length) {
    error.value = t('upstreams.account.noChange')
    return
  }
  await guarded(async () => {
    await upstreamsApi.update(account.id, input)
    toast.success(t('upstreams.saved'))
    emit('done')
  })
}

function endpointInput(
  account: UpstreamAccount
): UpstreamEndpointUpdate | string {
  const input: UpstreamEndpointUpdate = {}
  const baseUrl = form.baseUrl.trim()
  if (baseUrl !== account.base_url) {
    if (!isHttpUrl(baseUrl)) return t('upstreams.account.invalidUrl')
    input.base_url = baseUrl
  }
  if (form.proxyMode === 'clear') input.proxy_url = ''
  if (form.proxyMode === 'set') {
    if (!isHttpUrl(form.proxyUrl.trim()))
      return t('upstreams.account.invalidUrl')
    input.proxy_url = form.proxyUrl.trim()
  }
  return Object.keys(input).length ? input : t('upstreams.account.noChange')
}

async function saveEndpoint() {
  const account = props.account
  if (!account) return
  const input = endpointInput(account)
  if (typeof input === 'string') {
    error.value = input
    return
  }
  await guarded(async () => {
    await upstreamsApi.updateEndpoint(account.id, input)
    toast.success(t('upstreams.saved'))
    emit('done')
  })
}

function submit() {
  if (props.mode === 'settings') return saveSettings()
  if (props.mode === 'endpoint') return saveEndpoint()
  return challenge.value ? verify() : authorize()
}
</script>
<template>
  <ConsoleModal
    open
    :title="title"
    size="md"
    :close-disabled="busy"
    @close="close"
  >
    <form class="space-y-4" novalidate @submit.prevent="submit">
      <template v-if="challenge">
        <p class="text-sm font-semibold text-[var(--text-primary)]">
          {{ t('upstreams.account.twoFactorTitle') }}
        </p>
        <p class="text-sm text-[var(--text-secondary)]">
          {{ t('upstreams.account.twoFactorHint') }}
        </p>
        <FormField :label="t('upstreams.account.code')">
          <TextInput
            v-model="code"
            name="upstream-totp"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
          />
        </FormField>
      </template>

      <template v-else-if="isAuth">
        <template v-if="mode === 'create'">
          <FormField :label="t('upstreams.account.platform')" group>
            <SegmentedToggle
              :model-value="form.platform"
              :options="platformOptions"
              :label="t('upstreams.account.platform')"
              @update:model-value="setPlatform"
            />
          </FormField>
          <FormField :label="t('upstreams.account.name')">
            <TextInput
              v-model="form.name"
              name="upstream-name"
              :placeholder="t('upstreams.account.namePlaceholder')"
            />
          </FormField>
          <FormField :label="t('upstreams.account.baseUrl')">
            <TextInput
              v-model="form.baseUrl"
              name="upstream-base-url"
              type="url"
              :placeholder="t('upstreams.account.baseUrlPlaceholder')"
              @update:model-value="probeResult = null"
            />
          </FormField>
          <FormField :label="t('upstreams.account.proxyUrl')">
            <TextInput
              v-model="form.proxyUrl"
              name="upstream-proxy-url"
              :placeholder="t('upstreams.account.proxyPlaceholder')"
              @update:model-value="probeResult = null"
            />
          </FormField>
          <div class="flex flex-wrap items-center gap-2">
            <ConsoleButton
              variant="secondary"
              size="sm"
              :loading="busy"
              data-upstream-probe
              @click="probe"
            >
              {{ t('upstreams.account.probe') }}
            </ConsoleButton>
            <span
              v-if="probeResult"
              class="text-xs text-[var(--status-success-text)]"
              role="status"
            >
              {{ t('upstreams.account.probeOk') }} ·
              {{
                t('upstreams.account.probeSite', {
                  site: probeResult.site_name,
                  version: probeResult.version,
                })
              }}
            </span>
          </div>
          <p
            v-if="probeResult && !probeResult.password_login_available"
            class="rounded-lg bg-[var(--status-warning-soft)] px-3 py-2 text-xs text-[var(--status-warning-text)]"
          >
            {{ t(`upstreams.account.passwordUnavailable.${form.platform}`) }}
          </p>
          <p
            v-else-if="probeResult?.captcha_enabled"
            class="rounded-lg bg-[var(--status-warning-soft)] px-3 py-2 text-xs text-[var(--status-warning-text)]"
          >
            {{ t('upstreams.account.captchaEnabled') }}
          </p>
        </template>
        <p v-else class="text-sm text-[var(--text-secondary)]">
          {{ t('upstreams.account.reauthHint') }}
        </p>

        <FormField :label="t('upstreams.account.authMethod')" group>
          <SegmentedToggle
            v-model="form.method"
            :options="methodOptions"
            :label="t('upstreams.account.authMethod')"
          />
        </FormField>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t(`upstreams.account.methodHint.${form.method}`) }}
        </p>
        <template v-if="form.method === 'password'">
          <FormField :label="t('upstreams.account.username')">
            <TextInput
              v-model="secret.username"
              name="upstream-username"
              autocomplete="off"
            />
          </FormField>
          <FormField :label="t('upstreams.account.password')">
            <TextInput
              v-model="secret.password"
              name="upstream-password"
              type="password"
              autocomplete="new-password"
            />
          </FormField>
        </template>
        <FormField
          v-else
          :label="
            form.method === 'pat'
              ? t('upstreams.account.pat')
              : t('upstreams.account.refreshToken')
          "
        >
          <TextInput
            v-model="secret.token"
            name="upstream-token"
            type="password"
            autocomplete="new-password"
          />
        </FormField>
      </template>

      <template v-else-if="mode === 'settings'">
        <FormField :label="t('upstreams.account.name')">
          <TextInput v-model="form.name" name="upstream-name" />
        </FormField>
        <ConsoleToggle
          v-model="form.enabled"
          :label="t('upstreams.account.enabled')"
          name="upstream-enabled"
        />
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('upstreams.account.enabledHint') }}
        </p>
      </template>

      <template v-else>
        <p
          class="rounded-lg bg-[var(--status-warning-soft)] px-3 py-2 text-sm text-[var(--status-warning-text)]"
          role="note"
          data-upstream-endpoint-warning
        >
          {{ t('upstreams.account.endpointWarning') }}
        </p>
        <FormField :label="t('upstreams.account.baseUrl')">
          <TextInput
            v-model="form.baseUrl"
            name="upstream-base-url"
            type="url"
          />
        </FormField>
        <FormField
          :label="t('upstreams.account.proxyMode')"
          :hint="
            account?.has_proxy
              ? t('upstreams.account.proxyCurrent.yes')
              : t('upstreams.account.proxyCurrent.no')
          "
          group
        >
          <SegmentedToggle
            v-model="form.proxyMode"
            :options="proxyOptions"
            :label="t('upstreams.account.proxyMode')"
          />
        </FormField>
        <FormField
          v-if="form.proxyMode === 'set'"
          :label="t('upstreams.account.proxyUrl')"
        >
          <TextInput
            v-model="form.proxyUrl"
            name="upstream-proxy-url"
            :placeholder="t('upstreams.account.proxyPlaceholder')"
          />
        </FormField>
        <p
          v-if="platform === 'sub2api' && endpointProxyChanges"
          class="text-xs text-[var(--status-warning-text)]"
          role="note"
        >
          {{ t('upstreams.account.sessionBindingWarning') }}
        </p>
      </template>

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
        v-if="challenge"
        variant="ghost"
        :disabled="busy"
        @click="backFromChallenge"
      >
        {{ t('upstreams.account.back') }}
      </ConsoleButton>
      <ConsoleButton variant="secondary" :disabled="busy" @click="close">
        {{ t('common.cancel') }}
      </ConsoleButton>
      <ConsoleButton :loading="busy" data-upstream-submit @click="submit">
        {{
          challenge
            ? t('upstreams.account.verify')
            : isAuth
              ? t('upstreams.account.authorize')
              : t('common.save')
        }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
</template>
