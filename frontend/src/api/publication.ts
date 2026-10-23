import type { ApiEnvelope } from './types'

export function hasPendingPublication(envelope: ApiEnvelope<unknown>): boolean {
  const pending = envelope.publication_pending
  return pending === true || (typeof pending === 'number' && pending > 0)
}
