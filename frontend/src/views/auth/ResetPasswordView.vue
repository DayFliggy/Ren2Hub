<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { authApi } from '@/api/auth'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const toast = useToast()
const app = useAppStore()

const email = ref('')
const loading = ref(false)
const sent = ref(false)
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

async function submit() {
  if (loading.value || sent.value) return
  loading.value = true
  try {
    await app.initialize()
    if (!turnstileReady()) return
    // The token is single-use, so the widget is reset after every attempt.
    const token = turnstileToken.value || undefined
    turnstileToken.value = ''
    await authApi.resetPassword(email.value.trim(), token)
    sent.value = true
    toast.success(t('auth.resetSent'))
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loading.value = false
    if (app.turnstileEnabled) turnstileWidget.value?.reset()
  }
}
</script>

<template>
  <AuthLayout>
    <RouterLink
      :to="{ name: 'sign-in' }"
      class="mb-6 inline-flex min-h-6 items-center gap-1.5 text-sm text-[var(--text-tertiary)] transition-colors hover:text-[var(--text-primary)]"
    >
      ← {{ t('common.back') }}
    </RouterLink>
    <h1 class="display-title text-3xl font-bold text-[var(--text-primary)]">
      {{ t('auth.resetTitle') }}
    </h1>
    <p class="mt-2 text-sm text-[var(--text-tertiary)]">
      {{ t('auth.resetSubtitle') }}
    </p>

    <form class="mt-8 sibling-space-y-4" @submit.prevent="submit">
      <FormField :label="t('auth.email')">
        <TextInput
          v-model="email"
          type="email"
          :placeholder="t('auth.email')"
          autocomplete="email"
        />
      </FormField>
      <TurnstileWidget
        v-if="app.turnstileEnabled && !sent"
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
        :disabled="
          sent ||
          (app.turnstileEnabled && (!turnstileToken || turnstileUnavailable))
        "
      >
        {{ sent ? `✓ ${t('auth.resetSent')}` : t('auth.sendReset') }}
      </ConsoleButton>
    </form>
  </AuthLayout>
</template>
