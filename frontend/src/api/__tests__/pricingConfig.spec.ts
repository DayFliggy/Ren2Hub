import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { pricingConfigApi, serializePricing } from '@/api/pricingConfig'
import {
  initialPricingDraft,
  pricingChanges,
  tokenPrices,
  tokenPricing,
} from '@/features/pricing/draft'
// Generated from the real handlers by controller/frontend_contract_test.go.
import pricingResponse from '../../../tests/fixtures/native/model-pricing.get.json'
import pricingPatch from '../../../tests/fixtures/native/model-pricing.patch.request.json'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), patch: vi.fn(), post: vi.fn() },
}))

describe('model pricing requests', () => {
  afterEach(() => vi.clearAllMocks())

  it('queries an exact model with its abort signal and keeps plugin metadata', async () => {
    const entry = {
      model_name: 'task-model',
      version: 'empty',
      configured: {},
      effective: {},
      usage_schema: { seconds: { type: 'number' } },
      plugin_variants: [
        { plugin_key: 'provider', plugin_name: 'Task provider' },
      ],
    }
    const response = { ...pricingResponse.data, entries: [entry] }
    vi.mocked(api.get).mockResolvedValueOnce(response)
    const controller = new AbortController()
    await expect(
      pricingConfigApi.loadEntry('task-model', controller.signal)
    ).resolves.toMatchObject(entry)
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/option/model_pricing',
      { model: 'task-model' },
      { signal: controller.signal }
    )
    for (const entries of [
      [],
      [{ ...entry, model_name: 'other-model' }],
      [entry, { ...entry, model_name: 'other-model' }],
    ]) {
      vi.mocked(api.get).mockResolvedValueOnce({ ...response, entries })
      await expect(pricingConfigApi.loadEntry('task-model')).rejects.toThrow(
        'Invalid API response'
      )
    }
  })

  it('reads provider pricing issues and rejects malformed notices', async () => {
    const issues = ['unconfigured', 'incompatible', 'stale'].map((reason) => ({
      model_name: 'shared-model',
      plugin_key: reason,
      plugin_name: reason,
      reason,
    }))
    vi.mocked(api.get).mockResolvedValueOnce(issues)
    const abort = new AbortController()
    await expect(pricingConfigApi.issues(abort.signal)).resolves.toEqual(issues)
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/option/model_pricing/issues',
      undefined,
      { signal: abort.signal }
    )
    vi.mocked(api.get).mockResolvedValueOnce(null)
    await expect(pricingConfigApi.issues()).resolves.toEqual([])
    for (const value of [
      {},
      [{ ...issues[0], reason: 'unknown' }],
      [{ ...issues[0], plugin_key: 7 }],
    ]) {
      vi.mocked(api.get).mockResolvedValueOnce(value)
      await expect(pricingConfigApi.issues()).rejects.toThrow(
        'Invalid API response'
      )
    }
    vi.mocked(api.get).mockRejectedValueOnce(new Error('pricing unavailable'))
    await expect(pricingConfigApi.issues()).rejects.toThrow(
      'pricing unavailable'
    )
  })

  it('sends editor prices as the numbers the backend accepts', async () => {
    vi.mocked(api.get).mockResolvedValue(pricingResponse.data)
    vi.mocked(api.patch).mockResolvedValue({
      updated_models: ['contract-chat'],
    })
    const config = await pricingConfigApi.load()
    const draft = initialPricingDraft(config)
    expect(pricingChanges(config, draft)).toEqual([])

    // The editor keeps decimal text: $6/M input and $24/M output.
    const prices = tokenPrices(draft['contract-chat']!)
    draft['contract-chat'] = tokenPricing({
      ...prices,
      input: '6',
      CompletionRatio: '24',
    })
    expect(draft['contract-chat']).toEqual({
      ModelRatio: '3',
      CompletionRatio: '4',
    })
    await pricingConfigApi.save(pricingChanges(config, draft))

    expect(api.patch).toHaveBeenCalledWith(
      '/api/option/model_pricing',
      pricingPatch
    )
  })

  it('sends numeric preview and convert bodies', async () => {
    vi.mocked(api.post).mockResolvedValue({ billing_details: {} })
    const pricing = { ModelPrice: '0.04', ModelRatio: '2.5' }
    await pricingConfigApi.preview('contract-image', pricing)
    await pricingConfigApi.convert('contract-image', pricing)
    const body = {
      model_name: 'contract-image',
      pricing: { ModelPrice: 0.04, ModelRatio: 2.5 },
    }
    expect(api.post).toHaveBeenCalledWith(
      '/api/option/model_pricing/preview',
      body,
      { signal: undefined }
    )
    expect(api.post).toHaveBeenCalledWith(
      '/api/option/model_pricing/convert',
      body,
      { signal: undefined }
    )
  })

  it('reads preview descriptions and rejects fields the backend never sends', async () => {
    vi.mocked(api.post).mockResolvedValueOnce({
      effective: { ModelRatio: 1.5, CreateCacheRatio: 1.25 },
      cache_write_mode: 'claude_ttl',
      billing_details: {
        image_count: true,
        request_rules: [{ condition: 'size == "1024x1792"', multiplier: 2 }],
      },
    })
    await expect(pricingConfigApi.preview('m', {})).resolves.toEqual({
      effective: { ModelRatio: 1.5, CreateCacheRatio: 1.25 },
      cache_write_mode: 'claude_ttl',
      billing_details: {
        image_count: true,
        request_rules: [{ condition: 'size == "1024x1792"', multiplier: 2 }],
      },
    })
    vi.mocked(api.post).mockResolvedValueOnce({
      billing_details: {},
      cache_write_mode: 'ttl',
    })
    await expect(pricingConfigApi.preview('m', {})).rejects.toThrow(
      'Invalid API response: /api/option/model_pricing/preview'
    )
    vi.mocked(api.post).mockResolvedValueOnce({ expression: 3 })
    await expect(pricingConfigApi.convert('m', {})).rejects.toThrow(
      'Invalid API response: /api/option/model_pricing/convert'
    )
  })

  it('fetches unchanged models for exchange-rate price adjustments', async () => {
    vi.mocked(api.post).mockResolvedValue({ prices: {}, test_results: [] })
    await pricingConfigApi.fetchUpstream({
      id: -100,
      kind: 'basellm',
      name: 'Official prices',
      base_url: 'https://prices.example',
    })

    expect(api.post).toHaveBeenCalledWith(
      '/api/ratio_sync/fetch',
      {
        upstreams: [
          {
            id: -100,
            name: 'Official prices',
            base_url: 'https://prices.example',
            endpoint: '/llm-metadata/api/newapi/ratio_config-v1-base.json',
          },
        ],
        include_unchanged: true,
      },
      { signal: undefined }
    )
  })

  it('rejects prices the backend would refuse before sending', () => {
    for (const ModelRatio of ['', ' ', '-1', 'abc', Number.NaN, -1])
      expect(() => serializePricing({ ModelRatio })).toThrow('invalid')
    expect(serializePricing({ ModelRatio: '0' })).toEqual({ ModelRatio: 0 })
  })
})
