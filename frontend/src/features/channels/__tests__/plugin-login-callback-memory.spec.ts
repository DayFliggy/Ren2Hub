import { afterEach, beforeEach, expect, it } from 'vitest'
import {
  applyAuthRotation,
  clearAuthBundle,
  getAuthSessionSnapshot,
  setAuthBundle,
} from '@/api/authSession'
import { callbackSession } from './callback-session-fixture'
import {
  bindPluginLoginCallbackSession,
  discardPluginLoginCallback,
  prepareInitialPluginLoginCallback,
  preparePluginLoginCallback,
  takePluginLoginCallback,
} from '../lib/plugin-login-callback'

beforeEach(() => setAuthBundle(callbackSession()))
afterEach(() => {
  clearAuthBundle()
  discardPluginLoginCallback()
  window.history.replaceState(null, '', '/')
})

it.each(['/', '/gateway/'])(
  'removes incoming credentials before history initialization with base %s',
  (base) => {
    const path = '/oauth/plugin/fixture/callback'
    const href = `${base.replace(/\/$/, '')}${path}`
    const incoming = `${href}?state=bound-flow&code=secret-code#secret-fragment`
    window.history.replaceState({ current: incoming }, '', incoming)
    prepareInitialPluginLoginCallback(base)
    bindPluginLoginCallbackSession(path, getAuthSessionSnapshot())
    expect(window.location.pathname).toBe(href)
    expect(window.location.search).toBe('')
    expect(window.location.hash).toBe('')
    expect(window.history.state.current).toBe(href)
    expect(takePluginLoginCallback(path)?.params).toEqual({
      state: 'bound-flow',
      code: 'secret-code',
    })
    expect(takePluginLoginCallback(path)).toBeUndefined()
  }
)

it('discards abandoned parameters and never hands them to another plugin', () => {
  const path = '/oauth/plugin/fixture/callback'
  preparePluginLoginCallback(path, 'state=bound-flow&code=secret', '', path)
  expect(
    takePluginLoginCallback('/oauth/plugin/other/callback')
  ).toBeUndefined()
  expect(takePluginLoginCallback(path)).toBeUndefined()
  preparePluginLoginCallback(path, 'state=bound-flow&code=secret', '', path)
  preparePluginLoginCallback('/sign-in', '', '', '/sign-in')
  expect(takePluginLoginCallback(path)).toBeUndefined()
})

it('accepts one initial cookie restoration but not a later identity change', () => {
  clearAuthBundle()
  const path = '/oauth/plugin/fixture/callback'
  preparePluginLoginCallback(path, 'state=initial&code=secret', '', path)
  const before = getAuthSessionSnapshot()
  setAuthBundle(callbackSession())
  bindPluginLoginCallbackSession(path, before)
  expect(takePluginLoginCallback(path)?.params.state).toBe('initial')
})

it.each(['before-auth', 'before-consume'])(
  'rejects a new session at %s even for the same account',
  (phase) => {
    const path = '/oauth/plugin/fixture/callback'
    preparePluginLoginCallback(path, 'state=old&code=secret', '', path)
    if (phase === 'before-consume')
      bindPluginLoginCallbackSession(path, getAuthSessionSnapshot())
    setAuthBundle(callbackSession('other-session'))
    if (phase === 'before-auth')
      bindPluginLoginCallbackSession(path, getAuthSessionSnapshot())
    expect(takePluginLoginCallback(path)).toBeUndefined()
  }
)

it('keeps a normal access-token rotation in the same session valid', () => {
  const path = '/oauth/plugin/fixture/callback'
  preparePluginLoginCallback(path, 'state=bound&code=secret', '', path)
  bindPluginLoginCallbackSession(path, getAuthSessionSnapshot())
  applyAuthRotation({
    ...callbackSession(),
    access_token: 'rotated-test-access',
  })
  expect(takePluginLoginCallback(path)?.params.state).toBe('bound')
})

it('leaves site OAuth parameters untouched', () => {
  window.history.replaceState(
    null,
    '',
    '/oauth/github?state=site&code=site-code'
  )
  prepareInitialPluginLoginCallback('/')
  expect(window.location.search).toBe('?state=site&code=site-code')
})
