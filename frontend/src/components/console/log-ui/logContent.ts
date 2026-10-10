import type { LocaleCode } from '@/i18n'
import {
  renderServerMessage,
  translateServerText,
  type ServerMessageParams,
} from '@/i18n/serverMessages'
import type { LogItem } from '@/types/console'

interface ContentPart {
  key: string
  params?: ServerMessageParams
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function isContentPart(value: unknown): value is ContentPart {
  return (
    isRecord(value) &&
    typeof value.key === 'string' &&
    value.key !== '' &&
    (value.params === undefined || isRecord(value.params))
  )
}

function contentParts(other: LogItem['other']): ContentPart[] | null {
  if (!isRecord(other)) return null
  const parts = other.content_parts
  if (!Array.isArray(parts) || parts.length === 0) return null
  return parts.every(isContentPart) ? parts : null
}

/**
 * Log content in the viewer's language. The backend stores it as
 * other.content_parts (English source keys and params) and keeps the English
 * text in content, which rows written before content_parts still show.
 */
export function logContentText(
  log: Pick<LogItem, 'content' | 'other'>,
  locale: LocaleCode
): string {
  const parts = contentParts(log.other)
  if (!parts) return log.content
  return parts
    .map((part) => renderServerMessage(locale, part.key, part.params))
    .join(', ')
}

/**
 * The token name to show. A row written without a token, such as a channel
 * test, stores its name as a fixed English sentence.
 */
export function logTokenName(
  log: Pick<LogItem, 'token_id' | 'token_name'>,
  locale: LocaleCode
): string {
  return log.token_id === 0
    ? translateServerText(locale, log.token_name)
    : log.token_name
}
