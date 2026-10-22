import { useI18n } from 'vue-i18n'

import type { SecurityChange } from '@/api/accountSecurity'
import { useToast } from './useToast'

/** The change itself has already succeeded when the backend reports a failed
 * security notification email, so callers show this right after the change and
 * before any follow-up refresh that could fail on its own. */
export function useSecurityNotice() {
  const { t } = useI18n()
  const toast = useToast()
  return (result: SecurityChange): void => {
    if (result.notification_warning)
      toast.warning(t('settings.securityNotificationFailed'))
  }
}
