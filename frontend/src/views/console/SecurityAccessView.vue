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
  Settings2,
  ShieldCheck,
  Terminal,
  Trash2,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { api } from '@/api/console'
import { applyAuthRotation, parseAuthRotation } from '@/api/authSession'
import { completeOAuth } from '@/api/oauth'
import { proofOptions } from '@/api/security'
import { publicApi, type PublicStatus } from '@/api/public'
import { ApiError } from '@/api/types'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import PasswordStrengthMeter from '@/components/auth/PasswordStrengthMeter.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import LoginSessionsPanel from '@/components/console/LoginSessionsPanel.vue'
import PageHero from '@/components/console/PageHero.vue'
import PersonalAccessTokensPanel from '@/components/console/PersonalAccessTokensPanel.vue'
import SettingsSectionHeading from '@/components/console/SettingsSectionHeading.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import type { SettingsBinding, SettingsBindingId } from '@/types/settings'
import { formatTime } from '@/utils/format'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const toast = useToast()

const oauthAbort = new AbortController()

const proofDialog = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const enrollmentFlow = ref('')
const emailFlow = ref('')
const emailOldRequired = ref(false)
const emailOldCode = ref('')
const emailResendAt = ref(0)
const currentTime = ref(Date.now())
let emailTimer: ReturnType<typeof setInterval> | null = null

// null means the status could not be read; never render it as "disabled".
const passkeyEnabled = ref<boolean | null>(null)
const passkeyLastUsedAt = ref<string | null>(null)
const twoFAEnabled = ref<boolean | null>(null)
const backupCodesRemaining = ref(0)
const loadError = ref('')
let loadSequence = 0
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

// 显示名称
const profileOpen = ref(false)
const displayName = ref('')
const savingProfile = ref(false)

// 密码修改
const passwordOpen = ref(false)
const oldPassword = ref('')
const newPassword = ref('')
const savingPassword = ref(false)

// 账户删除
const deleteOpen = ref(false)
const deleteConfirmText = ref('')
const deleting = ref(false)

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
        bound: Boolean(auth.user?.email),
        account: auth.user?.email?.trim() ?? '',
      },
      {
        id: 'github',
        bound: Boolean(auth.user?.github_id),
        account: auth.user?.github_id?.trim() ?? '',
      },
      {
        id: 'linuxdo',
        bound: Boolean(auth.user?.linux_do_id),
        account: auth.user?.linux_do_id?.trim() ?? '',
      },
      {
        id: 'discord',
        bound: Boolean(auth.user?.discord_id),
        account: auth.user?.discord_id?.trim() ?? '',
      },
      {
        id: 'oidc',
        bound: Boolean(auth.user?.oidc_id),
        account: auth.user?.oidc_id?.trim() ?? '',
      },
      {
        id: 'wechat',
        bound: Boolean(auth.user?.wechat_id),
        account: auth.user?.wechat_id?.trim() ?? '',
      },
      {
        id: 'telegram',
        bound: Boolean(auth.user?.telegram_id),
        account: auth.user?.telegram_id?.trim() ?? '',
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
          bound: false,
          account: '',
        }
      ).label
    : ''
)

