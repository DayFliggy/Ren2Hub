import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { api } from '@/api/client'
import {
  clearAuthBundle,
  getAuthSessionSnapshot,
  setAuthBundle,
} from '@/api/authSession'
import { callbackSession } from './callback-session-fixture'
import PluginLoginCallback from '../PluginLoginCallback.vue'
import {
  bindPluginLoginCallbackSession,
  discardPluginLoginCallback,
  prepareInitialPluginLoginCallback,
} from '../lib/plugin-login-callback'
vi.mock('vue-router', () => ({
  useRoute: () => ({
    path: '/oauth/plugin/fixture/callback',
    params: { key: 'fixture' },
  }),
}))
beforeEach(() => setAuthBundle(callbackSession()))
afterEach(() => {
  clearAuthBundle()
  discardPluginLoginCallback()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.history.replaceState(null, '', '/')
})
it('clears callback secrets before submitting once and broadcasts only the correlated step', async () => {
  const posted: unknown[] = []
  vi.stubGlobal(
    'BroadcastChannel',
    class {
      postMessage(value: unknown) {
        posted.push(value)
      }
      close() {}
    }
  )
  window.history.replaceState(
    null,
    '',
    '/oauth/plugin/fixture/callback?state=bound-flow&code=secret-code#secret'
  )
  prepareInitialPluginLoginCallback('/')
  bindPluginLoginCallbackSession(
    '/oauth/plugin/fixture/callback',
    getAuthSessionSnapshot()
  )
  const post = vi.spyOn(api, 'post').mockImplementation(async () => {
    expect(window.location.search).toBe('')
    expect(window.location.hash).toBe('')
    return { channel_id: 4, step: { type: 'done' } }
  })
  const wrapper = mount(PluginLoginCallback, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  await flushPromises()
  await wrapper.vm.$nextTick()
  expect(post).toHaveBeenCalledTimes(1)
  expect(posted).toEqual([
    { channel_id: 4, step: { type: 'done' }, state: 'bound-flow' },
  ])
  expect(wrapper.text()).not.toContain('secret-code')
  wrapper.unmount()
})
describe('late callback', () => {
  it.each(['leave', 'sign-out', 'new-session', 'new-account'])(
    'does not broadcast completion after %s',
    async (change) => {
      const postMessage = vi.fn()
      vi.stubGlobal(
        'BroadcastChannel',
        class {
          postMessage = postMessage
          close() {}
        }
      )
      window.history.replaceState(
        null,
        '',
        '/oauth/plugin/fixture/callback?state=bound-flow&code=secret-code'
      )
      prepareInitialPluginLoginCallback('/')
      bindPluginLoginCallbackSession(
        '/oauth/plugin/fixture/callback',
        getAuthSessionSnapshot()
      )
      let resolve!: (v: unknown) => void
      const post = vi.spyOn(api, 'post').mockImplementation(
        () =>
          new Promise((done) => {
            resolve = done
          })
      )
      const wrapper = mount(PluginLoginCallback, {
        global: {
          plugins: [
            createI18n({
              legacy: false,
              locale: 'en',
              missingWarn: false,
              fallbackWarn: false,
            }),
          ],
        },
      })
      const signal = (post.mock.calls[0]![2] as { signal: AbortSignal }).signal
      if (change === 'leave') wrapper.unmount()
      else if (change === 'sign-out') clearAuthBundle()
      else
        setAuthBundle(
          callbackSession('another-session', change === 'new-account' ? 2 : 1)
        )
      resolve({ channel_id: 4, step: { type: 'done' } })
      await flushPromises()
      if (change === 'leave') expect(signal.aborted).toBe(true)
      expect(postMessage).not.toHaveBeenCalled()
      if (change !== 'leave') wrapper.unmount()
    }
  )
})
