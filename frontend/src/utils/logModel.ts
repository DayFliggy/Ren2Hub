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

function text(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

// Mirrors relay/common.ResponseModel: the backend stores the requested,
// upstream and returned names under other.response_model.
export function logModelObservation(log: LogItem) {
  const evidence = logEvidence(log)
  const observed = isRecord(evidence.response_model)
    ? evidence.response_model
    : {}
  const upstream =
    text(observed.upstream_model) || text(evidence.upstream_model_name)
  const response = text(observed.returned_model)
  const returned = response.toLowerCase()
  const expected = [text(observed.requested_model) || log.model, upstream]
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
