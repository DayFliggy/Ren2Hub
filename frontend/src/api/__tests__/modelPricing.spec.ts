import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  modelPricingApi,
  parsePriceVersion,
  validatePriceVersion,
  type PriceVersion,
} from '../modelPricing'
import { catalogPrices } from '../publicCatalog'
import {
  channelRatioDecimal,
  compareDecimal,
  multiplyDecimal,
} from '@/utils/decimal'

export const price: PriceVersion = {
  id: 3,
  model: 'test',
  mode: 'unit_price',
  prices: [
    {
      quantity: 'p',
      unit: 'per_million_tokens',
      price: '1.000000000000000001',
    },
    { quantity: 'c', unit: 'per_million_tokens', price: '0' },
  ],
  created_at: 1,
  hash: 'hash',
}
afterEach(() => vi.restoreAllMocks())
describe('immutable model pricing', () => {
  it('preserves explicit zero and decimal precision without a ratio tree', () => {
    expect(parsePriceVersion(price, '/prices')).toEqual(price)
    expect(multiplyDecimal(price.prices[0]!.price, '2')).toBe(
      '2.000000000000000002'
    )
    expect(compareDecimal('9007199254740993.1', '9007199254740993.09')).toBe(1)
    expect(multiplyDecimal('2.5', channelRatioDecimal(1e-7))).toBe('0.00000025')
  })
  it('rejects missing price, numeric price, unknown unit, duplicate dimensions and invalid modes', () => {
    for (const prices of [
      [{ quantity: 'p', unit: 'per_million_tokens', price: 1 }],
      [{ quantity: 'p', unit: 'unknown', price: '1' }],
      [price.prices[0], price.prices[0]],
      [],
    ])
      expect(() => parsePriceVersion({ ...price, prices }, '/prices')).toThrow()
    expect(() =>
      validatePriceVersion({ mode: 'expression', prices: [], expression: '' })
    ).toThrow()
    expect(() =>
      validatePriceVersion({ mode: 'free', prices: price.prices })
    ).toThrow()
    expect(
      parsePriceVersion({ ...price, mode: 'free', prices: [] }, '/prices').mode
    ).toBe('free')
  })
  it('publishes a new version with optimistic concurrency and no old option write', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ ...price, id: 4 })
    const published = await modelPricingApi.publish(price, 3)
    expect(post).toHaveBeenCalledWith('/api/billing/prices', {
      price,
      expected_version: 3,
    })
    expect(published.id).toBe(4)
  })
  it('keeps units distinct when switching the token display scale', () => {
    const row = {
      model_name: 'test',
      description: '',
      icon: '',
      tags: '',
      vendor_id: 0,
      owner_by: '',
      supported_endpoint_types: [],
      billing_mode: 'unit_price',
      channel_ratio_min: 1,
      channel_ratio_max: 1,
      price_version: {
        ...price,
        prices: [
          { quantity: 'p', unit: 'per_million_tokens' as const, price: '1' },
          { quantity: 'images', unit: 'per_image' as const, price: '0.02' },
        ],
      },
    }
    expect(catalogPrices(row, 2, 'K').map((line) => line.unit)).toEqual([
      'per_thousand_tokens',
      'per_image',
    ])
    expect(catalogPrices(row, 2, 'K').map((line) => line.value)).toEqual([
      '0.002',
      '0.04',
    ])
  })
})
