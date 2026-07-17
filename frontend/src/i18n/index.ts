import { createI18n } from 'vue-i18n'

import en from './locales/en'
import zhCN from './locales/zh-CN'

export type LocaleCode = 'zh-CN' | 'en'

const LOCALE_STORAGE_KEY = 'ren2hub_locale'
const DEFAULT_LOCALE: LocaleCode = 'zh-CN'

export const availableLocales = [
  { code: 'zh-CN', name: '简体中文', flag: '中' },
  { code: 'en', name: 'English', flag: 'EN' },
] as const

function isLocaleCode(value: unknown): value is LocaleCode {
  return value === 'zh-CN' || value === 'en'
}

function getInitialLocale(): LocaleCode {
  try {
    const saved = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    if (isLocaleCode(saved)) return saved
  } catch {
    // Browser storage is optional.
  }

  return window.navigator.language.toLowerCase().startsWith('zh')
    ? 'zh-CN'
    : 'en'
}

const initialLocale = getInitialLocale()

const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: 'en',
  messages: {
    en,
    'zh-CN': zhCN,
  },
})

export function setLocale(locale: string): void {
  if (!isLocaleCode(locale)) return
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    // Browser storage is optional.
  }
}

export function getLocale(): LocaleCode {
  const locale = i18n.global.locale.value
  return isLocaleCode(locale) ? locale : DEFAULT_LOCALE
}

document.documentElement.lang = initialLocale

export default i18n
