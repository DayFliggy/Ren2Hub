import { isRecord } from '@/api/contracts'
import type { LogItem } from '@/types/console'

export function logEvidence(log: LogItem): Record<string, unknown> {
  if (isRecord(log.other)) return log.other
  if (typeof log.other !== 'string') return {}
  try {
    const value: unknown = JSON.parse(log.other)
    return isRecord(value) ? value : {}
  } catch {
    return {}
  }
}

export function logModelObservation(log: LogItem) {
  const evidence = logEvidence(log)
  const upstream =
    typeof evidence.upstream_model_name === 'string'
      ? evidence.upstream_model_name.trim()
      : ''
  const response =
    typeof evidence.response_model_name === 'string'
      ? evidence.response_model_name.trim()
      : ''
  const returned = response.toLowerCase()
  const expected = [log.model, upstream]
    .map((name) => name.trim().toLowerCase())
    .filter(Boolean)
  const mismatch = Boolean(
    returned &&
    expected.length &&
    !expected.some(
      (name) => returned.startsWith(name) || returned.endsWith(name)
    )
  )
  return { upstream, response, mismatch }
}
