import type { MarketPricing } from '@/types/modelMarket'

/** Native endpoint variants must remain distinguishable without a plugin name. */
export function pricingVariantLabel(
  pricing: Pick<MarketPricing, 'pluginName' | 'endpointTypes'>,
  fallback: string
): string {
  return pricing.pluginName || pricing.endpointTypes.join(' · ') || fallback
}
