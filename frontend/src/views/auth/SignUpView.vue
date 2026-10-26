<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { parseAuthBundle, parseTwoFactorChallenge } from '@/api/authSession'
import { completeOAuth, oauthProviders } from '@/api/oauth'
import AffiliateAttributionField from '@/components/auth/AffiliateAttributionField.vue'
import PasswordStrengthMeter from '@/components/auth/PasswordStrengthMeter.vue'
import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { authApi } from '@/api/auth'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import {
  clearAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

const oauthAbort = new AbortController()
onBeforeUnmount(() => oauthAbort.abort())
const { t } = useI18n()
const router = useRouter()
const toast = useToast()
const app = useAppStore()
const auth = useAuthStore()

const username = ref('')
const email = ref('')
const password = ref('')
const confirm = ref('')
const affiliateCode = ref('')
const affiliateConfirmed = ref(false)
const verificationCode = ref('')
const verificationCodeId = useId()
const verificationSending = ref(false)
const loading = ref(false)
const turnstileToken = ref('')
const turnstileUnavailable = ref(false)
const turnstileWidget = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const errors = reactive({
  username: '',
  email: '',
  password: '',
  confirm: '',
  affiliate: '',
})
const busy = computed(() => loading.value || verificationSending.value)
const providers = computed(() => oauthProviders(app.status))
// Native Register reads email and code only when email verification is on.
const emailRequired = computed(() => app.status.email_verification === true)
const turnstileSatisfied = computed(
  () =>
    !app.turnstileEnabled ||
    (Boolean(turnstileToken.value) && !turnstileUnavailable.value)
)

function turnstileReady(): boolean {
  if (!app.turnstileEnabled) return true
  if (!app.turnstileSiteKey || turnstileUnavailable.value) {
    toast.error(t('common.turnstileUnavailable'))
    return false
  }
  if (!turnstileToken.value) {
    toast.error(t('common.turnstileRequired'))
    return false
  }
  return true
}

// A Turnstile token is single-use: each request takes the current one and the
// widget is reset when the request ends, whatever its outcome.
function takeTurnstileToken(): string | undefined {
  const token = turnstileToken.value
  turnstileToken.value = ''
  return token || undefined
}

function resetTurnstile() {
  if (app.turnstileEnabled) turnstileWidget.value?.reset()
}

async function sendVerification() {
  if (busy.value || !turnstileReady()) return
  verificationSending.value = true
  try {
    await authApi.sendVerification(email.value.trim(), takeTurnstileToken())
    toast.success(t('common.success'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    verificationSending.value = false
    resetTurnstile()
  }
}

// Mirrors the backend's minimal rules so obvious mistakes fail before the
// request instead of as an opaque server error.
function validate(): boolean {
  errors.username = ''
  errors.email = ''
  errors.password = ''
  errors.confirm = ''
  errors.affiliate = ''
  let valid = true
  if (!username.value.trim()) {
    errors.username = t('auth.usernameRequired')
    valid = false
  }
  if (emailRequired.value && !/^\S+@\S+\.\S+$/.test(email.value.trim())) {
    errors.email = t('auth.emailInvalid')
    valid = false
  }
  if ([...password.value].length < 8 || [...password.value].length > 128) {
    errors.password = t('security.passwordLength')
    valid = false
  }
  if (password.value !== confirm.value) {
    errors.confirm = t('auth.mismatch')
    valid = false
  }

  return valid
}

async function submit() {
  if (busy.value || !validate()) return
  loading.value = true
  const selectedAffiliateCode = affiliateConfirmed.value
    ? affiliateCode.value.trim()
    : ''
  if (selectedAffiliateCode) storeAffiliateAttribution(selectedAffiliateCode)
  else clearAffiliateAttribution()
  try {
    await app.initialize()
    if (!turnstileReady()) return
    await authApi.register(
      {
        username: username.value.trim(),
        password: password.value,
        aff_code: selectedAffiliateCode || undefined,
        ...(emailRequired.value
          ? {
              email: email.value.trim(),
              verification_code: verificationCode.value || undefined,
            }
          : {}),
      },
      takeTurnstileToken()
    )
    clearAffiliateAttribution()
    toast.success(t('auth.registerSuccess'))
    await router.push({ name: 'sign-in' })
  } catch (error) {
    if (
      error instanceof ApiError &&
      ['AFFILIATE_DISABLED', 'AFFILIATE_CODE_INVALID'].includes(
        error.code ?? ''
      )
    )
      errors.affiliate = error.message
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loading.value = false
    resetTurnstile()
  }
}

async function signUpWithOAuth(provider: string) {
  if (busy.value) return
  loading.value = true
  if (affiliateConfirmed.value && affiliateCode.value.trim())
    storeAffiliateAttribution(affiliateCode.value)
  else clearAffiliateAttribution()
  try {
    await app.initialize()
    const value = await completeOAuth(
      app.status,
      provider,
      'login',
      undefined,
      undefined,
      oauthAbort.signal
    )
    if (parseTwoFactorChallenge(value)) {
      toast.error(t('auth.oauthRequiresSignIn'))
      return
    }
    const bundle = parseAuthBundle(value)
    if (!bundle) throw new Error(t('security.invalidProof'))
    auth.acceptLogin(bundle)
    clearAffiliateAttribution()
    toast.success(t('auth.loginSuccess'))
    await router.push({ name: 'dashboard' })
  } catch (error) {
    if (
      error instanceof ApiError &&
      ['AFFILIATE_DISABLED', 'AFFILIATE_CODE_INVALID'].includes(
        error.code ?? ''
      )
    )
      errors.affiliate = error.message
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
onMounted(() => app.initialize())
</script>

<template>
  <AuthLayout>
    <div class="text-center">
      <p class="text-xs italic tracking-wide text-[var(--text-tertiary)]">
        {{ t('auth.cardMotto') }}
      </p>
      <h1
        class="display-title mt-3 text-3xl font-bold text-[var(--text-primary)]"
      >
        {{ t('auth.signUpTitle') }}
      </h1>
      <p class="mt-2 text-sm text-[var(--text-tertiary)]">
        {{ t('auth.signUpSubtitle') }}
      </p>
    </div>

    <form
      v-if="app.passwordRegisterEnabled"
      class="mt-8 sibling-space-y-4"
      @submit.prevent="submit"
    >
      <FormField :label="t('auth.username')">
        <TextInput
          v-model="username"
          :placeholder="t('auth.username')"
          autocomplete="username"
        />
        <span
          v-if="errors.username"
          class="mt-1.5 block text-xs text-[var(--status-danger-text)]"
        >
          {{ errors.username }}
        </span>
      </FormField>
      <FormField v-if="emailRequired" :label="t('auth.email')">
        <TextInput
          v-model="email"
          type="email"
          :placeholder="t('auth.email')"
          autocomplete="email"
        />
        <span
          v-if="errors.email"
          class="mt-1.5 block text-xs text-[var(--status-danger-text)]"
        >
          {{ errors.email }}
        </span>
      </FormField>
      <div v-if="emailRequired">
        <label
          :for="verificationCodeId"
          class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
          >{{ t('auth.verificationCode') }}</label
        >
        <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
          <TextInput
            :id="verificationCodeId"
            v-model="verificationCode"
            class="min-w-0 flex-1"
            inputmode="numeric"
            autocomplete="one-time-code"
            :placeholder="t('auth.verificationCode')"
          />
          <ConsoleButton
            type="button"
            variant="secondary"
            class="shrink-0"
            :disabled="busy || !email.trim() || !turnstileSatisfied"
            :loading="verificationSending"
            @click="sendVerification"
            >{{ t('auth.sendCode') }}</ConsoleButton
          >
        </div>
      </div>
      <FormField :label="t('auth.newPassword')" :hint="t('auth.passwordHint')">
        <TextInput
          v-model="password"
          type="password"
          :placeholder="t('auth.newPassword')"
          autocomplete="new-password"
        />
        <span
          v-if="errors.password"
          class="mt-1.5 block text-xs text-[var(--status-danger-text)]"
        >
          {{ errors.password }}
        </span>
      </FormField>
      <PasswordStrengthMeter :password="password" />
      <FormField :label="t('auth.confirmPassword')">
        <TextInput
          v-model="confirm"
          type="password"
          :placeholder="t('auth.confirmPassword')"
          autocomplete="new-password"
        />
        <span
          v-if="errors.confirm"
          class="mt-1.5 block text-xs text-[var(--status-danger-text)]"
        >
          {{ errors.confirm }}
        </span>
      </FormField>
      <AffiliateAttributionField
        v-model="affiliateCode"
        v-model:confirmed="affiliateConfirmed"
        :error="errors.affiliate"
        :disabled="busy"
        @changed="errors.affiliate = ''"
      />
      <TurnstileWidget
        v-if="app.turnstileEnabled"
        ref="turnstileWidget"
        :site-key="app.turnstileSiteKey"
        @verified="turnstileToken = $event"
        @unavailable="turnstileUnavailable = true"
      />

      <ConsoleButton
        type="submit"
        size="lg"
        block
        :loading="loading"
        :disabled="verificationSending || !turnstileSatisfied"
      >
        {{ t('auth.continue') }}
      </ConsoleButton>
    </form>

    <!-- OAuth-only sites have no form, but the code still applies to OAuth sign-up. -->
    <AffiliateAttributionField
      v-if="!app.passwordRegisterEnabled && providers.length"
      v-model="affiliateCode"
      v-model:confirmed="affiliateConfirmed"
      :error="errors.affiliate"
      :disabled="busy"
      @changed="errors.affiliate = ''"
    />

    <p
      v-if="!app.passwordRegisterEnabled && providers.length"
      class="mt-8 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('auth.passwordRegistrationDisabled') }}
    </p>
    <p
      v-else-if="app.passwordRegisterEnabled && providers.length"
      class="mt-5 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('auth.otherWaysToRegister') }}
    </p>
    <p
      v-else-if="!app.passwordRegisterEnabled"
      class="mt-8 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('auth.noRegistrationMethods') }}
    </p>

    <div v-if="providers.length" class="mt-4 sibling-space-y-2">
      <ConsoleButton
        v-for="provider in providers"
        :key="provider.slug"
        block
        variant="secondary"
        :disabled="busy"
        :loading="loading"
        @click="signUpWithOAuth(provider.slug)"
        >{{
          t('auth.continueWithProvider', { provider: provider.name })
        }}</ConsoleButton
      >
    </div>

    <p class="mt-6 text-center text-sm text-[var(--text-tertiary)]">
      {{ t('auth.hasAccount') }}
      <RouterLink
        :to="{ name: 'sign-in' }"
        class="font-semibold text-[var(--accent-text)] hover:underline"
      >
        {{ t('auth.goSignIn') }}
      </RouterLink>
    </p>
  </AuthLayout>
</template>
