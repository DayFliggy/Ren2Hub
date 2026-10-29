import { isDecimal } from '@/utils/decimal'
import { decimalText } from './draft'
import { parseTaskTiersFromExpr } from './lib/billing-expr'
import { compileBillingExpression } from './lib/billing-expression/parser'
import type { BillingUsageSchema } from './types'

// NewAPI task expressions price token usage per million tokens; display.ts
// reads a token term back only in this scaled form.
const TOKEN_SCALE = 1000000

export interface TaskCondition {
  field: string
  operator: string
  value: string
}

function compiled(source: string, message: string): string {
  if (compileBillingExpression(source).status !== 'ready')
    throw new Error(message)
  return source
}

// One base tier with a unit price per numeric usage field.
export function taskPriceExpression(
  prices: Record<string, string>,
  schema: BillingUsageSchema
): string {
  const terms = Object.entries(prices).filter(([, value]) => value !== '')
  if (!terms.length || terms.some(([, value]) => !isDecimal(value)))
    throw new Error('invalid')
  const body = terms.map(([field, value]) => {
    const term = `u(${JSON.stringify(field)}) * ${value}`
    return schema[field]?.unit === 'token' ? `${term} / ${TOKEN_SCALE}` : term
  })
  return compiled(`tier("base", ${body.join(' + ')})`, 'invalid')
}

function predicate(condition: TaskCondition, schema: BillingUsageSchema) {
  const field = schema[condition.field]
  if (!field || condition.value === '') throw new Error('requestRuleInvalid')
  const numeric = !field.enum && field.type !== 'boolean'
  if (numeric && !isDecimal(condition.value)) throw new Error('invalid')
  const value =
    field.type === 'boolean'
      ? String(condition.value === 'true')
      : numeric
        ? condition.value
        : JSON.stringify(condition.value)
  return `u(${JSON.stringify(condition.field)}) ${condition.operator} ${value}`
}

// Multiplies the whole expression when every condition holds. The version
// prefix must stay at the very start of the source.
export function taskConditionExpression(
  source: string,
  conditions: TaskCondition[],
  multiplier: string,
  schema: BillingUsageSchema
): string {
  const trimmed = source.trim()
  if (!trimmed || !isDecimal(multiplier) || !conditions.length)
    throw new Error('requestRuleInvalid')
  const predicates = conditions.map((item) => predicate(item, schema))
  const prefix = trimmed.startsWith('v1:') ? 'v1:' : ''
  const body = trimmed.slice(prefix.length)
  return compiled(
    `${prefix}(${body}) * ((${predicates.join(' && ')}) ? ${multiplier} : 1)`,
    'requestRuleInvalid'
  )
}

// The unit prices of a saved single-tier task expression, so the generator
// shows what is billed today. Other shapes stay editable as source only.
export function readTaskPrices(
  source: string,
  schema: BillingUsageSchema | undefined
): Record<string, string> {
  const tiers = parseTaskTiersFromExpr(source, schema)
  if (tiers.length !== 1 || tiers[0]!.constant) return {}
  return Object.fromEntries(
    Object.entries(tiers[0]!.unitPrices).map(([field, price]) => [
      field,
      decimalText(price),
    ])
  )
}
