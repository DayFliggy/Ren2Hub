import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { completeOAuth, oauthProviders } from '../oauth'
import { parsePublicStatus } from '../public'
import { ApiError } from '../types'
import {
  clearAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

afterEach(() => {
  vi.restoreAllMocks()
  clearAffiliateAttribution()
})

describe('OAuth referral and callback contract', () => {
  it('uses the configured OIDC name with a blank or absent fallback', () => {
    for (const [value, name] of [
      ['  Company Login  ', 'Company Login'],
      ['', 'OIDC'],
      ['   ', 'OIDC'],
      [undefined, 'OIDC'],
    ] as const) {
      const status = parsePublicStatus({
        oidc_enabled: true,
        oidc_display_name: value,
      })
      expect(oauthProviders(status)).toEqual([{ slug: 'oidc', name }])
    }
    expect(() => parsePublicStatus({ oidc_display_name: 123 })).toThrow()
  })
  it.each(['login', 'bind'] as const)(
    'uses the SPA callback and carries attribution only for login (%s)',
    async (intent) => {
      storeAffiliateAttribution('REFERRAL')
      const post = vi
        .spyOn(api, 'post')
        .mockResolvedValue({ flow_token: 'flow-token' })
      const get = vi
        .spyOn(api, 'get')
        .mockResolvedValue({ authenticated: true })
      const popup = {
        closed: false,
        close: vi.fn(),
        location: { replace: vi.fn() },
      } as unknown as Window
      const result = completeOAuth(
        { linuxdo_oauth: true, linuxdo_client_id: 'client' },
        'linuxdo',
        intent,
        undefined,
        undefined,
        undefined,
        popup
      )
      await flushPromises()
      expect(post).toHaveBeenCalledWith(
        '/api/oauth/state',
        {
          provider: 'linuxdo',
          intent,
          ...(intent === 'login' ? { aff: 'REFERRAL' } : {}),
        },
        undefined
      )
      const url = new URL(vi.mocked(popup.location.replace).mock.calls[0]![0])
      expect(url.searchParams.get('redirect_uri')).toBe(
        `${window.location.origin}/oauth/linuxdo`
      )
      window.dispatchEvent(
        new MessageEvent('message', {
          origin: window.location.origin,
          source: popup,
          data: {
            type: 'ren2hub:oauth-bind-callback',
            provider: 'linuxdo',
            state: 'flow-token',
            code: 'authorization-code',
          },
        })
      )
      await expect(result).resolves.toEqual({ authenticated: true })
      expect(get).toHaveBeenCalledWith('/api/oauth/linuxdo', {
        state: 'flow-token',
        code: 'authorization-code',
        error: undefined,
        error_description: undefined,
      })
      expect(popup.close).toHaveBeenCalledOnce()
    }
  )
  it('reports a blocked pop-up with a stable client error code', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const post = vi.spyOn(api, 'post')
    await expect(
      completeOAuth({ github_oauth: true }, 'github', 'login')
    ).rejects.toMatchObject({ code: 'oauthPopupBlocked' })
    expect(post).not.toHaveBeenCalled()
  })

  it('reports a cancelled verification as a translatable AbortError', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({ flow_token: 'flow-token' })
    const popup = {
      closed: false,
      close: vi.fn(),
      location: { replace: vi.fn() },
    } as unknown as Window
    const controller = new AbortController()
    controller.abort()
    await expect(
      completeOAuth(
        { github_oauth: true, github_client_id: 'client' },
        'github',
        'login',
        undefined,
        undefined,
        controller.signal,
        popup
      )
    ).rejects.toMatchObject({ name: 'AbortError', code: 'oauthCancelled' })
    expect(popup.close).toHaveBeenCalledOnce()
  })

  it.each([
    ['OAUTH_FLOW_BROWSER_MISMATCH', { code: 'oauthBrowserMismatch' }],
    ['FORBIDDEN', { message: 'Forbidden', status: 403, code: 'FORBIDDEN' }],
  ])('maps only the browser binding failure (%s)', async (code, expected) => {
    vi.spyOn(api, 'post').mockResolvedValue({ flow_token: 'flow-token' })
    const get = vi
      .spyOn(api, 'get')
      .mockRejectedValue(new ApiError('Forbidden', { status: 403, code }))
    const popup = {
      closed: false,
      close: vi.fn(),
      location: { replace: vi.fn() },
    } as unknown as Window
    const result = completeOAuth(
      { github_oauth: true, github_client_id: 'client' },
      'github',
      'login',
      undefined,
      undefined,
      undefined,
      popup
    )
    await flushPromises()
    window.dispatchEvent(
      new MessageEvent('message', {
        origin: window.location.origin,
        source: popup,
        data: {
          type: 'ren2hub:oauth-bind-callback',
          provider: 'github',
          state: 'flow-token',
          code: 'authorization-code',
        },
      })
    )
    await expect(result).rejects.toMatchObject(expected)
    expect(get).toHaveBeenCalledOnce()
  })

  it('sends cookies with the state and callback requests', async () => {
    const sent: Array<{ url: string; credentials: boolean }> = []
    const replies: unknown[] = [
      { success: true, data: { flow_token: 'flow-token' } },
      { success: true, data: { authenticated: true } },
    ]
    const nativeOpen = XMLHttpRequest.prototype.open
    const urls = new WeakMap<XMLHttpRequest, string>()
    vi.spyOn(XMLHttpRequest.prototype, 'open').mockImplementation(function (
      this: XMLHttpRequest,
      method: string,
      url: string | URL
    ) {
      urls.set(this, String(url))
      nativeOpen.call(this, method, url, true)
    })
    vi.spyOn(XMLHttpRequest.prototype, 'send').mockImplementation(function (
      this: XMLHttpRequest
    ) {
      sent.push({ url: urls.get(this)!, credentials: this.withCredentials })
      const body = JSON.stringify(replies.shift())
      Object.defineProperties(this, {
        readyState: { value: 4 },
        status: { value: 200 },
        statusText: { value: 'OK' },
        responseText: { value: body },
        response: { value: body },
      })
      this.getAllResponseHeaders = () => 'content-type: application/json'
      queueMicrotask(() => this.onloadend?.(new ProgressEvent('loadend')))
    })
    const popup = {
      closed: false,
      close: vi.fn(),
      location: { replace: vi.fn() },
    } as unknown as Window
    const result = completeOAuth(
      { github_oauth: true, github_client_id: 'client' },
      'github',
      'login',
      undefined,
      undefined,
      undefined,
      popup
    )
    await vi.waitFor(() => expect(popup.location.replace).toHaveBeenCalled())
    window.dispatchEvent(
      new MessageEvent('message', {
        origin: window.location.origin,
        source: popup,
        data: {
          type: 'ren2hub:oauth-bind-callback',
          provider: 'github',
          state: 'flow-token',
          code: 'authorization-code',
        },
      })
    )
    await expect(result).resolves.toEqual({ authenticated: true })
    expect(sent.map((item) => item.credentials)).toEqual([true, true])
    expect(
      sent.map((item) => new URL(item.url, location.origin).pathname)
    ).toEqual(['/api/oauth/state', '/api/oauth/github'])
  })
})
