<script setup lang="ts">
import { computed } from 'vue'
import { I18nT, useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'

const agreed = defineModel<boolean>({ required: true })
defineProps<{ disabled?: boolean }>()
const app = useAppStore()
const { t } = useI18n()
// One sentence per document combination keeps word order and spacing
// correct in each language.
const keypath = computed(() => {
  if (app.userAgreementEnabled && app.privacyPolicyEnabled)
    return 'auth.legalConsentBoth'
  return app.userAgreementEnabled
    ? 'auth.legalConsentAgreement'
    : 'auth.legalConsentPrivacy'
})
const linkClass =
  'rounded-sm font-medium text-[var(--accent-text)] hover:underline focus-ring'
</script>

<template>
  <label
    v-if="app.userAgreementEnabled || app.privacyPolicyEnabled"
    class="flex items-start gap-2.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-hover)] p-3 text-xs leading-5 text-[var(--text-secondary)]"
    data-legal-consent
  >
    <input
      v-model="agreed"
      type="checkbox"
      class="mt-0.5 h-4 w-4 shrink-0 accent-[var(--accent)]"
      :disabled="disabled"
    />
    <I18nT :keypath="keypath" tag="span" scope="global">
      <template #agreement>
        <RouterLink
          :to="{ name: 'user-agreement' }"
          target="_blank"
          rel="noopener noreferrer"
          :class="linkClass"
          >{{ t('auth.userAgreement') }}</RouterLink
        >
      </template>
      <template #privacy>
        <RouterLink
          :to="{ name: 'privacy-policy' }"
          target="_blank"
          rel="noopener noreferrer"
          :class="linkClass"
          >{{ t('auth.privacyPolicy') }}</RouterLink
        >
      </template>
    </I18nT>
  </label>
</template>
