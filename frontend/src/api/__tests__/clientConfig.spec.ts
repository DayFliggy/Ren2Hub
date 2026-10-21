import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  getTokenClientConfig,
  loadClientConfigCatalog,
} from '@/api/clientConfig'
import { modelCatalogFixture, nativeTokenFixture } from './modelCatalogFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.get).mockImplementation(async (url) =>
    url === '/api/token/7'
      ? { ...nativeTokenFixture, id: 7 }
      : modelCatalogFixture()
  )
})
describe('native client configuration', () => {
  it('derives protocol choices from the permission-scoped model catalog', async () => {
    const result = await loadClientConfigCatalog(7)
    expect(result.protocolModels).toEqual({
      codex: ['fixture-model'],
      pi: ['fixture-model'],
    })
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/model-catalog',
      { token_id: 7 },
      expect.anything()
    )
  })
  it('revalidates access before generation without reading a secret', async () => {
    const result = await getTokenClientConfig(
      7,
      'codex',
      'fixture-model',
      'https://api.example.test/prefix'
    )
    expect(result.content).toContain(
      'base_url = "https://api.example.test/prefix/v1"'
    )
    expect(result.content).toContain('env_key = "REN2HUB_API_KEY"')
    expect(result.content).not.toContain(nativeTokenFixture.key)
    expect(api.get).not.toHaveBeenCalledWith(
      expect.stringMatching(/\/key$/),
      expect.anything(),
      expect.anything()
    )
  })
  it.each([2, 3, 4])(
    'refuses an unavailable native token status %s',
    async (status) => {
      vi.mocked(api.get).mockImplementation(async (url) =>
        url === '/api/token/7'
          ? { ...nativeTokenFixture, id: 7, status }
          : modelCatalogFixture()
      )
      await expect(
        getTokenClientConfig(
          7,
          'codex',
          'fixture-model',
          'https://api.example.test'
        )
      ).rejects.toMatchObject({ code: 'token_unavailable' })
    }
  )
  it('refuses a model whose protocol route disappeared', async () => {
    const catalog = modelCatalogFixture()
    catalog.offers = []
    vi.mocked(api.get).mockImplementation(async (url) =>
      url === '/api/token/7' ? { ...nativeTokenFixture, id: 7 } : catalog
    )
    await expect(
      getTokenClientConfig(7, 'codex', 'fixture-model', catalog.server_address)
    ).rejects.toMatchObject({ code: 'model_unavailable' })
  })
  it('refuses an exhausted quota even before the native status has changed', async () => {
    vi.mocked(api.get).mockImplementation(async (url) =>
      url === '/api/token/7'
        ? {
            ...nativeTokenFixture,
            id: 7,
            unlimited_quota: false,
            remain_quota: 0,
          }
        : modelCatalogFixture()
    )
    await expect(
      getTokenClientConfig(
        7,
        'codex',
        'fixture-model',
        'https://api.example.test'
      )
    ).rejects.toMatchObject({ code: 'token_unavailable' })
  })
})
