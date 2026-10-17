import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { publicCatalogApi } from '@/api/publicCatalog'
import { getModelMarket, parseMarketGroups } from '@/api/modelMarket'
import { nativeMarketFixture } from '@/utils/__tests__/modelMarketFixtures'

afterEach(() => vi.restoreAllMocks())
describe('native model market access', () => {
  it('requests only user scoped groups and model names and intersects public prices', async () => {
    const { pricing, groups } = nativeMarketFixture()
    vi.spyOn(publicCatalogApi, 'pricing').mockResolvedValue(pricing)
    const get = vi.spyOn(api, 'get').mockImplementation(async (path, query) => {
      if (path === '/api/user/self/groups')
        return Object.fromEntries(
          groups.map((group) => [
            group.name,
            { desc: group.description, ratio: group.ratio },
          ])
        )
      if (path === '/api/user/models')
        return groups.find((group) => group.name === query?.group)!.models
      throw new Error(`Forbidden endpoint: ${path}`)
    })
    const signal = new AbortController().signal
    const catalog = await getModelMarket(signal)
    expect(catalog.models.map((model) => model.id)).not.toContain(
      'private-only'
    )
    expect(
      catalog.models
        .find((model) => model.id === 'chat-two')!
        .groups.map((group) => group.name)
    ).toEqual(['premium', 'auto'])
    expect(get).toHaveBeenCalledTimes(4)
    expect(
      get.mock.calls.every(([, , options]) => options?.signal === signal)
    ).toBe(true)
    expect(
      pricing.models.find((model) => model.model_name === 'chat-one')!
        .model_ratio
    ).toBe(0.5)
  })
  it('does not fall back to unrestricted data when a group model request fails', async () => {
    vi.spyOn(publicCatalogApi, 'pricing').mockResolvedValue(
      nativeMarketFixture().pricing
    )
    vi.spyOn(api, 'get').mockImplementation(async (path) => {
      if (path === '/api/user/self/groups')
        return { member: { desc: '', ratio: 1 } }
      throw new Error('permission revoked')
    })
    await expect(getModelMarket()).rejects.toThrow('permission revoked')
  })
  it('returns no models for an empty group directory without a global model query', async () => {
    vi.spyOn(publicCatalogApi, 'pricing').mockResolvedValue(
      nativeMarketFixture().pricing
    )
    const get = vi.spyOn(api, 'get').mockResolvedValue({})
    expect((await getModelMarket()).models).toEqual([])
    expect(get).toHaveBeenCalledTimes(1)
  })
  it.each([
    null,
    [],
    { member: { ratio: -1, desc: '' } },
    { member: { ratio: '0.2', desc: '' } },
    { member: { ratio: 'auto', desc: '' } },
    { member: { ratio: Infinity, desc: '' } },
    { auto: { ratio: 1, desc: '' } },
  ])('rejects malformed group pricing %j', (value) => {
    expect(() => parseMarketGroups(value)).toThrow()
  })
})
