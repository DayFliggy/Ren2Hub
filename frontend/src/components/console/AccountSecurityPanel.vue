<script setup lang="ts">
import {
  AlertTriangle,
  AtSign,
  Github,
  KeyRound,
  Link2,
  LockKeyhole,
  Mail,
  MessageCircle,
  MessagesSquare,
  RefreshCw,
  Send,
  ShieldCheck,
  Terminal,
  Trash2,
  UserRound,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { publicApi, type PublicStatus } from '@/api/public'
import type { SettingsBinding, SettingsBindingId } from '@/types/settings'
import type { UserInfo } from '@/types/auth'

import SettingsSectionHeading from './SettingsSectionHeading.vue'
import PersonalAccessTokensPanel from './PersonalAccessTokensPanel.vue'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import { completeOAuth } from '@/api/oauth'
import { proofOptions } from '@/api/security'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { applyAuthRotation, parseAuthRotation } from '@/api/authSession'
import { useAuthStore } from '@/stores/auth'

const oauthAbort = new AbortController()

const props = defineProps<{ user: UserInfo | null }>()

const emit = defineEmits<{
  editProfile: []
  changePassword: []
  deleteAccount: []
}>()

const { t } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const proofDialog = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const enrollmentFlow = ref('')
const emailFlow = ref('')
const emailOldRequired = ref(false)
const emailOldCode = ref('')
const emailResendAt = ref(0)
const currentTime = ref(Date.now())
let emailTimer: ReturnType<typeof setInterval> | null = null
const passkeyEnabled = ref(false)
const passkeyLastUsedAt = ref<string | null>(null)
const twoFAEnabled = ref(false)
const backupCodesRemaining = ref(0)
const setupQRCode = ref('')
const setupBackupCodes = ref<string[]>([])

const passkeyRegisterOpen = ref(false)
const passkeyRemoveOpen = ref(false)
const twoFASetupOpen = ref(false)
const twoFACode = ref('')
const twoFAAction = ref<'regenerate' | 'disable' | null>(null)
const backupCodesOpen = ref(false)
const visibleBackupCodes = ref<string[]>([])
const bindTarget = ref<SettingsBindingId | null>(null)
const unbindTarget = ref<SettingsBindingId | null>(null)
const bindingEmail = ref('')
const bindingCode = ref('')
const emailSending = ref(false)
const customBindings = ref<SettingsBinding[]>([])
const publicStatus = ref<PublicStatus | null>(null)

const bindingMeta: Record<
  Exclude<SettingsBindingId, `custom:${number}`>,
  { icon: typeof Mail; labelKey: string }
> = {
  email: { icon: Mail, labelKey: 'settings.bindingEmail' },
  github: { icon: Github, labelKey: 'settings.bindingGithub' },
  linuxdo: { icon: Terminal, labelKey: 'settings.bindingLinuxDO' },
  discord: { icon: MessageCircle, labelKey: 'settings.bindingDiscord' },
  oidc: { icon: ShieldCheck, labelKey: 'settings.bindingOIDC' },
  wechat: { icon: MessagesSquare, labelKey: 'settings.bindingWeChat' },
  telegram: { icon: Send, labelKey: 'settings.bindingTelegram' },
} as const

const builtinBindings = computed<SettingsBinding[]>(() =>
  (
    [
      {
        id: 'email',
        bound: Boolean(props.user?.email),
        account: props.user?.email?.trim() ?? '',
      },
      {
        id: 'github',
        bound: Boolean(props.user?.github_id),
        account: props.user?.github_id?.trim() ?? '',
      },
      {
        id: 'linuxdo',
        bound: Boolean(props.user?.linux_do_id),
        account: props.user?.linux_do_id?.trim() ?? '',
      },
      {
        id: 'discord',
        bound: Boolean(props.user?.discord_id),
        account: props.user?.discord_id?.trim() ?? '',
      },
      {
        id: 'oidc',
        bound: Boolean(props.user?.oidc_id),
        account: props.user?.oidc_id?.trim() ?? '',
      },
      {
        id: 'wechat',
        bound: Boolean(props.user?.wechat_id),
        account: props.user?.wechat_id?.trim() ?? '',
      },
      {
        id: 'telegram',
        bound: Boolean(props.user?.telegram_id),
        account: props.user?.telegram_id?.trim() ?? '',
      },
    ] as SettingsBinding[]
  ).filter((binding) => {
    if (binding.id === 'email') return true
    const status = publicStatus.value
    if (!status) return binding.bound
    const enabled: Record<string, boolean | undefined> = {
      github: status.github_oauth,
      discord: status.discord_oauth,
      linuxdo: status.linuxdo_oauth,
      oidc: status.oidc_enabled,
      wechat: status.wechat_login,
      telegram: status.telegram_oauth && status.telegram_oauth_configured,
    }
    return Boolean(enabled[binding.id] || binding.bound)
  })
)

const bindings = computed<SettingsBinding[]>(() => [
  ...builtinBindings.value,
  ...customBindings.value,
])

function bindingMetaFor(binding: SettingsBinding) {
  if (binding.id.startsWith('custom:')) {
    return {
      icon: Link2,
      label:
        binding.providerName ||
        binding.providerSlug ||
        t('settings.customOAuth'),
    }
  }
  const meta =
    bindingMeta[binding.id as Exclude<SettingsBindingId, `custom:${number}`>]
  return { icon: meta.icon, label: t(meta.labelKey) }
}

const bindingItems = computed(() =>
  bindings.value.map((binding) => ({
    ...binding,
    ...bindingMetaFor(binding),
  }))
)

const currentBindLabel = computed(() =>
  bindTarget.value
    ? bindingMetaFor(
        bindings.value.find((item) => item.id === bindTarget.value) ?? {
          id: bindTarget.value,
          bound: false,
          account: '',
        }
      ).label
    : ''
)

const currentUnbindLabel = computed(() =>
  unbindTarget.value
    ? bindingMetaFor(
        bindings.value.find((item) => item.id === unbindTarget.value) ?? {
          id: unbindTarget.value,
          bound: true,
          account: '',
        }
      ).label
    : ''
)

const emailBinding = computed(() =>
  bindings.value.find((item) => item.id === 'email')!
)

function applyRotation(data: unknown): void {
  const rotation = parseAuthRotation(data)
  if (rotation) applyAuthRotation(rotation)
}

type CredentialCreateOptions = NonNullable<
  Parameters<typeof navigator.credentials.create>[0]
>
type PasskeyCreationOptions = NonNullable<CredentialCreateOptions['publicKey']>
type PasskeyCredentialDescriptor = NonNullable<
  PasskeyCreationOptions['excludeCredentials']
>[number]
type WireCredentialDescriptor = Omit<PasskeyCredentialDescriptor, 'id'> & {
  id: string
}
type WirePasskeyCreationOptions = Omit<
  PasskeyCreationOptions,
  'challenge' | 'user' | 'excludeCredentials'
> & {
  challenge: string
  user: Omit<PasskeyCreationOptions['user'], 'id'> & { id: string }
  excludeCredentials?: WireCredentialDescriptor[]
}

function base64urlBuffer(value: string): ArrayBuffer {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4)
  const binary = atob(padded)
  return Uint8Array.from(binary, (char) => char.charCodeAt(0))
    .buffer as ArrayBuffer
}

