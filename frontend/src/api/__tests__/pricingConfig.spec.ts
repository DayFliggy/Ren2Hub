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
      body
    )
  })

  it('rejects prices the backend would refuse before sending', () => {
    for (const ModelRatio of ['', ' ', '-1', 'abc', Number.NaN, -1])
      expect(() => serializePricing({ ModelRatio })).toThrow('invalid')
    expect(serializePricing({ ModelRatio: '0' })).toEqual({ ModelRatio: 0 })
  })
})
