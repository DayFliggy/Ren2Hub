import { offerFixture } from './pricingFixture'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  modelPricingApi,
  parsePriceVersion,
  validatePriceVersion,
  type PriceVersion,
} from '../modelPricing'
import { catalogPrices } from '../publicCatalog'
import { compareDecimal, multiplyDecimal } from '@/utils/decimal'

export const price: PriceVersion = {
  contract_version: 3,
  calculation_version: 'billing-v3',
  result_unit: 'usd',
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
  it('requires explicit character metering and a reviewed reservation for provider facts', () => {
    const speech: PriceVersion = {
      ...price,
      prices: [
        { quantity: 'characters', unit: 'per_million_characters', price: '15' },
      ],
      metering: {
        request_kind: 'speech',
        quantity: 'characters',
        source: 'request_fixed',
        method: 'unicode_codepoints',
      },
    }
    expect(parsePriceVersion(speech, '/prices').metering).toEqual(
      speech.metering
    )
    expect(() =>
      parsePriceVersion({ ...speech, metering: undefined }, '/prices')
    ).toThrow()
    const transcription: PriceVersion = {
      ...price,
      prices: [{ quantity: 'seconds', unit: 'per_minute', price: '0.006' }],
      metering: {
        request_kind: 'transcription',
        quantity: 'seconds',
        source: 'completion_fact',
        method: 'provider_duration',
        reservation_quantity: '600',
      },
    }
    expect(parsePriceVersion(transcription, '/prices').metering).toEqual(
      transcription.metering
    )
    expect(() =>
      parsePriceVersion(
        {
          ...transcription,
          metering: {
            ...transcription.metering,
            reservation_quantity: undefined,
          },
        },
        '/prices'
      )
    ).toThrow()
  })
  it('accepts an explicit flat fee without unit prices and rejects a missing charge definition', () => {
    const flatOnly = { ...price, flat_fee: '0.125', prices: [] }
    expect(parsePriceVersion(flatOnly, '/prices')).toEqual(flatOnly)
    expect(() =>
      parsePriceVersion({ ...flatOnly, flat_fee: undefined }, '/prices')
    ).toThrow()
  })
  it('retains channel identity, exact tier rates, reasoning and effective windows', () => {
    const scoped: PriceVersion = {
      ...price,
      scope_kind: 'channel_model',
      channel_id: 9,
      flat_fee: '0.000000000000000001',
      effective_from: '2026-01-01T00:00:00Z',
      effective_to: '2027-01-01T00:00:00Z',
      prices: [
        {
          quantity: 'reasoning',
          unit: 'per_million_tokens',
          price: '',
          tier_mode: 'graduated',
          tiers: [
            { up_to: '1000000', unit_price: '1.000000000000000001' },
            { unit_price: '0.5' },
          ],
        },
      ],
    }
    expect(parsePriceVersion(scoped, '/prices')).toEqual(scoped)
    for (const invalid of [
      { channel_id: 0 },
      { scope_kind: 'global_model' },
      { effective_to: '2025-01-01T00:00:00Z' },
      { effective_from: '2026-01-01' },
      { flat_fee: '0.0000000000000000001' },
      {
        prices: [
          {
            ...scoped.prices[0],
            tiers: [{ unit_price: '1' }, { up_to: '1', unit_price: '2' }],
          },
        ],
      },
      {
        prices: [
          {
            ...scoped.prices[0],
            tiers: [
              { up_to: '10', unit_price: '1' },
              { up_to: '5', unit_price: '2' },
            ],
          },
        ],
      },
    ])
      expect(() =>
        parsePriceVersion({ ...scoped, ...invalid }, '/prices')
      ).toThrow()
    const catalog = {
      model_name: scoped.model,
      description: '',
      icon: '',
      tags: '',
      vendor_id: 0,
      owner_by: '',
      supported_endpoint_types: [],
      offers: [offerFixture(scoped)],
    }
    expect(catalogPrices(catalog, 'K')[0]?.tiers).toEqual([
      { up_to: '1000000', unit_price: '0.001000000000000000001' },
      { unit_price: '0.0005' },
    ])
  })
  it('preserves embedding scope through parsing and publication, and rejects unknown scopes', async () => {
    const embedding: PriceVersion = { ...price, usage_scope: 'embedding' }
    expect(parsePriceVersion(embedding, '/prices')).toEqual(embedding)
    expect(parsePriceVersion(price, '/prices')).not.toHaveProperty(
      'usage_scope'
    )
    for (const usage_scope of ['chat', '', null, 1])
      expect(() =>
        parsePriceVersion({ ...price, usage_scope }, '/prices')
      ).toThrow()
    const post = vi.spyOn(api, 'post').mockResolvedValue(embedding)
    expect(await modelPricingApi.publish(embedding, 3)).toEqual(embedding)
    expect(post).toHaveBeenCalledWith('/api/billing/prices', {
      price: embedding,
      expected_version: 3,
    })
  })
  it('loads current task schema choices and carries explicit publication selection', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      model: 'test',
      plugin_key: '',
      usage_schema: null,
      manifest_schema: null,
      review_required: true,
      plugins: [
        { key: 'one', name: 'One', version: '2' },
        { key: 'two', name: 'Two', version: '3' },
      ],
    })
    expect((await modelPricingApi.taskSchema('test')).plugins).toHaveLength(2)
    expect(get).toHaveBeenCalledWith(
      '/api/billing/prices/task-schema',
      { model: 'test' },
      { signal: undefined }
    )
    const post = vi.spyOn(api, 'post').mockResolvedValue(price)
    await modelPricingApi.publish(price, 3, 'two')
    expect(post).toHaveBeenCalledWith('/api/billing/prices', {
      price,
      expected_version: 3,
      plugin_key: 'two',
    })
  })
  it('rejects persisted prices without the current contract', () => {
    expect(() =>
      parsePriceVersion({ ...price, contract_version: undefined }, '/prices')
    ).toThrow()
    expect(() =>
      parsePriceVersion(
        { ...price, calculation_version: 'billing-v1' },
        '/prices'
      )
    ).toThrow()
  })
  it('keeps task schemas and exact custom units when parsing prices', () => {
    const task = {
      ...price,
      usage_schema: {
        version: 1,
        hash: 'schema',
        fields: {
          credits: {
            type: 'number',
            unit: 'credit',
            source: 'completion_fact',
            required: true,
          },
        },
      },
      prices: [
        {
          quantity: 'credits',
          unit: 'per_credit',
          price: '0.000000000000000001',
        },
      ],
    }
    expect(parsePriceVersion(task, '/prices')).toEqual(task)
  })
  it('preserves explicit zero and decimal precision without a ratio tree', () => {
    expect(parsePriceVersion(price, '/prices')).toEqual(price)
    expect(multiplyDecimal(price.prices[0]!.price, '2')).toBe(
      '2.000000000000000002'
    )
    expect(compareDecimal('9007199254740993.1', '9007199254740993.09')).toBe(1)
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
      offers: [
        offerFixture({
          ...price,
          prices: [
            { quantity: 'p', unit: 'per_million_tokens' as const, price: '1' },
            { quantity: 'images', unit: 'per_image' as const, price: '0.02' },
          ],
        }),
      ],
    }
    expect(catalogPrices(row, 'K').map((line) => line.unit)).toEqual([
      'per_thousand_tokens',
      'per_image',
    ])
    expect(catalogPrices(row, 'K').map((line) => line.value)).toEqual([
      '0.001',
      '0.02',
    ])
  })
})
