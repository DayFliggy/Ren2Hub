export type BillingUsageUnit = 'second' | 'count' | 'token' | 'credit'
export interface BillingUsageFieldSchema {
  type?: 'number' | 'boolean'
  unit?: BillingUsageUnit
  unitLabel?: string | Record<string, string>
  enum?: string[]
  enumLabels?: Record<string, string | Record<string, string>>
  description?: string | Record<string, string>
}
export type BillingUsageSchema = Record<string, BillingUsageFieldSchema>
export interface BillingUsageExample {
  label: string
  facts: Record<string, string | number | boolean>
}
