// No imports: Playwright specs load this module in Node, outside Vite.
export const PRICING_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
  'billing_setting.plugin_billing_expr',
] as const
export type PricingKey = (typeof PRICING_KEYS)[number]
