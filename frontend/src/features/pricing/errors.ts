import { ApiError } from '@/api/types'

type Translate = (key: string) => string
type HasKey = (key: string) => boolean

// Pricing helpers throw message keys; server errors carry their own text.
export function translateError(cause: unknown, t: Translate, te: HasKey) {
  if (!(cause instanceof Error)) return String(cause)
  return te(cause.message) ? t(cause.message) : cause.message
}

// The save endpoint answers a stale expected_version with HTTP 409. The
// transport also uses 409 for a changed login session, which is not a
// pricing conflict.
export function isPricingConflict(cause: unknown): boolean {
  return (
    cause instanceof ApiError &&
    cause.status === 409 &&
    cause.code !== 'AUTH_SESSION_CHANGED'
  )
}