const emailBinding = computed(() =>
  bindings.value.find((item) => item.id === 'email')
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

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

/**
 * Each source settles on its own so one failed request cannot reset the
 * others to a misleading "disabled". A newer load supersedes an older one.
 */
async function loadSecurity(): Promise<void> {
  const sequence = ++loadSequence
  const [passkey, twoFA, status, oauth] = await Promise.allSettled([
    api.get<{ enabled: boolean; last_used_at?: string | null }>(
      '/api/user/passkey'
    ),
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
  if (sequence !== loadSequence) return

  const failure = [passkey, twoFA, status, oauth].find(
    (result) => result.status === 'rejected'
  )
  loadError.value = failure ? errorMessage(failure.reason) : ''

  if (passkey.status === 'fulfilled') {
    passkeyEnabled.value = passkey.value.enabled
    passkeyLastUsedAt.value = passkey.value.last_used_at ?? null
  } else {
    passkeyEnabled.value = null
  }
  if (twoFA.status === 'fulfilled') {
    twoFAEnabled.value = twoFA.value.enabled
    backupCodesRemaining.value = twoFA.value.backup_codes_remaining ?? 0
  } else {
    twoFAEnabled.value = null
  }
  if (status.status === 'fulfilled') publicStatus.value = status.value
  // Custom bindings need both sources; keep the last good list otherwise.
  if (status.status !== 'fulfilled' || oauth.status !== 'fulfilled') return

  const storedBindings = oauth.value.map((binding) => ({
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
    ...(status.value.custom_oauth_providers ?? [])
      .filter(
        (provider) =>
          !storedBindings.some((binding) => binding.providerId === provider.id)
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
}

/** The API sends an RFC 3339 time; show it in the console's local format. */
const passkeyLastUsedLabel = computed(() => {
  if (!passkeyLastUsedAt.value) return t('settings.neverUsed')
  const millis = Date.parse(passkeyLastUsedAt.value)
  return Number.isNaN(millis)
    ? passkeyLastUsedAt.value
    : formatTime(Math.floor(millis / 1000))
})

function statusLabel(enabled: boolean | null): string {
  if (enabled === null) return t('security.page.statusUnknown')
  return enabled ? t('settings.enabled') : t('settings.disabled')
}

function statusTone(enabled: boolean | null): 'success' | 'neutral' {
  return enabled ? 'success' : 'neutral'
}

function openProfile(): void {
  displayName.value = auth.user?.display_name ?? ''
  profileOpen.value = true
}

async function saveProfile(): Promise<void> {
  if (!displayName.value.trim()) return
  savingProfile.value = true
  try {
    await auth.updateProfile({ display_name: displayName.value.trim() })
    toast.success(t('settings.profileSaved'))
    profileOpen.value = false
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    savingProfile.value = false
  }
}

function openPassword(): void {
  oldPassword.value = ''
  newPassword.value = ''
  passwordOpen.value = true
}

async function changePassword(): Promise<void> {
  savingPassword.value = true
  try {
    const proof = await proofDialog.value!.request({
      scope: auth.user?.has_password
        ? 'account.password.change'
        : 'account.password.set',
    })
    await auth.changePassword(oldPassword.value, newPassword.value, proof)
    await auth.fetchSelf(false)
    toast.success(t('settings.passwordChanged'))
    passwordOpen.value = false
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    savingPassword.value = false
  }
}

async function deleteAccount(): Promise<void> {
  if (deleting.value || deleteConfirmText.value !== auth.user?.username) return
  deleting.value = true
  try {
    const proof = await proofDialog.value!.request({ scope: 'account.delete' })
    await auth.deleteAccount(proof)
    await router.push({ name: 'sign-in' })
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    deleting.value = false
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
    let credential: Credential | null
    try {
      credential = await navigator.credentials.create({ publicKey })
    } catch (error) {
      // The browser reports a dismissed prompt as NotAllowedError/AbortError
      // with English text; show the localized cancellation instead.
      if (
        error instanceof DOMException &&
        (error.name === 'NotAllowedError' || error.name === 'AbortError')
      ) {
        toast.error(t('security.page.passkeyCancelled'))
        return
      }
      throw error
    }
    if (!(credential instanceof PublicKeyCredential)) {
      toast.error(t('security.page.passkeyCancelled'))
      return
    }
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
  bindingEmail.value = binding.id === 'email' ? (auth.user?.email ?? '') : ''
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
  <div class="space-y-6" data-handdrawn-page="security">
    <PageHero
      :title="t('nav.securityAccess')"
      :crumbs="[t('nav.profile'), t('nav.securityAccess')]"
    >
      <p class="mt-1 max-w-2xl text-sm text-[var(--text-tertiary)]">
        {{ t('security.page.subtitle') }}
      </p>
    </PageHero>

    <ErrorBanner
      v-if="loadError"
      :message="`${t('security.page.loadFailed')} ${loadError}`"
      @retry="loadSecurity"
    />

    <div
      class="grid items-start gap-8 xl:grid-cols-[minmax(0,1.75fr)_minmax(340px,1.15fr)] 2xl:gap-10"
    >
      <!-- 主栏：凭据、认证、会话与凭证 -->
      <div class="min-w-0 space-y-8">
        <!-- 01 登录与凭据 + 认证来源 -->
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
                <h2
                  class="truncate text-xl font-semibold text-[var(--text-primary)]"
                >
                  {{ t('settings.accountSecurityPanel') }}
                </h2>
                <p class="mt-1 text-sm leading-5 text-[var(--text-tertiary)]">
                  {{ t('settings.accountSecuritySubtitle') }}
                </p>
              </div>
            </div>
            <span
              class="hidden shrink-0 font-mono text-[10px] uppercase tracking-[0.16em] text-[var(--text-tertiary)] sm:inline"
              >{{ t('security.page.eyebrowAccount') }}</span
            >
          </header>

          <section>
            <SettingsSectionHeading
              index="01"
              :icon="LockKeyhole"
              :title="t('settings.accountProfile')"
            />
            <div
              class="divide-y divide-[var(--border-subtle)] border-t border-[var(--border-subtle)]"
            >
              <div class="settings-ledger-row">
                <span class="settings-ledger-label">{{
                  t('settings.displayName')
                }}</span>
                <span class="settings-ledger-value truncate">{{
                  auth.user?.display_name || '—'
                }}</span>
                <ConsoleButton
                  variant="ghost"
                  size="sm"
                  class="settings-row-action"
                  @click="openProfile"
                >
                  {{ t('common.edit') }}
                </ConsoleButton>
              </div>

              <div class="settings-ledger-row">
                <span class="settings-ledger-label">{{
                  t('settings.username')
                }}</span>
                <span class="settings-ledger-value truncate font-mono">{{
                  auth.user?.username || '—'
                }}</span>
                <span class="settings-row-note">{{
                  t('settings.readonly')
                }}</span>
              </div>

              <div class="settings-ledger-row">
                <span class="settings-ledger-label">{{
                  t('settings.loginPassword')
                }}</span>
                <span
                  class="settings-ledger-value font-mono tracking-[0.2em]"
                  >{{
                    auth.user?.has_password
                      ? '••••••••••'
                      : t('settings.disabled')
                  }}</span
                >
                <ConsoleButton
                  variant="ghost"
                  size="sm"
                  class="settings-row-action"
                  @click="openPassword"
                >
                  {{
                    auth.user?.has_password
                      ? t('settings.changePassword')
                      : t('common.edit')
                  }}
                </ConsoleButton>
              </div>

              <div class="settings-ledger-row">
                <span class="settings-ledger-label">{{
                  t('settings.loginEmail')
                }}</span>
                <span class="settings-ledger-value break-all">{{
                  emailBinding?.account || t('settings.unbound')
                }}</span>
                <ConsoleButton
                  variant="ghost"
                  size="sm"
                  class="settings-row-action"
                  @click="
                    openBinding(
                      emailBinding ?? { id: 'email', bound: false, account: '' }
                    )
                  "
                >
                  {{
                    emailBinding?.bound
                      ? t('settings.rebind')
                      : t('settings.bind')
                  }}
                </ConsoleButton>
              </div>
            </div>
          </section>

          <section class="border-t border-[var(--border-subtle)]">
            <SettingsSectionHeading
              index="02"
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
            <div
              class="grid border-t border-[var(--border-subtle)] sm:grid-cols-2"
            >
              <div
                v-for="(binding, index) in bindingItems"
                :key="binding.id"
                class="flex min-w-0 items-center justify-between gap-3 border-b border-[var(--border-subtle)] px-5 py-4 sm:px-6"
                :class="
                  index === bindingItems.length - 1 && index % 2 === 0
                    ? 'sm:col-span-2'
                    : { 'sm:border-r': index % 2 === 0 }
                "
              >
                <div class="flex min-w-0 items-center gap-3">
                  <span class="settings-icon-tile size-9">
                    <component
                      :is="binding.icon"
                      :size="17"
                      aria-hidden="true"
                    />
                  </span>
                  <div class="min-w-0">
                    <p
                      class="truncate text-sm font-semibold text-[var(--text-primary)]"
                    >
                      {{ binding.label }}
                    </p>
                    <p class="truncate text-xs text-[var(--text-tertiary)]">
                      {{
                        binding.bound ? binding.account : t('settings.unbound')
                      }}
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
        </ConsoleCard>

        <!-- 02 活跃会话管理 -->
        <LoginSessionsPanel />

        <!-- 03 个人访问凭证 (PAT) -->
        <ConsoleCard v-if="auth.user" :padded="false" class="overflow-hidden">
          <PersonalAccessTokensPanel :user-id="auth.user.id" />
        </ConsoleCard>

        <!-- 04 账户操作 (危险区域) -->
        <ConsoleCard
          :padded="false"
          class="overflow-hidden border-[var(--status-danger-soft)]"
        >
          <SettingsSectionHeading
            index="03"
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
              @click="deleteOpen = true"
            >
              <Trash2 :size="16" aria-hidden="true" />
              {{ t('settings.deleteAccount') }}
            </ConsoleButton>
          </div>
        </ConsoleCard>
      </div>

      <!-- 右侧栏：粘性常驻的安全验证与隐私看板 -->
      <aside class="min-w-0 space-y-6 xl:sticky xl:top-6">
        <!-- 安全验证卡片：Passkey + 2FA -->
        <ConsoleCard :padded="false" class="overflow-hidden">
          <header
            class="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4 sm:px-6"
          >
            <div class="flex items-center gap-2.5">
              <ShieldCheck
                :size="18"
                class="text-[var(--accent-text)]"
                aria-hidden="true"
              />
              <h3 class="text-base font-semibold text-[var(--text-primary)]">
                {{ t('security.page.signInVerification') }}
              </h3>
            </div>
            <span
              class="hidden font-mono text-[10px] uppercase tracking-wider text-[var(--text-tertiary)] sm:inline"
              >{{ t('security.page.eyebrowVerify') }}</span
            >
          </header>

          <!-- Passkey 子项 -->
          <div class="p-5 sm:p-6">
            <div class="flex items-start gap-3">
              <span class="settings-icon-tile">
                <KeyRound :size="18" aria-hidden="true" />
              </span>
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="font-semibold text-[var(--text-primary)]">
                    {{ t('settings.passkeyAuth') }}
                  </p>
                  <StatusChip :tone="statusTone(passkeyEnabled)">
                    {{ statusLabel(passkeyEnabled) }}
                  </StatusChip>
                </div>
                <p
                  v-if="passkeyEnabled !== null"
                  class="mt-1 text-xs text-[var(--text-tertiary)]"
                >
                  {{ t('settings.lastUsed') }}：{{ passkeyLastUsedLabel }}
                </p>
              </div>
            </div>
            <div class="mt-4">
              <ConsoleButton
                :variant="passkeyEnabled ? 'secondary' : 'primary'"
                size="sm"
                block
                :disabled="passkeyEnabled === null"
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

          <!-- 两步验证 子项 -->
          <div class="border-t border-[var(--border-subtle)] p-5 sm:p-6">
            <div class="flex items-start gap-3">
              <span class="settings-icon-tile">
                <ShieldCheck :size="18" aria-hidden="true" />
              </span>
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="font-semibold text-[var(--text-primary)]">
                    {{ t('settings.twoFA') }}
                  </p>
                  <StatusChip :tone="statusTone(twoFAEnabled)">
                    {{ statusLabel(twoFAEnabled) }}
                  </StatusChip>
                </div>
                <p
                  v-if="twoFAEnabled !== null"
                  class="mt-1 text-xs text-[var(--text-tertiary)]"
                >
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
            <div v-if="twoFAEnabled !== null" class="mt-4">
              <div v-if="twoFAEnabled" class="grid gap-2 sm:grid-cols-2">
                <ConsoleButton
                  variant="secondary"
                  size="sm"
                  @click="openTwoFAAction('regenerate')"
                >
                  <RefreshCw :size="14" aria-hidden="true" />
                  {{ t('settings.regenerateBackupCodes') }}
                </ConsoleButton>
                <ConsoleButton
                  variant="danger"
                  size="sm"
                  @click="openTwoFAAction('disable')"
                >
                  <AlertTriangle :size="14" aria-hidden="true" />
                  {{ t('settings.disableTwoFA') }}
                </ConsoleButton>
              </div>
              <ConsoleButton v-else size="sm" block @click="openTwoFASetup">
                {{ t('settings.enableTwoFA') }}
              </ConsoleButton>
            </div>
          </div>
        </ConsoleCard>

        <!-- IP 记录与通知只由账号设置的偏好面板写入，这里只给入口 -->
        <ConsoleCard :padded="false" class="overflow-hidden">
          <div class="flex items-start gap-3 p-5 sm:p-6">
            <span class="settings-icon-tile">
              <Settings2 :size="18" aria-hidden="true" />
            </span>
            <div class="min-w-0 flex-1">
              <h3 class="text-base font-semibold text-[var(--text-primary)]">
                {{ t('security.page.preferencesTitle') }}
              </h3>
              <p class="mt-1 text-xs text-[var(--text-tertiary)]">
                {{ t('security.page.preferencesHint') }}
              </p>
              <ConsoleButton
                variant="secondary"
                size="sm"
                class="mt-4"
                @click="router.push({ name: 'settings' })"
              >
                {{ t('security.page.openPreferences') }}
              </ConsoleButton>
            </div>
          </div>
        </ConsoleCard>
      </aside>
    </div>

    <!-- 弹窗列表 -->
    <!-- 编辑显示名称 -->
    <ConsoleModal
      :open="profileOpen"
      :title="t('settings.editDisplayName')"
      size="sm"
      @close="profileOpen = false"
    >
      <FormField :label="t('settings.displayName')">
        <TextInput
          v-model="displayName"
          name="display-name"
          maxlength="50"
          autocomplete="name"
        />
      </FormField>
      <template #footer>
        <ConsoleButton
          block
          size="lg"
          :loading="savingProfile"
          :disabled="!displayName.trim()"
          @click="saveProfile"
        >
          {{ t('settings.saveProfile') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>

    <!-- 修改密码 -->
    <ConsoleModal
      :open="passwordOpen"
      :title="t('settings.changePassword')"
      size="sm"
      @close="passwordOpen = false"
    >
      <div class="space-y-4">
        <FormField
          v-if="auth.user?.has_password"
          :label="t('settings.oldPassword')"
        >
          <TextInput
            v-model="oldPassword"
            type="password"
            autocomplete="current-password"
          />
        </FormField>
        <FormField
          :label="t('auth.newPassword')"
          :hint="t('auth.passwordHint')"
        >
          <TextInput
            v-model="newPassword"
            type="password"
            autocomplete="new-password"
          />
        </FormField>
        <PasswordStrengthMeter :password="newPassword" />
      </div>
      <template #footer>
        <ConsoleButton
          block
          size="lg"
          :loading="savingPassword"
          :disabled="
            (auth.user?.has_password && !oldPassword) ||
            [...newPassword].length < 8 ||
            [...newPassword].length > 128
          "
          @click="changePassword"
        >
          {{ t('common.save') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>

    <!-- 注册 Passkey -->
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

    <!-- 移除 Passkey 确认 -->
    <ConfirmDialog
      :open="passkeyRemoveOpen"
      :title="t('settings.removePasskey')"
      :message="t('settings.removePasskeyConfirm')"
      @confirm="removePasskey"
      @cancel="passkeyRemoveOpen = false"
    />

    <!-- 配置 2FA -->
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

    <!-- 查看备用码 -->
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
          <ConsoleButton
            variant="secondary"
            size="lg"
            @click="copyBackupCodes"
            >{{ t('common.copy') }}</ConsoleButton
          >
          <ConsoleButton size="lg" @click="backupCodesOpen = false">{{
            t('common.close')
          }}</ConsoleButton>
        </div>
      </template>
    </ConsoleModal>

    <!-- 绑定第三方提供商 / 邮箱 -->
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
            {{
              emailSending ? t('common.loading') : t('settings.sendEmailCode')
            }}
          </ConsoleButton>
        </FormField>
      </div>
      <FormField v-if="bindTarget === 'wechat'" :label="t('security.code')">
        <TextInput v-model="bindingCode" maxlength="128" />
      </FormField>
      <FormField
        v-if="bindTarget === 'email' && emailOldRequired"
        :label="t('security.oldEmailCode')"
      >
        <TextInput
          v-model="emailOldCode"
          autocomplete="one-time-code"
          maxlength="6"
        />
      </FormField>
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

    <!-- 解绑确认 -->
    <ConfirmDialog
      :open="unbindTarget !== null"
      :title="t('settings.unbindProvider', { provider: currentUnbindLabel })"
      :message="t('settings.unbindConfirm')"
      @confirm="confirmUnbind"
      @cancel="unbindTarget = null"
    />

    <!-- 注销账户确认 -->
    <ConsoleModal
      :open="deleteOpen"
      :title="t('settings.deleteAccount')"
      size="sm"
      @close="deleteOpen = false"
    >
      <div class="space-y-4">
        <p class="text-sm text-[var(--status-danger-text)]">
          {{ t('settings.deleteAccountConfirm') }}
        </p>
        <FormField
          :label="
            t('settings.typeNameToConfirm', { name: auth.user?.username ?? '' })
          "
        >
          <TextInput
            v-model="deleteConfirmText"
            :placeholder="auth.user?.username"
          />
        </FormField>
      </div>
      <template #footer>
        <ConsoleButton
          variant="danger"
          block
          size="lg"
          :loading="deleting"
          :disabled="deleteConfirmText !== auth.user?.username"
          @click="deleteAccount"
        >
          {{ t('settings.deleteAccount') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>

    <SecurityProofDialog ref="proofDialog" />
  </div>
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

/* ConsoleButton supplies the 44px mobile / 32px desktop height scale. */
.settings-row-action {
  justify-self: end;
  color: var(--accent-text);
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
