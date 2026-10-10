import { watch } from 'vue'

import { useToast } from '@/composables/useToast'
import { getLocale, hasStoredLocale, setLocale, type LocaleCode } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

// The backend translates emails, notifications and AI-client errors in the
// language saved on the account, so the account follows the console: a device
// without its own choice adopts the account language on sign-in, and a choice
// made while signed in is saved to the account.

/** The raw language value saved in the account setting JSON. */
export function savedAccountLanguage(setting: string | undefined): string {
  if (!setting) return ''
  try {
    const value: unknown = JSON.parse(setting)
    if (!value || typeof value !== 'object') return ''
    const language = (value as Record<string, unknown>).language
    return typeof language === 'string' ? language : ''
  } catch {
    // An unreadable setting holds no language to adopt.
    return ''
  }
}

/**
 * The console locale for a saved account language. Traditional Chinese has
 * no console locale and shows Simplified Chinese; other languages are not
 * adopted.
 */
export function accountLocale(setting: string | undefined): LocaleCode | null {
  const language = savedAccountLanguage(setting).trim().toLowerCase()
  if (language.startsWith('zh')) return 'zh-CN'
  if (language.startsWith('en')) return 'en'
  return null
}

/** Call once at the app root. */
export function useAccountLanguageAdoption(): void {
  const auth = useAuthStore()
  watch(
    () => auth.user?.id,
    (id) => {
      if (id === undefined || hasStoredLocale()) return
      const locale = accountLocale(auth.user?.setting)
      if (locale && locale !== getLocale()) void setLocale(locale)
    },
    { immediate: true }
  )
}

/** Switches the console language and saves it to the signed-in account. */
export function useLocaleChoice(): (locale: LocaleCode) => Promise<void> {
  const auth = useAuthStore()
  const toast = useToast()
  return async (locale) => {
    await setLocale(locale)
    if (!auth.user || savedAccountLanguage(auth.user.setting) === locale) return
    try {
      await auth.saveLanguage(locale)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    }
  }
}
