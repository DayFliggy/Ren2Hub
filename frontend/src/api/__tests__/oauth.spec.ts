import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { completeOAuth } from '../oauth'
import {
  clearAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

afterEach(() => {
  vi.restoreAllMocks()
  clearAffiliateAttribution()
})

describe('OAuth referral and callback contract', () => {
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
})
