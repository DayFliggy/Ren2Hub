export type PriceSort =
  | 'default'
  | 'name'
  | 'input-asc'
  | 'input-desc'
  | 'output-asc'
  | 'output-desc'
  | 'call-asc'
  | 'call-desc'
export const PRICE_SORTS: PriceSort[] = [
  'default',
  'name',
  'input-asc',
  'input-desc',
  'output-asc',
  'output-desc',
  'call-asc',
  'call-desc',
]
export interface UsageReference {
  status: 'available' | 'unavailable'
  amount_usd?: string
  label?: string
  facts?: Record<string, string | boolean>
  plugin_key?: string
  plugin_name?: string
  matched_tier?: string
  reason?:
    | 'missing_example'
    | 'invalid_example'
    | 'dynamic_context'
    | 'unsupported_expression'
    | 'calculation_failed'
}
export interface ReferencePrice {
  key: string
  amount: string
  unit: 'per_million_tokens' | 'per_call' | 'per_image' | 'per_example'
  source: 'configured' | 'tier' | 'usage_example'
  label?: string
  condition?: string
  requestRules?: string
  pluginName?: string
  facts?: Record<string, string | boolean>
}
