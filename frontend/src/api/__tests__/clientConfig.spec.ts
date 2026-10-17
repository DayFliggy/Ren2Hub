import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { publicApi } from '@/api/public'
import {
  getTokenClientConfig,
  loadClientConfigCatalog,
} from '@/api/clientConfig'

const token = {
  id: 7,
  name: 'Key',
  key: 'sk-****masked',
  type: 'manual',
  status: 1,
  used_quota: 0,
  remain_quota: 1000,
  unlimited_quota: false,
  group: 'default',
  cross_group_retry: false,
  expired_time: -1,
  created_time: 100,
}
const pricing = [
  {
    model_name: 'future-model',
    supported_endpoint_types: ['openai', 'openai-response'],
  },
]

beforeEach(() => {
  vi.spyOn(publicApi, 'status').mockResolvedValue({
    server_address: 'https://gateway.example',
  })
})
afterEach(() => vi.restoreAllMocks())

describe('native Key client configuration contract', () => {
  it('uses only masked token metadata and real routing candidates, including new model names', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (path) => {
      if (path === '/api/pricing') return pricing
      if (path === '/api/token/7') return token
      if (path === '/api/token/7/routing/channels')
        return [
          {
            id: 2,
            name: 'Allowed',
            type: 1,
            status: 1,
            models: ['future-model', 'future-model'],
          },
        ]
      if (path === '/api/token/7/routing/preview')
        return {
          mode: 'manual',
          version: 2,
          model: 'future-model',
          path: '/v1/responses',
          groups: ['default'],
          entries: [{ channel_id: 2, eligible: true, reason: 'eligible' }],
        }
      throw new Error(`Unexpected path ${path}`)
    })
    const post = vi.spyOn(api, 'post')
    const catalog = await loadClientConfigCatalog(7)
    expect(catalog.models).toEqual(['future-model'])
    const result = await getTokenClientConfig(
      7,
      'codex',
      catalog.models[0],
      catalog.baseURL
    )
    expect(result.content).toContain('model = "future-model"')
    expect(result.content).not.toContain(token.key)
    expect(get).toHaveBeenCalledWith(
      '/api/token/7/routing/preview',
      { model: 'future-model', path: '/v1/responses' },
      { signal: undefined }
    )
    expect(post).not.toHaveBeenCalled()
    expect(
      get.mock.calls.every(
        ([path]) => !path.includes('client-config') && !path.endsWith('/key')
      )
    ).toBe(true)
  })

  it.each([
    'model_not_allowed',
    'protocol_unavailable',
    'entry_disabled',
    'unavailable',
  ])('does not export after routing rejects %s', async (reason) => {
    vi.spyOn(api, 'get').mockImplementation(async (path) => {
      if (path === '/api/pricing') return pricing
      return path === '/api/token/7'
        ? token
        : {
            mode: 'manual',
            version: 3,
            model: 'future-model',
            path: '/v1/responses',
            groups: ['default'],
            entries: [{ channel_id: 2, eligible: false, reason }],
          }
    })
    await expect(
      getTokenClientConfig(
        7,
        'codex',
        'future-model',
        'https://gateway.example'
      )
    ).rejects.toThrow('model_unavailable')
  })

  it.each([{ status: 2 }, { expired_time: 1 }])(
    'rechecks disabled or expired tokens on regeneration',
    async (state) => {
      const get = vi
        .spyOn(api, 'get')
        .mockImplementation(async (path) =>
          path === '/api/pricing' ? pricing : { ...token, ...state }
        )
      await expect(
        getTokenClientConfig(
          7,
          'codex',
          'future-model',
          'https://gateway.example'
        )
      ).rejects.toThrow('token_unavailable')
      expect(get.mock.calls.some(([path]) => path.endsWith('/preview'))).toBe(
        false
      )
    }
  )

  it('uses Chat Completions eligibility for Pi and propagates authorization failures', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (path) => {
      if (path === '/api/pricing') return pricing
      if (path === '/api/token/7') return token
      throw new Error('permission revoked')
    })
    await expect(
      getTokenClientConfig(7, 'pi', 'future-model', 'https://gateway.example', {
        contextWindow: 64000,
        maxTokens: 8000,
        reasoning: false,
        imageInput: false,
      })
    ).rejects.toThrow('permission revoked')
    expect(get).toHaveBeenLastCalledWith(
      '/api/token/7/routing/preview',
      { model: 'future-model', path: '/v1/chat/completions' },
      { signal: undefined }
    )
  })

  it('does not mistake a routable non-Responses model for Codex compatibility', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (path) => {
      if (path === '/api/token/7') return token
      if (path === '/api/pricing')
        return [
          { model_name: 'future-model', supported_endpoint_types: ['openai'] },
        ]
      throw new Error(`Unexpected request: ${path}`)
    })
    await expect(
      getTokenClientConfig(
        7,
        'codex',
        'future-model',
        'https://gateway.example'
      )
    ).rejects.toThrow('model_unavailable')
    expect(get.mock.calls.some(([path]) => path.endsWith('/preview'))).toBe(
      false
    )
  })
})
