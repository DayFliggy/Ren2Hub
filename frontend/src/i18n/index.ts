import { createI18n } from 'vue-i18n'

import enCommon from './locales/en/common'
import enHome from './locales/en/home'
import zhCNCommon from './locales/zh-CN/common'
import zhCNHome from './locales/zh-CN/home'

export type LocaleCode = 'zh-CN' | 'en'

export const LOCALE_STORAGE_KEY = 'ren2hub_locale'
export const LEGACY_LOCALE_STORAGE_KEY = 'renren_locale'
const DEFAULT_LOCALE: LocaleCode = 'zh-CN'
export type MessageDomain = 'auth' | 'console' | 'lab'

export const availableLocales = [
  { code: 'zh-CN', name: '简体中文', flag: '中' },
  { code: 'en', name: 'English', flag: 'EN' },
] as const

function isLocaleCode(value: unknown): value is LocaleCode {
  return value === 'zh-CN' || value === 'en'
}

export function resolveInitialLocale(
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  browserLanguage: string
): LocaleCode {
  try {
    const saved = storage.getItem(LOCALE_STORAGE_KEY)
    if (isLocaleCode(saved)) return saved

    const legacy = storage.getItem(LEGACY_LOCALE_STORAGE_KEY)
    if (isLocaleCode(legacy)) {
      storage.setItem(LOCALE_STORAGE_KEY, legacy)
      storage.removeItem(LEGACY_LOCALE_STORAGE_KEY)
      return legacy
    }
  } catch {
    // A restricted storage context may still use the browser locale.
  }

  return browserLanguage.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

export function resolveInitialLocaleFromStorage(
  getStorage: () => Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  browserLanguage: string
): LocaleCode {
  try {
    return resolveInitialLocale(getStorage(), browserLanguage)
  } catch {
    return browserLanguage.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
  }
}

const initialLocale = resolveInitialLocaleFromStorage(
  () => window.localStorage,
  window.navigator.language
)

const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: 'en',
  messages: {
    en: { ...enHome, ...enCommon },
    'zh-CN': { ...zhCNHome, ...zhCNCommon },
  },
})

const domainLoads = new Map<MessageDomain, Promise<void>>()

const domainLoaders: Record<
  MessageDomain,
  () => Promise<{ en: Record<string, unknown>; zhCN: Record<string, unknown> }>
> = {
  auth: async () => {
    const [en, zhCN] = await Promise.all([
      import('./locales/en/auth'),
      import('./locales/zh-CN/auth'),
    ])
    return { en: en.default, zhCN: zhCN.default }
  },
  console: async () => {
    const [en, zhCN] = await Promise.all([
      import('./locales/en/console'),
      import('./locales/zh-CN/console'),
    ])
    return { en: en.default, zhCN: zhCN.default }
  },
  lab: async () => {
    const [en, zhCN] = await Promise.all([
      import('./locales/en/lab'),
      import('./locales/zh-CN/lab'),
    ])
    return { en: en.default, zhCN: zhCN.default }
  },
}

export function loadMessageDomain(domain: MessageDomain): Promise<void> {
  const existing = domainLoads.get(domain)
  if (existing) return existing

  const request = domainLoaders[domain]()
    .then(({ en, zhCN }) => {
      i18n.global.mergeLocaleMessage('en', en)
      i18n.global.mergeLocaleMessage('zh-CN', zhCN)
    })
    .catch((error: unknown) => {
      domainLoads.delete(domain)
      throw error
    })
  domainLoads.set(domain, request)
  return request
}

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
