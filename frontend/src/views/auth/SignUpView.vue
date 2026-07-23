<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import PasswordStrengthMeter from '@/components/auth/PasswordStrengthMeter.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { authApi } from '@/api/auth'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const { t } = useI18n()
const router = useRouter()
const toast = useToast()

const username = ref('')
const email = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)

async function submit() {
  if (password.value !== confirm.value) {
    toast.error(t('auth.mismatch'))
    return
  }
  loading.value = true
  try {
    await authApi.register({
      username: username.value,
      email: email.value,
      password: password.value,
    })
    toast.success(t('toast.registerSuccess'))
    await router.push({ name: 'sign-in' })
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthLayout>
    <h1 class="text-3xl font-bold text-[var(--text-primary)]">
      {{ t('auth.signUpTitle') }}
    </h1>
    <p class="mt-2 text-sm text-[var(--text-tertiary)]">
      {{ t('auth.signUpSubtitle') }}
    </p>

    <form class="mt-8 space-y-4" @submit.prevent="submit">
      <FormField :label="t('auth.username')">
        <TextInput
          v-model="username"
          :placeholder="t('auth.username')"
          autocomplete="username"
        />
      </FormField>
      <FormField :label="t('auth.email')">
        <TextInput
          v-model="email"
          type="email"
          :placeholder="t('auth.email')"
          autocomplete="email"
        />
      </FormField>
      <FormField :label="t('auth.newPassword')" :hint="t('auth.passwordHint')">
        <TextInput
          v-model="password"
          type="password"
          :placeholder="t('auth.newPassword')"
          autocomplete="new-password"
        />
      </FormField>
      <PasswordStrengthMeter :password="password" />
      <FormField :label="t('auth.confirmPassword')">
        <TextInput
          v-model="confirm"
          type="password"
          :placeholder="t('auth.confirmPassword')"
          autocomplete="new-password"
        />
      </FormField>

      <ConsoleButton type="submit" size="lg" block :loading="loading">
        {{ t('auth.continue') }}
      </ConsoleButton>
    </form>

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
