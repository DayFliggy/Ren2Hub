import { ApiError } from '@/api/types'

export type ErrorContext = 'read' | 'action'

/**
 * The i18n key for a failed version request. Server error text is English
 * and never shown; failures map by HTTP status. A 500 after an update may
 * mean the old program could not be restored either, so no text claims it.
 */
export function updateErrorMessage(
  cause: unknown,
  context: ErrorContext
): { key: string; params?: Record<string, unknown> } {
  if (!(cause instanceof ApiError)) return { key: 'errors.generic' }
  if (cause.code === 'INVALID_RESPONSE') return { key: 'errors.invalid' }
  // The transport reports a sign-in change during the request as HTTP 409.
  if (cause.code === 'AUTH_SESSION_CHANGED')
    return { key: 'errors.sessionChanged' }
  switch (cause.status) {
    case 400:
      return { key: 'errors.rejected' }
    case 404:
      return { key: 'errors.notInList' }
    case 409:
      return { key: 'errors.busy' }
    case 500:
      if (context === 'action') return { key: 'errors.server' }
      break
    case 502:
      return {
        key: context === 'action' ? 'errors.download' : 'errors.github',
      }
    case 504:
    case undefined:
      // A gateway timeout or a dropped connection leaves the outcome open.
      return {
        key: context === 'action' ? 'errors.noResponse' : 'errors.unreachable',
      }
  }
  return { key: 'errors.status', params: { status: cause.status } }
}
