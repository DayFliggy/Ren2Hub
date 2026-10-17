import { invalidResponse, isRecord, requiredString } from './contracts'
import { isDecimal } from '@/utils/decimal'
import type { UsageReference } from '@/types/pricingReference'

export function parseUsageReference(
  value: unknown,
  endpoint: string
): UsageReference | undefined {
  if (value === undefined) return undefined
  if (
    !isRecord(value) ||
    !['available', 'unavailable'].includes(String(value.status))
  )
    invalidResponse(endpoint)
  const result: UsageReference = {
    status: value.status as UsageReference['status'],
  }
  for (const key of [
    'label',
    'plugin_key',
    'plugin_name',
    'matched_tier',
  ] as const) {
    if (value[key] !== undefined)
      result[key] = requiredString(value[key], endpoint)
  }
  if (value.facts !== undefined) {
    if (
      !isRecord(value.facts) ||
      Object.values(value.facts).some(
        (fact) => typeof fact !== 'string' && typeof fact !== 'boolean'
      )
    )
      invalidResponse(endpoint)
    result.facts = value.facts as Record<string, string | boolean>
  }
  if (result.status === 'available') {
    if (
      !isDecimal(value.amount_usd) ||
      !result.label ||
      !result.facts ||
      value.reason !== undefined
    )
      invalidResponse(endpoint)
    result.amount_usd = value.amount_usd
  } else {
    if (
      value.amount_usd !== undefined ||
      ![
        'missing_example',
        'invalid_example',
        'dynamic_context',
        'unsupported_expression',
        'calculation_failed',
      ].includes(String(value.reason))
    )
      invalidResponse(endpoint)
    result.reason = value.reason as UsageReference['reason']
  }
  return result
}
