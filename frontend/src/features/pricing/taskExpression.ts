import { isDecimal } from '@/utils/decimal'
import { decimalText } from './draft'
import { parseTaskTiersFromExpr } from './lib/billing-expr'
import { flattenBinary } from './lib/billing-expression/display'
import { compileBillingExpression } from './lib/billing-expression/parser'
import type { ExpressionNode } from './lib/billing-expression/types'
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
function taskPriceExpression(
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

// `cond ? m : 1` multiplies the whole request: the shape of request rules and
// of taskConditionExpression.
function isMultiplierFactor(node: ExpressionNode): boolean {
  return (
    node.kind === 'conditional' &&
    node.yes.kind === 'literal' &&
    typeof node.yes.value === 'number' &&
    node.no.kind === 'literal' &&
    node.no.value === 1
  )
}

// A saved task expression the generator can rebuild: one single-tier price
// factor and the multipliers around it. A blank or literal source has nothing
// to keep. Any other shape returns null, since rebuilding it would drop a term
// such as image_count, a division or another tier.
function taskParts(source: string, schema: BillingUsageSchema | undefined) {
  const trimmed = source.trim()
  const prefix = trimmed.startsWith('v1:') ? 'v1:' : ''
  if (!trimmed) return { prefix, multipliers: [], price: null }
  const result = compileBillingExpression(trimmed)
  if (result.status !== 'ready') return null
  if (result.ast.kind === 'literal')
    return { prefix, multipliers: [], price: null }
  const text = (node: ExpressionNode) => trimmed.slice(node.start, node.end)
  const factors = flattenBinary(result.ast, '*')
  const priced = factors.filter((node) => !isMultiplierFactor(node))
  if (priced.length !== 1) return null
  const tiers = parseTaskTiersFromExpr(text(priced[0]!), schema)
  if (tiers.length !== 1 || tiers[0]!.constant) return null
  return {
    prefix,
    multipliers: factors.filter(isMultiplierFactor).map(text),
    price: tiers[0]!,
  }
}

// Replaces only the price of a saved task expression and keeps its multipliers,
// otherwise a matching request would be billed at the bare unit price.
export function repriceTaskExpression(
  source: string,
  prices: Record<string, string>,
  schema: BillingUsageSchema
): string {
  const parts = taskParts(source, schema)
  if (!parts) throw new Error('taskPriceUnsupported')
  const base = taskPriceExpression(prices, schema)
  if (!parts.multipliers.length) return `${parts.prefix}${base}`
  return compiled(
    `${parts.prefix}(${base}) * ${parts.multipliers.join(' * ')}`,
    'invalid'
  )
}

// The unit prices of a saved single-tier task expression, so the generator
// shows what is billed today. Other shapes stay editable as source only.
export function readTaskPrices(
  source: string,
  schema: BillingUsageSchema | undefined
): Record<string, string> {
  const price = taskParts(source, schema)?.price
  if (!price) return {}
  return Object.fromEntries(
    Object.entries(price.unitPrices).map(([field, value]) => [
      field,
      decimalText(value),
    ])
  )
}
