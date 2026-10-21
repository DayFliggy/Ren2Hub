import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { getModelMarket, parseModelMarket } from '@/api/modelMarket'
import { modelCatalogFixture } from './modelCatalogFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
beforeEach(() => vi.resetAllMocks())
describe('native model catalog', () => {
  it('reads the native catalog and retains exact prices and synthetic vendors', async () => {
    const input = modelCatalogFixture()
    input.models[0]!.pricing_values.input = '0.1234567890123456789'
    vi.mocked(api.get).mockResolvedValue(input)
    const signal = new AbortController().signal
    const result = await getModelMarket(signal)
    expect(api.get).toHaveBeenCalledWith('/api/user/model-catalog', undefined, {
      signal,
    })
    expect(result.models[0]!.vendor_id).toBe(-10)
    expect(result.models[0]!.pricing_values.input).toBe('0.1234567890123456789')
    expect(result.models[0]!.capabilities).toBeNull()
  })
  it('distinguishes configured zero prices from unavailable prices', () => {
    const input = modelCatalogFixture()
    input.models[0]!.price_status = 'zero'
    input.models[0]!.pricing_values = { input: '0', output: '0' }
    input.offers[0]!.price_status = 'unavailable'
    input.offers[0]!.pricing_values = {}
    const result = parseModelMarket(input)
    expect(result.models[0]!.pricing_values.input).toBe('0')
    expect(result.offers[0]!.price_status).toBe('unavailable')
    expect(result.offers[0]!.pricing_values).toEqual({})
  })
  it.each([
    [
      'duplicate model',
      (input: ReturnType<typeof modelCatalogFixture>) =>
        input.models.push({ ...input.models[0]! }),
    ],
    [
      'duplicate channel',
      (input: ReturnType<typeof modelCatalogFixture>) =>
        input.channels.push({ ...input.channels[0]! }),
    ],
    [
      'unknown channel',
      (input: ReturnType<typeof modelCatalogFixture>) => {
        input.offers[0]!.channel_id = 42
      },
    ],
    [
      'unknown model',
      (input: ReturnType<typeof modelCatalogFixture>) => {
        input.offers[0]!.model_id = 'forbidden'
      },
    ],
    [
      'negative price',
      (input: ReturnType<typeof modelCatalogFixture>) => {
        input.models[0]!.pricing_values.input = '-1'
      },
    ],
    [
      'missing configured price',
      (input: ReturnType<typeof modelCatalogFixture>) => {
        input.models[0]!.pricing_values = {}
      },
    ],
  ])('rejects %s instead of inventing a price', (_label, mutate) => {
    const input = modelCatalogFixture()
    mutate(input)
    expect(() => parseModelMarket(input)).toThrow('Invalid API response')
  })
})