function credentialJSON(
  credential: PublicKeyCredential
): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse
  return {
    id: credential.id,
    rawId: btoa(String.fromCharCode(...new Uint8Array(credential.rawId)))
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/g, ''),
    type: credential.type,
    response: {
      clientDataJSON: btoa(
        String.fromCharCode(...new Uint8Array(response.clientDataJSON))
      )
        .replace(/\+/g, '-')
        .replace(/\//g, '_')
        .replace(/=+$/g, ''),
      attestationObject: btoa(
        String.fromCharCode(...new Uint8Array(response.attestationObject))
      )
        .replace(/\+/g, '-')
        .replace(/\//g, '_')
        .replace(/=+$/g, ''),
    },
  }
}

async function loadSecurity(): Promise<void> {
  try {
    const [passkey, twoFA, status, oauth] = await Promise.all([
      api.get<{ enabled: boolean; last_used_at?: string }>('/api/user/passkey'),
      api.get<{ enabled: boolean; backup_codes_remaining?: number }>(
        '/api/user/2fa/status'
      ),
      publicApi.status(),
      api.get<
        Array<{
          provider_id: number
          provider_name: string
          provider_slug: string
          provider_icon?: string
          provider_user_id: string
        }>
      >('/api/user/oauth/bindings'),
    ])
    passkeyEnabled.value = passkey.enabled
    passkeyLastUsedAt.value = passkey.last_used_at ?? null
    twoFAEnabled.value = twoFA.enabled
    backupCodesRemaining.value = twoFA.backup_codes_remaining ?? 0
    publicStatus.value = status
    const storedBindings = oauth.map((binding) => ({
      id: `custom:${binding.provider_id}` as SettingsBindingId,
      bound: true,
      account: binding.provider_user_id,
      providerId: binding.provider_id,
      providerSlug: binding.provider_slug,
      providerName: binding.provider_name,
      providerIcon: binding.provider_icon,
    }))
    customBindings.value = [
      ...storedBindings,
      ...(status.custom_oauth_providers ?? [])
        .filter(
          (provider) =>
            !storedBindings.some(
              (binding) => binding.providerId === provider.id
            )
        )
        .map((provider) => ({
          id: `custom:${provider.id}` as SettingsBindingId,
          bound: false,
          account: '',
          providerId: provider.id,
          providerSlug: provider.slug,
          providerName: provider.name,
          providerIcon: provider.icon,
        })),
    ]
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function openOAuthPopup(provider: string): Promise<void> {
  const popup = window.open('', 'ren2hub-oauth', 'popup,width=560,height=720')
  if (!popup) {
    toast.error(t('settings.oauthPopupBlocked'))
    return
  }
  let popupTransferred = false
  try {
    const proof = await proofDialog.value!.request({
      scope: 'account.binding.bind',
      context: { provider },
    })
    const status = await publicApi.status()
    popupTransferred = true
    await completeOAuth(
      status,
      provider,
      'bind',
      undefined,
      proof,
      oauthAbort.signal,
      popup
    )
    await auth.fetchSelf(false)
    await loadSecurity()
    toast.success(t('settings.bindingSaved'))
  } catch (error) {
    if (!popupTransferred) popup.close()
    toast.error(error instanceof Error ? error.message : String(error))
  }
}

async function sendEmailCode(): Promise<void> {
  const email = bindingEmail.value.trim().toLowerCase()
  if (!email.includes('@')) {
    toast.error(t('settings.emailBindingInvalid'))
    return
  }
  emailSending.value = true
  try {
    let data: {
      flow_token: string
      old_email_required: boolean
      resend_at: number
    }
    if (emailFlow.value) {
      data = await api.post('/api/oauth/email/bind/resend', {
        flow_token: emailFlow.value,
      })
    } else {
      const proof = await proofDialog.value!.request({
        scope: 'account.binding.bind',
        context: { provider: 'email', email },
      })
      data = await api.post(
        '/api/oauth/email/bind/start',
        { email },
        proofOptions(proof)
      )
    }
    emailFlow.value = data.flow_token
    emailOldRequired.value = data.old_email_required
    emailResendAt.value = data.resend_at
    toast.success(t('settings.emailCodeSent'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    emailSending.value = false
  }
}

async function registerPasskey(): Promise<void> {
  try {
    const proof = await proofDialog.value!.request({
      scope: 'passkey.register',
    })
    const headers = proof ? { 'X-Security-Proof': proof } : undefined
    const begin = await api.post<{
      options: { publicKey: WirePasskeyCreationOptions }
      flow_token: string
    }>('/api/user/passkey/register/begin', undefined, { headers })
    const wirePublicKey = begin.options.publicKey
    const publicKey: PasskeyCreationOptions = {
      ...wirePublicKey,
      challenge: base64urlBuffer(wirePublicKey.challenge),
      user: {
        ...wirePublicKey.user,
        id: base64urlBuffer(wirePublicKey.user.id),
      },
      excludeCredentials: wirePublicKey.excludeCredentials?.map(
        (descriptor) => ({
          ...descriptor,
          id: base64urlBuffer(descriptor.id),
        })
      ),
    }
    const credential = await navigator.credentials.create({ publicKey })
    if (!(credential instanceof PublicKeyCredential))
      throw new Error('Passkey registration was cancelled')
    const finish = await api.post<unknown>(
      '/api/user/passkey/register/finish',
      {
        flow_token: begin.flow_token,
        credential: credentialJSON(credential),
      },
      { headers }
    )
    applyRotation(finish)
    passkeyRegisterOpen.value = false
    toast.success(t('settings.passkeyEnabled'))
    await loadSecurity()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function removePasskey(): Promise<void> {
  try {
    const proof = await proofDialog.value!.request({ scope: 'passkey.delete' })
    const result = await api.delete<unknown>('/api/user/passkey', undefined, {
      headers: proof ? { 'X-Security-Proof': proof } : undefined,
    })
    applyRotation(result)
    passkeyRemoveOpen.value = false
    toast.success(t('settings.passkeyRemoved'))
    await loadSecurity()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function openTwoFASetup(): Promise<void> {
  try {
    const proof = await proofDialog.value!.request({ scope: '2fa.setup' })
    const result = await api.post<{
      qr_code_data: string
      backup_codes: string[]
      flow_token: string
    }>('/api/user/2fa/setup', undefined, proofOptions(proof))
    enrollmentFlow.value = result.flow_token
    setupQRCode.value = result.qr_code_data
    setupBackupCodes.value = result.backup_codes
    twoFACode.value = ''
    twoFASetupOpen.value = true
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function confirmTwoFASetup(): Promise<void> {
  if (!/^\d{6}$/.test(twoFACode.value)) return
  try {
    const result = await api.post<unknown>('/api/user/2fa/enable', {
      flow_token: enrollmentFlow.value,
      code: twoFACode.value,
    })
    applyRotation(result)
    visibleBackupCodes.value = setupBackupCodes.value
    twoFASetupOpen.value = false
    backupCodesOpen.value = true
    toast.success(t('settings.twoFAEnabled'))
    await loadSecurity()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

function openTwoFAAction(action: 'regenerate' | 'disable'): void {
  twoFAAction.value = action
  void confirmTwoFAAction()
}

async function confirmTwoFAAction(): Promise<void> {
  if (!twoFAAction.value) return
  const endpoint =
    twoFAAction.value === 'regenerate'
      ? '/api/user/2fa/backup_codes'
      : '/api/user/2fa/disable'
  try {
    const proof = await proofDialog.value!.request({
      scope:
        twoFAAction.value === 'regenerate'
          ? '2fa.backup_codes.regenerate'
          : '2fa.disable',
    })
    const result = await api.post<
      { backup_codes?: string[] } & Record<string, unknown>
    >(endpoint, undefined, proofOptions(proof))
    applyRotation(result)
    if (twoFAAction.value === 'regenerate') {
      visibleBackupCodes.value = result.backup_codes ?? []
      backupCodesOpen.value = true
      toast.success(t('settings.backupCodesRegenerated'))
    } else {
      toast.success(t('settings.twoFADisabled'))
    }
    twoFAAction.value = null
    await loadSecurity()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  }
}

async function copyBackupCodes(): Promise<void> {
  try {
    await navigator.clipboard.writeText(visibleBackupCodes.value.join('\n'))
    toast.success(t('settings.backupCodesCopied'))
  } catch {
    toast.error(t('common.failed'))
  }
}

function openBinding(binding: SettingsBinding): void {
  if (binding.bound && binding.id.startsWith('custom:')) {
    unbindTarget.value = binding.id
    return
  }
  if (binding.bound && binding.id !== 'email') return
  bindTarget.value = binding.id
  bindingEmail.value = binding.id === 'email' ? (props.user?.email ?? '') : ''
  bindingCode.value = ''
  emailFlow.value = ''
  emailOldCode.value = ''
  emailOldRequired.value = false
  emailResendAt.value = 0
}

async function confirmBinding(): Promise<void> {
  if (!bindTarget.value) return
  if (bindTarget.value === 'email') {
    if (
      !bindingEmail.value.includes('@') ||
      !/^\d{6}$/.test(bindingCode.value)
    ) {
      toast.error(t('settings.emailBindingInvalid'))
      return
    }
    try {
      await api.post('/api/oauth/email/bind', {
        flow_token: emailFlow.value,
        new_code: bindingCode.value,
        old_code: emailOldCode.value,
      })
      await auth.fetchSelf(false)
      toast.success(t('settings.bindingSaved'))
      bindTarget.value = null
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
    return
  } else if (bindTarget.value === 'wechat') {
    try {
      const code = bindingCode.value.trim()
      const proof = await proofDialog.value!.request({
        scope: 'account.binding.bind',
        context: { provider: 'wechat', code },
      })
      await api.post('/api/oauth/wechat/bind', { code }, proofOptions(proof))
      await auth.fetchSelf(false)
      await loadSecurity()
      bindTarget.value = null
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    }
  } else {
    const provider =
      bindings.value.find((item) => item.id === bindTarget.value)
        ?.providerSlug ?? bindTarget.value
    bindTarget.value = null
    void openOAuthPopup(provider)
  }
}

async function confirmUnbind(): Promise<void> {
  if (!unbindTarget.value) return
  const binding = bindings.value.find((item) => item.id === unbindTarget.value)
  if (!binding?.providerId) return
  try {
    const proof = await proofDialog.value!.request({
      scope: 'account.binding.unbind',
      context: { provider_id: binding.providerId },
    })
    await api.delete(
      `/api/user/oauth/bindings/${binding.providerId}`,
      undefined,
      proofOptions(proof)
    )
    await loadSecurity()
    toast.success(t('settings.bindingRemoved'))
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    unbindTarget.value = null
  }
}

onMounted(() => {
  void loadSecurity()
  emailTimer = setInterval(() => {
    currentTime.value = Date.now()
  }, 1000)
})

onBeforeUnmount(() => {
  oauthAbort.abort()
  if (emailTimer) clearInterval(emailTimer)
})
</script>

<template>
  <ConsoleCard :padded="false" class="overflow-hidden">
    <header
      class="flex items-center justify-between gap-5 border-b border-[var(--border-subtle)] px-6 py-6 sm:px-7"
    >
      <div class="flex min-w-0 items-center gap-3">
        <ShieldCheck
          :size="20"
          :stroke-width="1.8"
          class="shrink-0 text-[var(--text-tertiary)]"
          aria-hidden="true"
        />
        <div class="min-w-0">
          <h2 class="truncate text-xl font-semibold text-[var(--text-primary)]">
            {{ t('settings.accountSecurityPanel') }}
          </h2>
          <p class="mt-1 text-sm leading-5 text-[var(--text-tertiary)]">
            {{ t('settings.accountSecuritySubtitle') }}
          </p>
        </div>
      </div>
      <span
        class="font-mono text-[10px] tracking-[0.16em] text-[var(--text-tertiary)]"
        >SECURITY</span
      >
    </header>

    <PersonalAccessTokensPanel v-if="user" :user-id="user.id" />
    <section>
      <SettingsSectionHeading
        index="01"
        :icon="UserRound"
        :title="t('settings.accountProfile')"
      />
      <div
        class="divide-y divide-[var(--border-subtle)] border-t border-[var(--border-subtle)]"
      >
        <div class="settings-ledger-row">
          <span class="settings-ledger-label">{{
            t('settings.displayName')
          }}</span>
          <span class="settings-ledger-value">{{
            user?.display_name || '—'
          }}</span>
          <button
            type="button"
            class="settings-row-action"
            @click="emit('editProfile')"
          >
            {{ t('common.edit') }}
          </button>
        </div>
        <div class="settings-ledger-row">
          <span class="settings-ledger-label">{{
            t('settings.username')
          }}</span>
          <span class="settings-ledger-value font-mono text-xs"
            >@{{ user?.username || '—' }}</span
          >
          <span class="settings-row-note">{{ t('settings.readonly') }}</span>
        </div>
        <div class="settings-ledger-row">
          <span class="settings-ledger-label">{{
            t('settings.loginEmail')
          }}</span>
          <span class="settings-ledger-value break-all">{{
            emailBinding?.account || t('settings.unbound')
          }}</span>
          <button
            type="button"
            class="settings-row-action"
            @click="openBinding(emailBinding)"
          >
            {{
              emailBinding?.bound ? t('settings.rebind') : t('settings.bind')
            }}
          </button>
        </div>
        <div class="settings-ledger-row">
          <span class="settings-ledger-label">{{
            t('settings.loginPassword')
          }}</span>
          <span class="settings-ledger-value font-mono tracking-[0.2em]"
            >••••••••••</span
          >
          <button
            type="button"
            class="settings-row-action"
            @click="emit('changePassword')"
          >
            {{ t('common.edit') }}
          </button>
        </div>
      </div>
    </section>

    <section class="border-t border-[var(--border-subtle)]">
      <SettingsSectionHeading
        index="02"
        :icon="KeyRound"
        :title="t('settings.passkeyLogin')"
      />
      <div class="border-t border-[var(--border-subtle)] px-5 py-5 sm:px-6">
        <div
          class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"
        >
          <div class="flex min-w-0 items-start gap-3">
            <span class="settings-icon-tile"
              ><KeyRound :size="19" aria-hidden="true"
            /></span>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <p class="font-semibold text-[var(--text-primary)]">
                  {{ t('settings.passkeyAuth') }}
                </p>
                <StatusChip :tone="passkeyEnabled ? 'success' : 'neutral'">
                  {{
                    passkeyEnabled
                      ? t('settings.enabled')
                      : t('settings.disabled')
                  }}
                </StatusChip>
              </div>
              <p class="mt-1 text-sm text-[var(--text-tertiary)]">
                {{ t('settings.lastUsed') }}：{{
                  passkeyLastUsedAt || t('settings.neverUsed')
                }}
              </p>
            </div>
          </div>
          <ConsoleButton
            :variant="passkeyEnabled ? 'secondary' : 'primary'"
            size="sm"
            class="w-full sm:w-auto"
            @click="
              passkeyEnabled
                ? (passkeyRemoveOpen = true)
                : (passkeyRegisterOpen = true)
            "
          >
            {{
              passkeyEnabled
                ? t('settings.removePasskey')
                : t('settings.enablePasskey')
            }}
          </ConsoleButton>
        </div>
      </div>
    </section>

    <section class="border-t border-[var(--border-subtle)]">
      <SettingsSectionHeading
        index="03"
        :icon="LockKeyhole"
        :title="t('settings.twoFA')"
      />
      <div class="border-t border-[var(--border-subtle)] px-5 py-5 sm:px-6">
        <div class="flex flex-col gap-4">
          <div class="flex items-start gap-3">
            <span class="settings-icon-tile"
              ><ShieldCheck :size="19" aria-hidden="true"
            /></span>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <p class="font-semibold text-[var(--text-primary)]">
                  {{ t('settings.twoFA') }}
                </p>
                <StatusChip :tone="twoFAEnabled ? 'success' : 'neutral'">
                  {{
                    twoFAEnabled
                      ? t('settings.enabled')
                      : t('settings.disabled')
                  }}
                </StatusChip>
              </div>
              <p class="mt-1 text-sm text-[var(--text-tertiary)]">
                {{
                  twoFAEnabled
                    ? t('settings.backupCodesRemaining', {
                        count: backupCodesRemaining,
                      })
                    : t('settings.twoFADesc')
                }}
              </p>
            </div>
          </div>
          <div v-if="twoFAEnabled" class="grid gap-2 sm:grid-cols-2">
            <ConsoleButton
              variant="secondary"
              size="sm"
              @click="openTwoFAAction('regenerate')"
            >
              <RefreshCw :size="15" aria-hidden="true" />
              {{ t('settings.regenerateBackupCodes') }}
            </ConsoleButton>
            <ConsoleButton
              variant="danger"
              size="sm"
              @click="openTwoFAAction('disable')"
            >
              <AlertTriangle :size="15" aria-hidden="true" />
              {{ t('settings.disableTwoFA') }}
            </ConsoleButton>
          </div>
          <ConsoleButton
            v-else
            size="sm"
            class="w-full sm:w-fit"
            @click="openTwoFASetup"
          >
            {{ t('settings.enableTwoFA') }}
          </ConsoleButton>
        </div>
      </div>
    </section>

    <section class="border-t border-[var(--border-subtle)]">
      <SettingsSectionHeading
        index="04"
        :icon="Link2"
        :title="t('settings.authSources')"
      >
        <span class="text-xs text-[var(--text-tertiary)]">{{
          t('settings.boundCount', {
            count: bindings.filter((item) => item.bound).length,
            total: bindings.length,
          })
        }}</span>
      </SettingsSectionHeading>
      <div class="grid border-t border-[var(--border-subtle)] sm:grid-cols-2">
        <div
          v-for="(binding, index) in bindingItems"
          :key="binding.id"
          class="flex min-w-0 items-center justify-between gap-3 border-b border-[var(--border-subtle)] px-5 py-4 sm:px-6"
          :class="{ 'sm:border-r': index % 2 === 0 }"
        >
          <div class="flex min-w-0 items-center gap-3">
            <span class="settings-icon-tile size-9">
              <component :is="binding.icon" :size="17" aria-hidden="true" />
            </span>
            <div class="min-w-0">
              <p
                class="truncate text-sm font-semibold text-[var(--text-primary)]"
              >
                {{ binding.label }}
              </p>
              <p class="truncate text-xs text-[var(--text-tertiary)]">
                {{ binding.bound ? binding.account : t('settings.unbound') }}
              </p>
            </div>
          </div>
          <ConsoleButton
            :variant="binding.bound ? 'ghost' : 'secondary'"
            size="sm"
            :disabled="
              binding.bound &&
              binding.id !== 'email' &&
              !binding.id.startsWith('custom:')
            "
            @click="openBinding(binding)"
          >
            {{
              binding.id === 'email' && binding.bound
                ? t('common.edit')
                : binding.bound
                  ? t('settings.unbind')
                  : t('settings.bind')
            }}
          </ConsoleButton>
        </div>
      </div>
    </section>

    <section class="border-t border-[var(--status-danger-soft)]">
      <SettingsSectionHeading
        index="05"
        :icon="AlertTriangle"
        :title="t('settings.dangerTitle')"
        danger
      />
      <div
        class="flex flex-col gap-4 border-t border-[var(--border-subtle)] px-5 py-5 sm:flex-row sm:items-center sm:justify-between sm:px-6"
      >
        <div>
          <p class="font-semibold text-[var(--status-danger-text)]">
            {{ t('settings.deleteAccount') }}
          </p>
          <p class="mt-1 text-sm text-[var(--text-tertiary)]">
            {{ t('settings.deleteAccountDesc') }}
          </p>
        </div>
        <ConsoleButton
          variant="danger"
          class="w-full sm:w-auto"
          @click="emit('deleteAccount')"
        >
          <Trash2 :size="16" aria-hidden="true" />
          {{ t('settings.deleteAccount') }}
        </ConsoleButton>
      </div>
    </section>
  </ConsoleCard>

  <ConsoleModal
    :open="passkeyRegisterOpen"
    :title="t('settings.enablePasskey')"
    size="sm"
    @close="passkeyRegisterOpen = false"
  >
    <div class="settings-context-callout">
      <KeyRound :size="20" aria-hidden="true" />
      <p>{{ t('settings.passkeyPrompt') }}</p>
    </div>
    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          @click="passkeyRegisterOpen = false"
          >{{ t('common.cancel') }}</ConsoleButton
        >
        <ConsoleButton size="lg" @click="registerPasskey">{{
          t('common.confirm')
        }}</ConsoleButton>
      </div>
    </template>
  </ConsoleModal>

  <ConfirmDialog
    :open="passkeyRemoveOpen"
    :title="t('settings.removePasskey')"
    :message="t('settings.removePasskeyConfirm')"
    @confirm="removePasskey"
    @cancel="passkeyRemoveOpen = false"
  >
  </ConfirmDialog>

  <ConsoleModal
    :open="twoFASetupOpen"
    :title="t('settings.twoFASetup')"
    size="sm"
    @close="twoFASetupOpen = false"
  >
    <div class="flex flex-col items-center gap-5">
      <img
        v-if="setupQRCode"
        :src="setupQRCode"
        :alt="t('settings.twoFAQRCodeAlt')"
        class="size-36 rounded-lg border border-[var(--border-subtle)] bg-[#ffffff] p-3"
      />
      <div
        class="w-full rounded-lg bg-[var(--surface-muted)] px-4 py-3 text-center font-mono text-xs text-[var(--text-secondary)]"
      >
        {{ t('settings.twoFASecretHint') }}
      </div>
      <FormField
        :label="t('settings.twoFACode')"
        :hint="t('settings.sixDigitHint')"
        class="w-full"
      >
        <TextInput
          v-model="twoFACode"
          inputmode="numeric"
          maxlength="6"
          placeholder="000000"
        />
      </FormField>
    </div>
    <template #footer>
      <ConsoleButton
        block
        size="lg"
        :disabled="!/^\d{6}$/.test(twoFACode)"
        @click="confirmTwoFASetup"
      >
        {{ t('settings.enableTwoFA') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>

  <ConsoleModal
    :open="backupCodesOpen"
    :title="t('settings.backupCodes')"
    size="sm"
    @close="backupCodesOpen = false"
  >
    <div class="grid grid-cols-2 gap-2">
      <code
        v-for="code in visibleBackupCodes"
        :key="code"
        class="rounded-lg bg-[var(--surface-muted)] px-3 py-2 text-center text-xs text-[var(--text-primary)]"
        >{{ code }}</code
      >
    </div>
    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton variant="secondary" size="lg" @click="copyBackupCodes">{{
          t('common.copy')
        }}</ConsoleButton>
        <ConsoleButton size="lg" @click="backupCodesOpen = false">{{
          t('common.close')
        }}</ConsoleButton>
      </div>
    </template>
  </ConsoleModal>

  <ConsoleModal
    :open="bindTarget !== null"
    :title="t('settings.bindProvider', { provider: currentBindLabel })"
    size="sm"
    @close="bindTarget = null"
  >
    <div v-if="bindTarget === 'email'" class="space-y-4">
      <FormField :label="t('settings.email')">
        <TextInput
          v-model="bindingEmail"
          type="email"
          :disabled="Boolean(emailFlow)"
          placeholder="name@example.com"
        />
      </FormField>
      <FormField
        :label="t('settings.twoFACode')"
        :hint="t('settings.emailCodeHint')"
      >
        <TextInput
          v-model="bindingCode"
          inputmode="numeric"
          maxlength="6"
          placeholder="000000"
        />
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="
            emailSending ||
            (Boolean(emailFlow) && currentTime < emailResendAt * 1000)
          "
          @click="sendEmailCode"
        >
          {{ emailSending ? t('common.loading') : t('settings.sendEmailCode') }}
        </ConsoleButton>
      </FormField>
    </div>
    <FormField v-if="bindTarget === 'wechat'" :label="t('security.code')"
      ><TextInput v-model="bindingCode" maxlength="128"
    /></FormField>
    <FormField
      v-if="bindTarget === 'email' && emailOldRequired"
      :label="t('security.oldEmailCode')"
      ><TextInput
        v-model="emailOldCode"
        autocomplete="one-time-code"
        maxlength="6"
    /></FormField>
    <div v-if="bindTarget !== 'email'" class="settings-context-callout">
      <AtSign :size="20" aria-hidden="true" />
      <p>
        {{ t('settings.oauthPrompt', { provider: currentBindLabel }) }}
      </p>
    </div>
    <template #footer>
      <ConsoleButton block size="lg" @click="confirmBinding">{{
        t('settings.bind')
      }}</ConsoleButton>
    </template>
  </ConsoleModal>

  <ConfirmDialog
    :open="unbindTarget !== null"
    :title="t('settings.unbindProvider', { provider: currentUnbindLabel })"
    :message="t('settings.unbindConfirm')"
    @confirm="confirmUnbind"
    @cancel="unbindTarget = null"
  />
  <SecurityProofDialog ref="proofDialog" />
</template>

<style scoped>
.settings-ledger-row {
  display: grid;
  grid-template-columns: minmax(7rem, 0.8fr) minmax(0, 1.4fr) auto;
  align-items: center;
  gap: 1rem;
  min-height: 4.5rem;
  padding: 0.875rem 1.5rem;
}

.settings-ledger-label,
.settings-row-note {
  font-size: 0.75rem;
  color: var(--text-tertiary);
}

.settings-ledger-value {
  min-width: 0;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-primary);
}

.settings-row-note {
  text-align: right;
}

.settings-row-action {
  border-radius: 0.375rem;
  padding: 0.25rem 0.375rem;
  font-size: 0.75rem;
  color: var(--accent-text);
}

.settings-row-action:hover {
  background: var(--surface-muted);
}

.settings-icon-tile {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 0.625rem;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.settings-context-callout {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-muted);
  padding: 1rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
}

@media (max-width: 639px) {
  .settings-ledger-row {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.375rem 0.75rem;
    padding-inline: 1.25rem;
  }

  .settings-ledger-value {
    grid-column: 1 / -1;
    grid-row: 2;
  }

  .settings-row-action,
  .settings-row-note {
    grid-column: 2;
    grid-row: 1;
  }
}
</style>
