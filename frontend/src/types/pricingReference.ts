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
export interface ReferencePrice {
  key: string
  amount: string
  unit: 'per_million_tokens' | 'per_call' | 'per_image'
  source: 'configured' | 'tier'
  label?: string
  condition?: string
  requestRules?: string
  pluginName?: string
}
