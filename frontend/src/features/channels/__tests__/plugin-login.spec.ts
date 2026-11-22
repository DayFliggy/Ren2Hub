import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { clearAuthBundle } from '@/api/authSession'
import ChannelBalanceDetails from '../components/ChannelBalanceDetails.vue'
import PluginCredentials from '../components/PluginCredentials.vue'
import PluginLoginDialog from '../components/PluginLoginDialog.vue'
import {
  parsePluginLoginStep,
  readPluginLoginMessage,
  safeLoginUrl,
  startPluginLogin,
  continuePluginLogin,
  submitPluginLoginCallback,
} from '../lib/plugin-login'
const global = {
  plugins: [
    createI18n({
      legacy: false,
      locale: 'en',
      missingWarn: false,
      fallbackWarn: false,
    }),
  ],
  stubs: { ConsoleModal: { template: '<div><slot /></div>' } },
}
afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
describe('plugin login contracts', () => {
  it('rejects unsafe links and requires both channel and state on broadcasts', () => {
    for (const url of [
      'javascript:alert(1)',
      'data:text/html,test',
      'https://user:pass@host.test',
    ])
      expect(safeLoginUrl(url)).toBeNull()
    expect(() =>
      parsePluginLoginStep({ type: 'redirect', url: 'javascript:alert(1)' })
    ).toThrow()
    expect(
      readPluginLoginMessage(
        { channel_id: 4, state: 'other', step: { type: 'done' } },
        4,
        'flow'
      )
    ).toBeNull()
    expect(
      readPluginLoginMessage(
        { channel_id: 5, state: 'flow', step: { type: 'done' } },
        4,
        'flow'
      )
    ).toBeNull()
    expect(
      readPluginLoginMessage(
        { channel_id: 4, state: 'flow', step: { type: 'done' } },
        4,
        'flow'
      )
    ).toEqual({ type: 'done' })
  })
  it('preserves upstream start/continue/callback payload contracts', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({ flow: 'state', step: { type: 'pending' } })
      .mockResolvedValueOnce({ step: { type: 'done' } })
      .mockResolvedValueOnce({ channel_id: 4, step: { type: 'done' } })
    const signal = new AbortController().signal
    // A channel holds one credential: a login replaces it, so the request
    // names no key line and never asks to add one.
    await startPluginLogin(4, signal)
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/api/channel/4/plugin_login',
      {},
      { signal }
    )
    await continuePluginLogin(4, 'state', { fields: { otp: '123' } }, signal)
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/api/channel/4/plugin_login/continue',
      { flow: 'state', input: { fields: { otp: '123' } } },
      { signal }
    )
    await submitPluginLoginCallback(
      'fixture',
      { state: 'state', code: 'single-use' },
      signal
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/api/channel/plugin_login/callback',
      { plugin: 'fixture', params: { state: 'state', code: 'single-use' } },
      { signal }
    )
  })
  it('reports an invalid plugin login step through i18n', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      flow: 'flow',
      step: { type: 'redirect', url: 'javascript:alert(1)' },
    })
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(
      'common.clientErrors.pluginLoginInvalidResponse'
    )
    wrapper.unmount()
  })
  it('cancels a pending device flow and ignores a late server response', async () => {
    vi.useFakeTimers()
    let resolve!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    expect(post).toHaveBeenCalledTimes(1)
    const signal = (post.mock.calls[0]![2] as { signal: AbortSignal }).signal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    resolve({
      flow: 'state',
      step: {
        type: 'device',
        user_code: 'code',
        verification_uri: 'https://provider.test',
        interval: 1,
      },
    })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10000)
    expect(post).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('done')).toBeUndefined()
  })
  it('keeps the device code while pending and emits success once', async () => {
    vi.useFakeTimers()
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({
        flow: 'state',
        step: {
          type: 'device',
          user_code: 'ABCD',
          verification_uri: 'https://provider.test',
          interval: 1,
        },
      })
      .mockResolvedValueOnce({ step: { type: 'pending', interval: 1 } })
      .mockResolvedValueOnce({ step: { type: 'done' } })
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.text()).toContain('ABCD')
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(post).toHaveBeenCalledTimes(3)
    expect(wrapper.emitted('done')).toHaveLength(1)
    wrapper.unmount()
  })
  it('does not poll an expired device code', async () => {
    vi.useFakeTimers()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      flow: 'state',
      step: {
        type: 'device',
        user_code: 'ABCD',
        verification_uri: 'https://provider.test',
        expires_at: Math.floor(Date.now() / 1000) - 1,
        interval: 1,
      },
    })
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(post).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('pluginExtensions.expired')
    wrapper.unmount()
  })
  it('rejects a valid redirect callback after the local session is invalidated', async () => {
    let receive!: (event: { data: unknown }) => void
    vi.stubGlobal(
      'BroadcastChannel',
      class {
        addEventListener(_: string, listener: typeof receive) {
          receive = listener
        }
        close() {}
      }
    )
    vi.spyOn(api, 'post').mockResolvedValue({
      flow: 'state',
      step: { type: 'redirect', url: 'https://provider.test' },
    })
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    await flushPromises()
    clearAuthBundle()
    receive({ data: { state: 'state', channel_id: 4, step: { type: 'done' } } })
    await flushPromises()
    expect(wrapper.emitted('done')).toBeUndefined()
    expect(wrapper.text()).toContain('pluginExtensions.waiting')
    wrapper.unmount()
  })
  it('stops device polling when the authentication identity is invalidated', async () => {
    vi.useFakeTimers()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      flow: 'state',
      step: { type: 'pending', interval: 1 },
    })
    const wrapper = mount(PluginLoginDialog, {
      props: { channelId: 4 },
      global,
    })
    await flushPromises()
    clearAuthBundle()
    await vi.advanceTimersByTimeAsync(5000)
    expect(post).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })
})
describe('plugin credentials', () => {
  const buttons = (wrapper: ReturnType<typeof mount>) =>
    wrapper.findAll('button').map((button) => button.text())
  it('offers one sign-in when the channel has no credential', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ items: [] })
    const wrapper = mount(PluginCredentials, {
      props: { channelId: 4, canWrite: true },
      global,
    })
    await flushPromises()
    expect(buttons(wrapper)).toEqual(['pluginExtensions.signIn'])
    wrapper.unmount()
  })
  it('logs the stored credential in again and offers no second account', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      items: [{ label: 'fixture@example.com' }],
    })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ flow: 'state', step: { type: 'pending' } })
    const wrapper = mount(PluginCredentials, {
      props: { channelId: 4, canWrite: true },
      global,
    })
    await flushPromises()
    expect(wrapper.text()).toContain('fixture@example.com')
    expect(buttons(wrapper)).toEqual(['pluginExtensions.relogin'])
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/channel/4/plugin_login',
      {},
      expect.any(Object)
    )
    wrapper.unmount()
  })
})
describe('channel balance details', () => {
  it('shows the channel balance only, not per-key rows of an older query', () => {
    const balance_detail = {
      amount: 7,
      currency: 'USD',
      error: 'quota endpoint failed',
      windows: [{ name: 'Daily', used: 3, limit: 10, unit: 'req' }],
      keys: { '0': { amount: 2, error: 'stale key failure' } },
    }
    const wrapper = mount(ChannelBalanceDetails, {
      props: {
        channel: {
          id: 1,
          balance: 7,
          other_info: JSON.stringify({ balance_detail }),
        } as never,
      },
      global,
    })
    const text = wrapper.text()
    expect(text).toContain('$7')
    expect(text).toContain('quota endpoint failed')
    expect(text).toContain('Daily 3 / 10 req')
    expect(text).not.toContain('stale key failure')
    expect(wrapper.findAll('details > div')).toHaveLength(0)
  })
})
