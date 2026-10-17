import { describe, expect, it } from 'vitest'
import { compareMarketModels, normalizeMarketView } from '@/utils/modelMarket'
import { nativeMarketFixture } from './modelMarketFixtures'

describe('native model market projection', () => {
  it('keeps missing, unconfigured and explicitly free prices distinct', () => {
    const { catalog } = nativeMarketFixture()
    expect(catalog.missingPrices).toEqual(['broken', 'missing'])
    expect(
      catalog.models.find((model) => model.id === 'free')!.billingMode
    ).toBe('token')
    expect(
      catalog.models.find((model) => model.id === 'unconfigured')!.billingMode
    ).toBe('unconfigured')
    expect(catalog.models.find((model) => model.id === 'movie')!.types).toEqual(
      ['video']
    )
    expect(catalog.models.find((model) => model.id === 'fixed')!.types).toEqual(
      ['other']
    )
  })
  it('sorts comparable dimensions while leaving missing dimension prices last', () => {
    const { catalog } = nativeMarketFixture()
    const one = catalog.models.find((model) => model.id === 'chat-one')!
    const two = catalog.models.find((model) => model.id === 'chat-two')!
    const movie = catalog.models.find((model) => model.id === 'movie')!
    expect(compareMarketModels(one, two, 'input-asc', 'premium')).toBeLessThan(
      0
    )
    expect(
      compareMarketModels(one, two, 'input-desc', 'premium')
    ).toBeGreaterThan(0)
    expect(compareMarketModels(movie, one, 'input-desc', '')).toBeGreaterThan(0)
    expect(compareMarketModels(one, two, 'input-asc', 'auto')).toBe(
      compareMarketModels(one, two, 'name', 'auto')
    )
  })
  it('keeps pricing records intact instead of storing multiplied prices', () => {
    const { catalog, pricing } = nativeMarketFixture()
    const item = catalog.models.find((model) => model.id === 'chat-one')!
    expect(item.pricing).toBe(pricing.models[0])
    expect(item.pricing.model_ratio).toBe(0.5)
    expect(item.groups.map((group) => group.ratio)).toEqual([0.2, 2, 'auto'])
  })
  it('accepts the three restored view modes and safely handles stale saved values', () => {
    expect(
      ['models', 'channels', 'comparison', 'retired'].map(normalizeMarketView)
    ).toEqual(['models', 'channels', 'comparison', 'models'])
  })
})
