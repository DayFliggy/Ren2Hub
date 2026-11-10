import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import { useAppStore } from '@/stores/app'

// Upstream web/src/features/auth requires agreement before any sign-in or
// sign-up method once a user agreement or privacy policy is published.
export function useLegalConsent() {
  const app = useAppStore()
  const { t } = useI18n()
  const toast = useToast()
  const agreed = ref(false)
  const required = computed(
    () => app.userAgreementEnabled || app.privacyPolicyEnabled
  )
  const blocked = computed(() => required.value && !agreed.value)

  function ensureAgreed(): boolean {
    if (!blocked.value) return true
    toast.error(t('auth.legalConsentRequired'))
    return false
  }

  return { agreed, required, blocked, ensureAgreed }
}
