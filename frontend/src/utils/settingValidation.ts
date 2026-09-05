import type { SystemSettingField } from '@/constants/systemSettingsCatalog'
import { SETTINGS_RECORD_EDITORS } from '@/constants/settingsEditors'

const object = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === 'object' && !Array.isArray(v)
export function validSettingUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return (
      ['http:', 'https:'].includes(url.protocol) &&
      !url.username &&
      !url.password
    )
  } catch {
    return false
  }
}

/** Returns a message key; no invalid input is normalized into a successful value. */
export function validateSetting(
  field: SystemSettingField,
  value: unknown
): string | null {
  if (field.kind === 'boolean')
    return typeof value === 'boolean' ? null : 'shape'
  if (field.kind === 'number') {
    if (value === '' || value === null || !Number.isFinite(Number(value)))
      return 'number'
    const n = Number(value)
    if (
      (field.min !== undefined && n < field.min) ||
      (field.max !== undefined && n > field.max) ||
      (field.integer && !Number.isSafeInteger(n))
    )
      return 'range'
    return null
  }
  if (typeof value !== 'string') return 'shape'
  if (field.kind === 'url')
    return !value.trim() || validSettingUrl(value) ? null : 'url'
  if (field.kind === 'select' || field.kind === 'role')
    return field.options?.some((option) => option.value === value)
      ? null
      : 'shape'
  if (
    !['json', 'list', 'key-value', 'ratio', 'amount-list', 'discount'].includes(
      field.kind
    )
  )
    return null
  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    return 'shape'
  }
  const schema = SETTINGS_RECORD_EDITORS[field.key]
  if (schema) {
    if (
      !Array.isArray(parsed) ||
      parsed.some((row) => !object(row)) ||
      (schema.limit && parsed.length > schema.limit)
    )
      return 'shape'
    const keys = new Set<string>()
    for (const row of parsed as Record<string, unknown>[]) {
      if (schema.uniqueKey) {
        const key = String(row[schema.uniqueKey] ?? '').trim()
        if (!key) return 'required'
        if (keys.has(key)) return 'duplicate'
        keys.add(key)
      }
      for (const column of schema.fields) {
        const item = row[column.key]
        if (column.required && (item == null || String(item).trim() === ''))
          return 'required'
        if (item == null || item === '') continue
        if (
          column.kind === 'number' &&
          (typeof item !== 'number' ||
            !Number.isFinite(item) ||
            (column.min !== undefined && item < column.min))
        )
          return 'range'
        if (column.kind === 'url' && !validSettingUrl(String(item)))
          return 'url'
        if (
          column.kind === 'datetime' &&
          !Number.isFinite(Date.parse(String(item)))
        )
          return 'shape'
        if (column.kind === 'boolean' && typeof item !== 'boolean')
          return 'shape'
        if (column.choices && !column.choices.includes(String(item)))
          return 'shape'
        if (column.maxLength && String(item).length > column.maxLength)
          return 'range'
      }
    }
    return null
  }
  if (field.kind === 'list')
    return Array.isArray(parsed) && parsed.every((v) => typeof v === 'string')
      ? null
      : 'shape'
  if (field.kind === 'amount-list')
    return Array.isArray(parsed) &&
      parsed.every((v) => Number.isSafeInteger(v) && v > 0) &&
      new Set(parsed).size === parsed.length
      ? null
      : 'range'
  if (field.kind === 'key-value')
    return object(parsed) &&
      Object.values(parsed).every((v) => typeof v === 'string')
      ? null
      : 'shape'
  if (field.kind === 'ratio')
    return object(parsed) &&
      Object.values(parsed).every(
        (v) => typeof v === 'number' && Number.isFinite(v) && v >= 0
      )
      ? null
      : 'range'
  if (field.kind === 'discount')
    return object(parsed) &&
      Object.entries(parsed).every(
        ([k, v]) =>
          Number.isSafeInteger(Number(k)) &&
          Number(k) > 0 &&
          typeof v === 'number' &&
          v > 0 &&
          v <= 1
      )
      ? null
      : 'range'
  if (
    String(field.defaultValue).trim().startsWith('[') &&
    !Array.isArray(parsed)
  )
    return 'shape'
  if (String(field.defaultValue).trim().startsWith('{') && !object(parsed))
    return 'shape'
  return null
}
