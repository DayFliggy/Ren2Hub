<script setup lang="ts">
import { Eye, EyeOff } from 'lucide-vue-next'
import { computed, onBeforeUnmount, ref } from 'vue'
import { api } from '@/api/client'
import { parseAuthBundle, parseTwoFactorChallenge } from '@/api/authSession'
import { completeOAuth, oauthProviders } from '@/api/oauth'
import { assertPasskey, type AssertionOptions } from '@/utils/passkey'
import type { TwoFactorChallenge } from '@/types/auth'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import AffiliateAttributionField from '@/components/auth/AffiliateAttributionField.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { ApiError } from '@/api/types'
import { sanitizeRedirect } from '@/router'
import { useAppStore } from '@/stores'
import {
  clearAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

const oauthAbort = new AbortController()
onBeforeUnmount(() => oauthAbort.abort())
const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const app = useAppStore()
const toast = useToast()

const challengeMethods = ref<TwoFactorChallenge['methods']>([])
const rpID = ref('')
const affiliateCode = ref('')
const affiliateConfirmed = ref(false)
const affiliateError = ref('')
const providers = computed(() => oauthProviders(app.status))
const passwordEnabled = computed(
  () =>
    app.status.password_login_enabled !== false &&
    app.isFeatureEnabled('password_login', 'live')
)
const passkeyEnabled = computed(() => app.isFeatureEnabled('passkey'))
const username = ref('')
const password = ref('')
const twoFactorCode = ref('')
const twoFactorFlowToken = ref('')
const twoFactorExpiresAt = ref(0)
const showPassword = ref(false)
const loading = ref(false)
const turnstileToken = ref('')
const turnstileUnavailable = ref(false)
const turnstileWidget = ref<InstanceType<typeof TurnstileWidget> | null>(null)

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

function resetTwoFactor() {
  challengeMethods.value = []
  twoFactorFlowToken.value = ''
  twoFactorCode.value = ''
  twoFactorExpiresAt.value = 0
  turnstileToken.value = ''
}

async function submit() {
  if (loading.value) return
  loading.value = true
  try {
    if (twoFactorFlowToken.value) {
      if (Date.now() >= twoFactorExpiresAt.value * 1000) {
        resetTwoFactor()
        toast.error(t('auth.twoFactorExpired'))
        return
      }
      await auth.verifyTwoFactor(
        twoFactorFlowToken.value,
        twoFactorCode.value.trim()
      )
    } else {
      await app.initialize()
      if (!turnstileReady()) return
      const submittedTurnstileToken = turnstileToken.value
      if (app.turnstileEnabled) {
        turnstileToken.value = ''
        turnstileWidget.value?.reset()
      }
      const challenge = await auth.login(
        username.value,
        password.value,
        submittedTurnstileToken || undefined
      )
      if (challenge) {
        challengeMethods.value = challenge.methods
        twoFactorFlowToken.value = challenge.flow_token
        twoFactorExpiresAt.value = challenge.expires_at
        return
      }
    }
    clearAffiliateAttribution()
    toast.success(t('auth.loginSuccess'))
    const redirect = sanitizeRedirect(route.query.redirect)
    await router.push(redirect || { name: 'dashboard' })
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
async function finishAlternateLogin(value: unknown) {
  const challenge = parseTwoFactorChallenge(value)
  if (challenge) {
    challengeMethods.value = challenge.methods
    twoFactorFlowToken.value = challenge.flow_token
    twoFactorExpiresAt.value = challenge.expires_at
    return
  }
  const bundle = parseAuthBundle(value)
  if (!bundle) throw new Error(t('security.invalidProof'))
  auth.acceptLogin(bundle)
  clearAffiliateAttribution()
  await router.push(
    sanitizeRedirect(route.query.redirect) || { name: 'dashboard' }
  )
}
async function signInPasskey() {
  if (loading.value) return
  loading.value = true
  try {
    const verifying = Boolean(twoFactorFlowToken.value)
    // Second-factor verification continues the password login flow
    // (controller/login_verification.go); primary sign-in is discoverable.
    const prefix = verifying
      ? '/api/user/login/passkey'
      : '/api/user/passkey/login'
    const begin = await api.post<{
      options: AssertionOptions
      flow_token: string
    }>(prefix + '/begin', {
      rp_id: rpID.value,
      ...(verifying ? { flow_token: twoFactorFlowToken.value } : {}),
    })
    const credential = await assertPasskey(begin.options)
    await finishAlternateLogin(
      await api.post(
        prefix + '/finish',
        verifying
          ? {
              flow_token: twoFactorFlowToken.value,
              passkey_flow_token: begin.flow_token,
              credential,
            }
          : { flow_token: begin.flow_token, credential }
      )
    )
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
async function signInOAuth(provider: string) {
  if (loading.value) return
  loading.value = true
  if (affiliateConfirmed.value && affiliateCode.value.trim())
    storeAffiliateAttribution(affiliateCode.value)
  else clearAffiliateAttribution()
  try {
    await finishAlternateLogin(
      await completeOAuth(
        app.status,
        provider,
        'login',
        undefined,
        undefined,
        oauthAbort.signal
      )
    )
  } catch (error) {
    if (
      error instanceof ApiError &&
      ['AFFILIATE_DISABLED', 'AFFILIATE_CODE_INVALID'].includes(
        error.code ?? ''
      )
    )
      affiliateError.value = error.message
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthLayout>
    <div class="text-center">
      <h1
        class="auth-huiwen-title display-title mt-3 text-3xl font-bold text-[var(--text-primary)]"
      >
        {{ t('auth.signInTitle') }}
      </h1>
      <p class="mt-2 text-xs italic tracking-wide text-[var(--text-tertiary)]">
        {{ t('auth.cardMotto') }}
      </p>
    </div>

    <form
      v-if="passwordEnabled || twoFactorFlowToken"
      class="mt-8 sibling-space-y-4"
      @submit.prevent="submit"
    >
      <template v-if="!twoFactorFlowToken">
        <FormField :label="t('auth.username')">
          <TextInput
            v-model="username"
            :placeholder="t('auth.usernameOrEmailPlaceholder')"
            autocomplete="username"
          />
        </FormField>

        <FormField :label="t('auth.password')">
          <div class="relative">
            <TextInput
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              :placeholder="t('auth.passwordPlaceholder')"
              autocomplete="current-password"
              class="[&_input]:pr-12"
            />
            <button
              type="button"
              class="absolute right-1.5 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-lg text-[var(--text-tertiary)] transition-colors hover:bg-[var(--surface-hover)] hover:text-[var(--text-primary)] focus-ring"
              :aria-label="
                showPassword ? t('auth.hidePassword') : t('auth.showPassword')
              "
              @click="showPassword = !showPassword"
            >
              <EyeOff v-if="showPassword" :size="18" />
              <Eye v-else :size="18" />
            </button>
          </div>
        </FormField>
      </template>

      <FormField
        v-else-if="
          challengeMethods.some(
            (item) => item.method === '2fa' && item.available
          )
        "
        :label="t('auth.twoFactorCode')"
      >
        <TextInput
          v-model="twoFactorCode"
          autocomplete="one-time-code"
          maxlength="32"
          :placeholder="t('auth.twoFactorPlaceholder')"
        />
      </FormField>

      <!-- The 32px hit area overlaps the surrounding gaps through negative
           margins, so the row keeps its compact visual height. -->
      <div v-if="!twoFactorFlowToken" class="flex justify-end">
        <RouterLink
          :to="{ name: 'reset' }"
          class="-mx-2 -my-2 inline-flex min-h-8 items-center rounded-md px-2 text-xs font-medium text-[var(--accent-text)] hover:underline focus-ring"
        >
          {{ t('auth.forgot') }}
        </RouterLink>
      </div>

      <TurnstileWidget
        v-if="app.turnstileEnabled && !twoFactorFlowToken"
        ref="turnstileWidget"
        :site-key="app.turnstileSiteKey"
        @verified="turnstileToken = $event"
        @unavailable="turnstileUnavailable = true"
      />

      <ConsoleButton
        v-if="
          !twoFactorFlowToken ||
          challengeMethods.some(
            (item) => item.method === '2fa' && item.available
          )
        "
        type="submit"
        size="lg"
        block
        :loading="loading"
        :disabled="
          !twoFactorFlowToken &&
          app.turnstileEnabled &&
          (!turnstileToken || turnstileUnavailable)
        "
      >
        {{ twoFactorFlowToken ? t('auth.verifyTwoFactor') : t('auth.signIn') }}
      </ConsoleButton>
      <button
        v-if="twoFactorFlowToken"
        type="button"
        class="w-full text-center text-xs font-medium text-[var(--accent-text)] hover:underline focus-ring"
        @click="resetTwoFactor"
      >
        {{ t('auth.backToPassword') }}
      </button>
    </form>

    <AffiliateAttributionField
      v-if="providers.length && !twoFactorFlowToken"
      v-model="affiliateCode"
      v-model:confirmed="affiliateConfirmed"
      :disabled="loading"
      :error="affiliateError"
      @changed="affiliateError = ''"
    />

    <div class="mt-4 sibling-space-y-3">
      <FormField
        v-if="passkeyEnabled && (app.status.passkey_rp_ids?.length ?? 0) > 1"
        :label="t('security.passkeyDomain')"
      >
        <select
          v-model="rpID"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
        >
          <option value="">{{ t('security.automaticDomain') }}</option>
          <option v-for="id in app.status.passkey_rp_ids" :key="id" :value="id">
            {{ id }}
          </option>
        </select>
      </FormField>
      <ConsoleButton
        v-if="
          passkeyEnabled &&
          (!twoFactorFlowToken ||
            challengeMethods.some(
              (item) => item.method === 'passkey' && item.available
            ))
        "
        block
        variant="secondary"
        :disabled="loading"
        @click="signInPasskey"
        >{{ t('security.signInPasskey') }}</ConsoleButton
      >
      <template v-if="!twoFactorFlowToken"
        ><ConsoleButton
          v-for="provider in providers"
          :key="provider.slug"
          block
          variant="secondary"
          :disabled="loading"
          @click="signInOAuth(provider.slug)"
          >{{
            t('security.signInProvider', { provider: provider.name })
          }}</ConsoleButton
        ></template
      >
    </div>
    <p
      v-if="app.registerEnabled && !twoFactorFlowToken"
      class="mt-6 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('auth.noAccount') }}
      <RouterLink
        :to="{ name: 'sign-up' }"
        class="font-semibold text-[var(--accent-text)] hover:underline"
      >
        {{ t('auth.goSignUp') }}
      </RouterLink>
    </p>
  </AuthLayout>
</template>

<style scoped>
.auth-huiwen-title {
  font-family: 'Ren2AuthHuiwen', var(--font-display);
  font-weight: 400;
  letter-spacing: 0;
  font-synthesis: none;
}
</style>
