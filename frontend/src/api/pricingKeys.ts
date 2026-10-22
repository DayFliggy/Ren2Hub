// No imports: Playwright specs load this module in Node, outside Vite.
// The backend stores these keys as float64 and rejects strings (400).
export const NUMERIC_PRICING_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
] as const
export const PRICING_KEYS = [
  ...NUMERIC_PRICING_KEYS,
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
  'billing_setting.plugin_billing_expr',
] as const
export type PricingKey = (typeof PRICING_KEYS)[number]
export type NumericPricingKey = (typeof NUMERIC_PRICING_KEYS)[number]
