import {
  getAuthSessionSnapshot,
  type AuthSessionSnapshot,
} from '@/api/authSession'

const callbackPath = /^\/oauth\/plugin\/[^/]+\/callback\/?$/
type SessionIdentity = {
  generation: number
  sid: string | undefined
  userId: number | undefined
}
let pending:
  | {
      path: string
      params: Record<string, string>
      session: SessionIdentity
      bound: boolean
    }
  | undefined

function identity(snapshot = getAuthSessionSnapshot()): SessionIdentity {
  return {
    generation: snapshot.generation,
    sid: snapshot.bundle?.session.sid,
    userId: snapshot.bundle?.user.id,
  }
}

function sameSession(a: SessionIdentity, b: SessionIdentity): boolean {
  return (
    a.generation === b.generation && a.sid === b.sid && a.userId === b.userId
  )
}

export function isPluginLoginCallbackSessionCurrent(
  session: SessionIdentity
): boolean {
  return sameSession(session, identity())
}

/** Keep callback credentials only until this navigation mounts its component. */
export function preparePluginLoginCallback(
  path: string,
  search: string,
  hash: string,
  cleanHref: string
): boolean {
  if (!callbackPath.test(path)) {
    discardPluginLoginCallback()
    return false
  }
  if (pending?.path !== path) discardPluginLoginCallback()
  if (!search && !hash) return false

  pending = {
    path,
    params: Object.fromEntries(new URLSearchParams(search)),
    session: identity(),
    bound: false,
  }
  const clean = new URL(cleanHref, window.location.origin)
  if (window.location.pathname === clean.pathname) {
    const state = window.history.state as Record<string, unknown> | null
    window.history.replaceState(
      state && 'current' in state ? { ...state, current: cleanHref } : state,
      '',
      cleanHref
    )
  }
  return true
}

/** Run before createWebHistory can copy the initial callback URL into state. */
export function prepareInitialPluginLoginCallback(base: string): void {
  const prefix = base.replace(/\/$/, '')
  if (prefix && !window.location.pathname.startsWith(`${prefix}/`)) return
  preparePluginLoginCallback(
    window.location.pathname.slice(prefix.length),
    window.location.search,
    window.location.hash,
    window.location.pathname
  )
}

/** The route's existing auth check may restore a fresh tab's cookie once. */
export function bindPluginLoginCallbackSession(
  path: string,
  beforeAuthCheck: AuthSessionSnapshot
): void {
  if (!pending || pending.path !== path) return
  const current = identity()
  const before = identity(beforeAuthCheck)
  const restored =
    !before.sid && current.sid && current.generation === before.generation + 1
  if (
    !sameSession(pending.session, before) ||
    !current.sid ||
    (!restored && !sameSession(before, current))
  ) {
    discardPluginLoginCallback()
    return
  }
  pending.session = current
  pending.bound = true
}

export function takePluginLoginCallback(
  path: string
): { params: Record<string, string>; session: SessionIdentity } | undefined {
  const value = pending
  discardPluginLoginCallback()
  if (
    value?.path !== path ||
    !value.bound ||
    !isPluginLoginCallbackSessionCurrent(value.session)
  )
    return undefined
  return { params: value.params, session: value.session }
}

export function discardPluginLoginCallback(): void {
  pending = undefined
}
