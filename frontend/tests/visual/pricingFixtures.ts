export function pricingOfferFixture(price: {
  id: number
  model: string
  prices: unknown[]
  flat_fee?: string
  [key: string]: unknown
}) {
  const version = { ...price }
  delete version.channel_id
  delete version.provenance
  return {
    offer_id: `offer-${price.model}-${price.id}`,
    priced_model: price.model,
    price_version: {
      ...version,
      contract_version: 3,
      calculation_version: 'billing-v3',
      result_unit: 'usd',
    },
    conditions: {},
    adjustments: [{ kind: 'channel_multiplier', multiplier: '1' }],
    display_prices: price.prices,
    ...(price.flat_fee ? { display_flat_fee: price.flat_fee } : {}),
    availability: { status: 'available' },
  }
}
