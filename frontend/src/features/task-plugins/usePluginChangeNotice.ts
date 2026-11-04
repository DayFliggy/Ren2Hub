import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import type { ModelPricingIssue } from '@/api/pricingConfig'

/** A saved plugin change remains successful when its prices need attention. */
export function usePluginChangeNotice() {
  const { t } = useI18n()
  const toast = useToast()
  return (title: string, issues?: ModelPricingIssue[] | null) => {
    if (!issues?.length) {
      toast.success(title)
      return
    }
    const details = issues
      .map(
        (issue) =>
          `${issue.model_name} (${issue.plugin_name || issue.plugin_key}): ${t(`taskPlugins.pricingIssueReasons.${issue.reason}`)}`
      )
      .join('; ')
    toast.warning(`${title}. ${t('taskPlugins.pricingIssuesHint')} ${details}`)
  }
}
