import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  MODEL_PRICE_KEYS,
  mergeModelPriceEdit,
  modelPriceFields,
  modelPricingApi,
  parseModelPriceOptions,
  type ModelPriceEdit,
  type ModelPriceMaps,
} from '../modelPricing'

function edit(overrides: Partial<ModelPriceEdit> = {}): ModelPriceEdit {
  const initial: ModelPriceMaps = Object.fromEntries(
    MODEL_PRICE_KEYS.map((key) => [key, {}])
  )
  initial.ModelRatio = { old: 2, other: 4 }
  initial.CompletionRatio = { old: 3 }
  initial.CacheRatio = { old: 0 }
  return {
    initial,
    originalName: 'old',
    name: 'old',
    fields: modelPriceFields(initial, 'old'),
    mode: 'per-token',
    dirty: false,
    ...overrides,
  }
}
afterEach(() => vi.restoreAllMocks())
describe('model pricing preservation', () => {
  it('strictly parses values and does not invent maps absent from the response', () => {
    expect(
      parseModelPriceOptions([{ key: 'ModelRatio', value: '{"a":0}' }])
    ).toEqual({ ModelRatio: { a: 0 } })
    for (const value of ['null', '[]', '{"a":"2"}', '{"a":-1}', '{'])
      expect(() =>
        parseModelPriceOptions([{ key: 'ModelRatio', value }])
      ).toThrow()
  })
  it('makes no price write for untouched metadata', () => {
    const request = edit()
    expect(mergeModelPriceEdit(request.initial, request)).toEqual({})
  })
  it('moves loaded pricing on rename and merges unrelated fresh edits', () => {
    const request = edit({ name: 'new' })
    const patch = mergeModelPriceEdit(
      { ...request.initial, ModelRatio: { old: 2, other: 9, latest: 6 } },
      request
    )
    expect(JSON.parse(patch.ModelRatio!)).toEqual({
      other: 9,
      latest: 6,
      new: 2,
    })
    expect(JSON.parse(patch.CacheRatio!)).toEqual({ new: 0 })
  })
  it('rejects target conflicts and concurrent source price changes', () => {
    const request = edit({ name: 'new' })
    expect(() =>
      mergeModelPriceEdit(
        { ...request.initial, ModelPrice: { new: 10 } },
        request
      )
    ).toThrow('priceTargetConflict')
    expect(() =>
      mergeModelPriceEdit(
        { ...request.initial, ModelRatio: { old: 5 } },
        request
      )
    ).toThrow('priceConcurrentChange')
  })
  it('removes inactive or explicitly cleared fields but preserves unloaded options', () => {
    const request = edit({ dirty: true, mode: 'per-request' })
    request.fields.ModelPrice = '0.2'
    delete request.initial.AudioRatio
    const patch = mergeModelPriceEdit(
      { ...request.initial, AudioRatio: { old: 5 } },
      request
    )
    expect(JSON.parse(patch.ModelPrice!)).toEqual({ old: 0.2 })
    expect(JSON.parse(patch.ModelRatio!)).toEqual({ other: 4 })
    expect(patch.AudioRatio).toBeUndefined()
    request.mode = 'per-token'
    request.fields.CacheRatio = ''
    expect(
      JSON.parse(mergeModelPriceEdit(request.initial, request).CacheRatio!)
    ).toEqual({})
  })
  it('preserves existing prices when a new unpriced model is created', () => {
    const request = edit({ originalName: '', name: 'existing' })
    expect(
      mergeModelPriceEdit({ ModelPrice: { existing: 9 } }, request)
    ).toEqual({})
  })
  it('reads current maps before one atomic bulk write and surfaces save failure', async () => {
    const request = edit({ dirty: true })
    request.fields.ModelRatio = '7'
    vi.spyOn(api, 'get').mockResolvedValue(
      MODEL_PRICE_KEYS.map((key) => ({
        key,
        value: JSON.stringify(request.initial[key]),
      }))
    )
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('save rejected'))
    await expect(modelPricingApi.save(request)).rejects.toThrow('save rejected')
    expect(put).toHaveBeenCalledExactlyOnceWith('/api/option/bulk', {
      options: { ModelRatio: '{"old":7,"other":4}' },
    })
    vi.mocked(api.get).mockRejectedValue(new Error('read rejected'))
    await expect(modelPricingApi.save(request)).rejects.toThrow('read rejected')
    expect(put).toHaveBeenCalledTimes(1)
  })
})
