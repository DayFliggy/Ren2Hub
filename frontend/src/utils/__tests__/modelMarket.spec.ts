import { describe, expect, it } from 'vitest'
import {
  buildModelMarketCatalog,
  compareMarketOffers,
  formatMarketRatio,
  normalizeMarketView,
} from '@/utils/modelMarket'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
describe('native model catalog projection', () => {
  it('uses already-scaled channel prices exactly once and preserves the baseline', () => {
    const input = modelCatalogFixture()
    const model = buildModelMarketCatalog(input).models[0]!
    expect(model.baselines[0]!.priceLines).toContainEqual({
      key: 'input',
      amount: '1.25',
      unit: 'per_million_tokens',
    })
    expect(model.offers[0]!.pricing.priceLines).toContainEqual({
      key: 'input',
      amount: '0.25',
      unit: 'per_million_tokens',
    })
    expect(model.endpoints[1]!.path).toBe('/v1/responses')
    expect(model.vendor).toBe('Fixture vendor')
  })
  it('keeps unknown prices distinct from free offers and sorts exact native prices', () => {
    const input = modelCatalogFixture()
    const model = buildModelMarketCatalog(input).models[0]!
    const priced = model.offers[0]!
    input.offers[0]!.price_status = 'unavailable'
    input.offers[0]!.pricing_values = {}
    const unknown = buildModelMarketCatalog(input).models[0]!.offers[0]!
    expect(unknown.pricing.billingMode).toBe('unconfigured')
    expect(unknown.pricing.priceLines).toEqual([])
    expect(compareMarketOffers(priced, unknown, 'default')).toBeLessThan(0)
  })
  it('preserves ratio precision and recognizes only supported view modes', () => {
    expect(formatMarketRatio('0.123456789012345678')).toBe(
      '0.123456789012345678×'
    )
    expect(normalizeMarketView('comparison')).toBe('comparison')
    expect(normalizeMarketView('old-grid')).toBe('models')
  })
})
