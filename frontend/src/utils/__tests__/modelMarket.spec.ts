import { describe, expect, it } from 'vitest'
import {
  buildModelMarketCatalog,
  compareMarketOffers,
  formatMarketRatio,
  normalizeMarketView,
  projectMarketPricing,
} from '@/utils/modelMarket'
import { createModelMarketFixture } from '../../../tests/fixtures/modelMarket'

const priceMap = (price: ReturnType<typeof projectMarketPricing>) =>
  Object.fromEntries(price.priceLines.map((line) => [line.key, line.amount]))

describe('model market price projection', () => {
  it('applies channel ratio exactly once without changing the platform baseline', () => {
    const response = createModelMarketFixture()
    response.models[0]!.baselines[0]!.values.ModelRatio =
      '0.1234567890123456789'
    const model = buildModelMarketCatalog(response).models[0]!
    expect(priceMap(model.baselines[0]!)).toMatchObject({
      input: '0.2469135780246913578',
      output: '0.9876543120987654312',
    })
    expect(priceMap(model.offers[0]!.pricing)).toMatchObject({
      input: '0.04938271560493827156',
      output: '0.19753086241975308624',
    })
    expect(model.offers[0]!.baseline).toEqual(model.baselines[0])
    expect(response.models[0]!.baselines[0]!.values.ModelRatio).toBe(
      '0.1234567890123456789'
    )
  })

  it('distinguishes zero, omitted and unconfigured cache prices', () => {
    const fixture = createModelMarketFixture()
    const raw = fixture.models[1]!.baselines[0]!
    expect(priceMap(projectMarketPricing(raw, '0.2'))).toMatchObject({
      cacheRead: '0.06',
      cacheWrite: '0',
    })
    const withoutWrite = {
      ...raw,
      values: { ModelRatio: '1.5', CacheRatio: '0.1' },
    }
    expect(priceMap(projectMarketPricing(withoutWrite))).toEqual({
      input: '3',
      cacheRead: '0.3',
    })
    expect(
      projectMarketPricing({ ...raw, configured: false }).priceLines
    ).toEqual([])
    expect(
      priceMap(projectMarketPricing({ ...raw, cache_write_mode: 'none' }))
    ).not.toHaveProperty('cacheWrite')
    expect(
      priceMap(
        projectMarketPricing(
          {
            ...raw,
            values: { ModelRatio: '1.5', CreateCacheRatio: '1.25' },
            cache_write_mode: 'claude_ttl',
          },
          '0.2'
        )
      )
    ).toMatchObject({ cacheWrite: '0.75', cacheWrite1h: '1.2' })
  })

  it('keeps audio dimensions distinct and applies audio output to the audio base', () => {
    const audio = buildModelMarketCatalog(
      createModelMarketFixture()
    ).models.find((model) => model.id === 'audio-preview')!
    expect(audio.type).toBe('audio')
    expect(priceMap(audio.baselines[0]!)).toEqual({
      input: '5',
      output: '20',
      audioInput: '40',
      audioOutput: '80',
    })
    expect(priceMap(audio.offers[0]!.pricing)).toEqual({
      input: '1',
      output: '4',
      audioInput: '8',
      audioOutput: '16',
    })
  })

  it('does not present text cache and image factors as native audio charges', () => {
    const raw = createModelMarketFixture().models[0]!.baselines[0]!
    raw.values.ImageRatio = '2'
    raw.supported_endpoint_types = ['audio']
    const audio = priceMap(projectMarketPricing(raw))
    expect(audio).toHaveProperty('audioInput', '2.5')
    expect(audio).not.toHaveProperty('cacheRead')
    expect(audio).not.toHaveProperty('cacheWrite')
    expect(audio).not.toHaveProperty('image')
    // Plugin usage rules cannot be inferred from the URL they intercept.
    const plugin = priceMap(
      projectMarketPricing({ ...raw, plugin_key: 'task' })
    )
    expect(plugin).toHaveProperty('cacheRead', '0.625')
    expect(plugin).not.toHaveProperty('audioInput')
  })

  it.each(['openai-audio', 'realtime'])(
    'projects default audio factors for %s without inventing missing text output',
    (endpoint) => {
      const fixture = createModelMarketFixture()
      const model = fixture.models[0]!
      model.supported_endpoint_types = [endpoint]
      model.baselines[0]!.supported_endpoint_types = [endpoint]
      model.baselines[0]!.values = { ModelRatio: '2' }
      fixture.offers
        .filter((offer) => offer.model_id === model.id)
        .forEach((offer) => {
          offer.supported_endpoint_types = [endpoint]
        })
      const projected = buildModelMarketCatalog(fixture).models[0]!
      expect(projected.type).toBe('audio')
      expect(priceMap(projected.baselines[0]!)).toEqual({
        input: '4',
        audioInput: '4',
        audioOutput: '4',
      })
      expect(priceMap(projected.offers[0]!.pricing)).toEqual({
        input: '0.8',
        audioInput: '0.8',
        audioOutput: '0.8',
      })
    }
  )

  it('keeps each plugin/endpoint variant attached to its matching baseline and expression', () => {
    const video = buildModelMarketCatalog(
      createModelMarketFixture()
    ).models.find((model) => model.id === 'video-preview')!
    const dynamic = video.offers.find(
      (offer) => offer.pricing.pluginKey === 'seconds'
    )!
    const fixed = video.offers.find(
      (offer) => offer.pricing.pluginKey === 'fixed'
    )!
    expect(dynamic.pricing).toMatchObject({
      billingMode: 'dynamic',
      expression: 'u("seconds") * 0.2',
      priceLines: [],
      usageSchema: { seconds: { type: 'number', unit: 'second' } },
    })
    expect(dynamic.baseline.id).toBe('video-preview:seconds')
    expect(dynamic.endpointTypes).toEqual(['openai-video'])
    expect(fixed.pricing).toMatchObject({
      billingMode: 'per_call',
      priceLines: [{ key: 'request', amount: '0.016', unit: 'per_call' }],
    })
    expect(fixed.baseline.priceLines[0]!.amount).toBe('0.08')
    expect(fixed.endpointTypes).toEqual(['video-task'])
  })

  it('keeps a per-image price out of per-request comparisons', () => {
    const fixture = createModelMarketFixture()
    fixture.models.find(
      (model) => model.id === 'image-preview'
    )!.baselines[0]!.billing_details = { image_count: true }
    const catalog = buildModelMarketCatalog(fixture)
    const image = catalog.models.find((model) => model.id === 'image-preview')!
      .offers[0]!
    const request = catalog.models
      .find((model) => model.id === 'video-preview')!
      .offers.find((offer) => offer.pricing.billingMode === 'per_call')!
    expect(image.pricing.priceLines).toEqual([
      { key: 'request', amount: '0.008', unit: 'per_image' },
    ])
    expect(request.pricing.priceLines[0]!.amount).toBe('0.016')
    expect(compareMarketOffers(image, request, 'call')).toBeGreaterThan(0)
  })

  it('compares only matching price dimensions and units while placing unknown prices last', () => {
    const models = buildModelMarketCatalog(createModelMarketFixture()).models
    const offer = (name: string) =>
      models.find((model) => model.id === name)!.offers[0]!
    const ordered = [
      offer('image-preview'),
      offer('audio-preview'),
      offer('gpt-4.1'),
      offer('unpriced-preview'),
    ]
    expect(
      [...ordered]
        .sort((a, b) => compareMarketOffers(a, b, 'input'))
        .map((item) => item.modelId)
    ).toEqual(['gpt-4.1', 'audio-preview', 'image-preview', 'unpriced-preview'])
    expect(
      [...ordered].sort((a, b) => compareMarketOffers(a, b, 'call'))[0]!.modelId
    ).toBe('image-preview')
    expect(
      compareMarketOffers(
        offer('unpriced-preview'),
        offer('gpt-4.1'),
        'default'
      )
    ).toBeGreaterThan(0)
  })

  it.each(['grid', 'list', '', null, 'unknown'])(
    'migrates old/invalid view %s to the model view',
    (value) => {
      expect(normalizeMarketView(value)).toBe('models')
    }
  )
})

describe('market ratio labels', () => {
  it.each([
    ['1', '1.0×'],
    ['1.0000', '1.0×'],
    ['0', '0.0×'],
    ['0.2000', '0.2×'],
    ['0.1234567890123456789', '0.1234567890123456789×'],
    ['9007199254740993.001', '9007199254740993.001×'],
  ])(
    'formats %s without rounding or floating point conversion',
    (value, label) => {
      expect(formatMarketRatio(value)).toBe(label)
    }
  )
})
