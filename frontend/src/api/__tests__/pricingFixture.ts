import type { PricingOffer } from '@/api/liveContracts'
import type { PublicPriceVersion } from '@/api/modelPricing'

export function offerFixture(
  price: PublicPriceVersion,
  overrides: Partial<PricingOffer> = {}
): PricingOffer {
  return {
    offer_id: `offer-${price.id}`,
    priced_model: price.model,
    price_version: {
      contract_version: 3,
      calculation_version: 'billing-v3',
      result_unit: 'usd',
      ...price,
    },
    conditions: {},
    adjustments: [{ kind: 'channel_multiplier', multiplier: '1' }],
    display_prices: price.prices,
    ...(price.flat_fee ? { display_flat_fee: price.flat_fee } : {}),
    availability: { status: 'available' },
    ...overrides,
  }
}
