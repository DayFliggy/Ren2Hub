import { shallowReactive } from 'vue'

import type { LocaleCode } from './index'

// Backend web console text arrives as English source text with {{name}}
// placeholders. English is the source language, so only zh-CN has a catalog:
// flat JSON maps keyed by that English text, one file per backend stream.
// They stay outside vue-i18n because its message syntax gives {, }, @ and |
// a meaning that backend text and upstream provider errors do not have.

export type ServerMessageParams = Record<string, unknown>
type Catalog = Readonly<Record<string, string>>
type CatalogLocale = Exclude<LocaleCode, 'en'>

const catalogFiles = import.meta.glob<Catalog>(
  './locales/zh-CN/server-messages/*.json',
  { import: 'default' }
)

const catalogs = shallowReactive<Partial<Record<CatalogLocale, Catalog>>>({})
let catalogLoad: Promise<void> | null = null

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

/** Adds entries to a locale's catalog; the loader merges every file this way. */
export function mergeServerMessages(
  locale: CatalogLocale,
  entries: Catalog
): void {
  catalogs[locale] = { ...catalogs[locale], ...entries }
}

/** Loads the zh-CN catalog once. English needs none. */
export function loadServerMessages(locale: LocaleCode): Promise<void> {
  if (locale !== 'zh-CN') return Promise.resolve()
  catalogLoad ??= Promise.all(Object.values(catalogFiles).map((load) => load()))
    .then((files) => {
      mergeServerMessages(locale, Object.assign({}, ...files))
    })
    .catch((error: unknown) => {
      catalogLoad = null
      throw error
    })
  return catalogLoad
}

function catalogEntry(locale: LocaleCode, key: string): string | undefined {
  if (locale === 'en') return undefined
  const catalog = catalogs[locale]
  return catalog && Object.hasOwn(catalog, key) ? catalog[key] : undefined
}

/**
 * Fills {{name}} placeholders in one pass, so a value that itself contains
 * "{{name}}" stays as written. Unknown names keep their placeholder.
 */
export function interpolateServerMessage(
  template: string,
  params?: ServerMessageParams
): string {
  if (!params) return template
  return template.replace(/\{\{(\w+)\}\}/g, (placeholder, name: string) =>
    Object.hasOwn(params, name) ? String(params[name]) : placeholder
  )
}

/**
 * Renders a backend message key in the locale. A key without a translation
 * shows the backend's English text, or the key filled with its params when the
 * response carried no text.
 */
export function renderServerMessage(
  locale: LocaleCode,
  key: string,
  params?: ServerMessageParams,
  englishText?: string
): string {
  const template = catalogEntry(locale, key)
  if (template !== undefined) return interpolateServerMessage(template, params)
  return englishText || interpolateServerMessage(key, params)
}

/**
 * Translates text the backend stores as a fixed English sentence, such as a
 * gateway task fail reason. The same fields carry provider errors and
 * administrator text, which have no entry and are shown exactly as written.
 */
export function translateServerText(locale: LocaleCode, text: string): string {
  return catalogEntry(locale, text) ?? text
}

/**
 * The text of a web console response: message_key with message_params when
 * present, otherwise message. Returns undefined when the payload has neither.
 */
export function serverResponseMessage(
  locale: LocaleCode,
  payload: unknown
): string | undefined {
  if (!isRecord(payload)) return undefined
  const message = typeof payload.message === 'string' ? payload.message : ''
  const key = payload.message_key
  if (typeof key !== 'string' || !key) return message || undefined
  const params = isRecord(payload.message_params)
    ? payload.message_params
    : undefined
  return renderServerMessage(locale, key, params, message)
}
