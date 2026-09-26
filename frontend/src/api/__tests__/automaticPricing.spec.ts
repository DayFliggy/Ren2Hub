import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  automaticPricingApi,
  parseAutomaticPricingStatus,
} from '../automaticPricing'

const status = {
  config: { enabled: true, check_interval_minutes: 30 },
  sources: [
    {
      source: 'sub2api',
      role: 'primary',
      url: 'https://example.com/prices.json',
      hash: 'abc',
      checked_at: 1,
      updated_at: 1,
      last_error: '',
      model_count: 2,
      rejected_count: 1,
    },
  ],
  origins: [
    {
      model: 'test',
      version_id: 3,
      origin: 'manual',
      source_model: '',
      source_hash: '',
    },
  ],
}
afterEach(() => vi.restoreAllMocks())
describe('automatic price contract', () => {
  it('loads source provenance and strictly rejects invalid configuration, origins and counts', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(status)
    const controller = new AbortController()
    expect(await automaticPricingApi.status(controller.signal)).toEqual(status)
    expect(get).toHaveBeenCalledWith(
      '/api/billing/prices/automatic',
      undefined,
      { signal: controller.signal }
    )
    for (const invalid of [
      { ...status, config: { enabled: 'true', check_interval_minutes: 30 } },
      ...[4, 1441, 5.5, '30'].map((minutes) => ({
        ...status,
        config: { enabled: true, check_interval_minutes: minutes },
      })),
      { ...status, sources: [{ ...status.sources[0], model_count: -1 }] },
      { ...status, sources: [{ ...status.sources[0], rejected_count: '2' }] },
      { ...status, sources: [{ ...status.sources[0], source: 'unknown' }] },
      {
        ...status,
        sources: [{ ...status.sources[0], snapshot_state: 'fresh' }],
      },
      { ...status, sources: [...status.sources, ...status.sources] },
      { ...status, origins: [{ ...status.origins[0], version_id: 0 }] },
      { ...status, origins: [{ ...status.origins[0], origin: 'unknown' }] },
      { ...status, origins: [...status.origins, ...status.origins] },
    ])
      expect(() => parseAutomaticPricingStatus(invalid)).toThrow(
        'Invalid API response'
      )
  })
  it('keeps retired sources readable in provenance but rejects them as active catalogs', () => {
    for (const source of ['models_dev', 'openrouter']) {
      const historical = {
        ...status,
        origins: [{ ...status.origins[0], origin: source }],
      }
      expect(parseAutomaticPricingStatus(historical)).toEqual(historical)
      expect(() =>
        parseAutomaticPricingStatus({
          ...status,
          sources: [{ ...status.sources[0], source }],
        })
      ).toThrow('Invalid API response')
    }
  })
  it('retains every explicit snapshot state without inventing one for older responses', () => {
    for (const snapshot_state of [
      'bundled',
      'current',
      'cached',
      'unavailable',
    ]) {
      const response = {
        ...status,
        sources: [{ ...status.sources[0], snapshot_state }],
      }
      expect(parseAutomaticPricingStatus(response)).toEqual(response)
    }
    expect(parseAutomaticPricingStatus(status).sources[0]).not.toHaveProperty(
      'snapshot_state'
    )
  })
  it('saves disabled schedules and parses asynchronous sync tasks', async () => {
    const config = { enabled: false, check_interval_minutes: 5 }
    const put = vi.spyOn(api, 'put').mockResolvedValue(config)
    expect(await automaticPricingApi.configure(config)).toEqual(config)
    expect(put).toHaveBeenCalledWith('/api/billing/prices/automatic', config)
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      task_id: 'task',
      type: 'billing_pricing_sync',
      status: 'pending',
      created_at: 1,
      updated_at: 1,
    })
    expect(await automaticPricingApi.sync()).toMatchObject({
      task_id: 'task',
      status: 'pending',
    })
    expect(post).toHaveBeenCalledWith('/api/billing/prices/automatic/sync')
  })
  it('restores a price with CAS and preserves exact decimal text', async () => {
    const price = {
      contract_version: 2,
      calculation_version: 'billing-v2',
      result_unit: 'usd',
      id: 4,
      model: 'test',
      mode: 'unit_price',
      prices: [
        {
          quantity: 'p',
          unit: 'per_million_tokens',
          price: '1.000000000000000001',
        },
      ],
      created_at: 1,
      hash: 'hash',
    }
    const post = vi.spyOn(api, 'post').mockResolvedValue(price)
    expect(await automaticPricingApi.restore('test', 3)).toEqual(price)
    expect(post).toHaveBeenCalledWith('/api/billing/prices/automatic/restore', {
      model: 'test',
      expected_version: 3,
    })
  })
})
