import { useI18n } from 'vue-i18n'
import { isLocaleCode } from '@/i18n'
import { translateServerText } from '@/i18n/serverMessages'

/**
 * Translates fixed backend English text, such as permission catalog labels, in
 * the current locale. Text without a catalog entry is shown as written.
 */
export function useServerText() {
  const { locale } = useI18n({ useScope: 'global' })
  return (text: string) =>
    isLocaleCode(locale.value) ? translateServerText(locale.value, text) : text
}
