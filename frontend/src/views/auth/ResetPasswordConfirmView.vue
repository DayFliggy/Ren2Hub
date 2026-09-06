<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Copy } from 'lucide-vue-next'
import { useClipboard } from '@vueuse/core'
import { authApi } from '@/api/auth'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import TextInput from '@/components/common/TextInput.vue'
import FormField from '@/components/common/FormField.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { copy, copied } = useClipboard()
const email = typeof route.query.email === 'string' ? route.query.email : ''
const token = typeof route.query.token === 'string' ? route.query.token : ''
const valid = computed(() => Boolean(email && token))
const password = ref('')
const loading = ref(false)
const error = ref('')

async function confirm() {
  if (!valid.value || loading.value || password.value) return
  loading.value = true
  error.value = ''
  try {
    password.value = await authApi.confirmPasswordReset(email, token)
    // Remove the one-time token from history once it has been consumed.
    await router.replace({ name: 'reset-confirm' })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthLayout>
    <h1 class="text-3xl font-bold text-[var(--text-primary)]">
      {{ t('auth.resetTitle') }}
    </h1>
    <ErrorBanner
      v-if="error || (!valid && !password)"
      class="mt-6"
      :message="error || t('auth.resetInvalid')"
      @retry="confirm"
    />
    <div v-if="password" class="mt-6 space-y-4">
      <p>{{ t('auth.resetCompleted') }}</p>
      <FormField :label="t('auth.newPassword')"
        ><TextInput v-model="password" readonly
      /></FormField>
      <ConsoleButton variant="secondary" @click="copy(password)"
        ><Copy :size="16" />{{
          copied ? t('common.copied') : t('common.copy')
        }}</ConsoleButton
      >
    </div>
    <ConsoleButton
      v-else
      class="mt-6"
      :loading="loading"
      :disabled="!valid"
      @click="confirm"
      >{{ t('auth.resetConfirm') }}</ConsoleButton
    >
    <RouterLink
      :to="{ name: 'sign-in' }"
      class="mt-6 block text-[var(--accent-text)]"
      >{{ t('auth.goSignIn') }}</RouterLink
    >
  </AuthLayout>
</template>
