import { catalogPrices } from '@/api/publicCatalog'
import type { PricingModelContract } from '@/api/liveContracts'
import type { PriceSort, ReferencePrice } from '@/types/pricingReference'
import { compareDecimal, isDecimal, multiplyDecimal } from '@/utils/decimal'
import { decimalText } from './draft'
import { splitBillingExprAndRequestRules } from './lib/billing-expr'
import {
  readTokenTierChain,
  readTimeTokenPricing,
} from './lib/billing-expression/display'
import { compileBillingExpression } from './lib/billing-expression/parser'
import type { ExpressionNode } from './lib/billing-expression/types'

const tokenKeys: Record<string, string> = {
  p: 'input',
  c: 'output',
  cr: 'cacheRead',
  cc: 'cacheWrite',
  cc1h: 'cacheWrite1h',
  cc12h: 'cacheWrite12h',
  img: 'image',
  img_cr: 'imageCache',
  img_o: 'imageOutput',
  ai: 'audioInput',
  ao: 'audioOutput',
}

/** Read the source literal, never the parser's floating point display value. */
function exactLiteral(source: string, node: ExpressionNode): string | null {
  let text = source.slice(node.start, node.end).trim()
  if (
    node.kind === 'unary' &&
    node.operator === '-' &&
    node.operand.kind === 'literal' &&
    node.operand.value === 0
  ) {
    const operand = source.slice(node.operand.start, node.operand.end).trim()
    if (!/^(?:0+(?:\.0*)?|\.0+)(?:[eE][+-]?\d+)?$/.test(operand)) return null
    text = '0'
  }
  if (!/^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(text)) return null
  const exponent = text.match(/[eE]([+-]?\d+)$/)?.[1]
  if (exponent && Math.abs(Number(exponent)) > 128) return null
  text = decimalText(text)
    .replace(/^0+(?=\d)/, '')
    .replace(/^\./, '0.')
    .replace(/\.$/, '')
  return isDecimal(text) ? text : null
}

export function expressionReferencePrices(
  expression: string,
  ratio = '1'
): ReferencePrice[] {
  const { billingExpr, requestRuleExpr } =
    splitBillingExprAndRequestRules(expression)
  const compiled = compileBillingExpression(billingExpr)
  if (compiled.status !== 'ready') return []
  const tiers =
    readTokenTierChain(compiled.ast) ?? readTimeTokenPricing(billingExpr)?.tiers
  const first = tiers?.[0]
  if (!first) return []
  const meta = {
    source: 'tier' as const,
    label: first.label,
    condition:
      first.conditionText ||
      first.conditions.map((c) => `${c.var} ${c.op} ${c.value}`).join(' && '),
    requestRules: requestRuleExpr,
  }
  if (first.fixedPriceNode) {
    const literal = exactLiteral(billingExpr, first.fixedPriceNode)
    const amount = literal === null ? null : multiplyDecimal(literal, ratio)
    return amount === null || !isDecimal(amount)
      ? []
      : [
          {
            ...meta,
            key: 'request',
            amount,
            unit: first.imageCount ? 'per_image' : 'per_call',
          },
        ]
  }
  const prices: ReferencePrice[] = []
  for (const [variable, node] of Object.entries(first.priceNodes ?? {})) {
    const literal = exactLiteral(billingExpr, node)
    if (literal === null) return []
    const amount = multiplyDecimal(literal, ratio)
    if (!isDecimal(amount)) return []
    prices.push({
      ...meta,
      key: tokenKeys[variable] ?? variable,
      amount,
      unit: 'per_million_tokens',
    })
  }
  return prices
}

/** Task usage expressions are not token prices and have no native quote endpoint. */
export function publicPricingReferences(
  model: PricingModelContract
): ReferencePrice[] {
  const ratio = '1'
  if (model.billing_mode === 'tiered_expr') {
    if (Object.keys(model.billing_usage_schema ?? {}).length) return []
    return expressionReferencePrices(model.billing_expr ?? '', ratio)
  }
  return catalogPrices(model, 'M').map((line) => ({
    key: line.key,
    amount: multiplyDecimal(line.value, ratio),
    unit: line.unit as 'per_call' | 'per_million_tokens',
    source: 'configured',
  }))
}

function referenceSortKey(
  prices: ReferencePrice[],
  sort: PriceSort,
  audio: boolean
): { bucket: number; amount: string } | undefined {
  const dimension = sort.startsWith('call-')
    ? 'request'
    : sort.startsWith('output-')
      ? audio
        ? 'audioOutput'
        : 'output'
      : audio
        ? 'audioInput'
        : 'input'
  const price = prices.find((item) => item.key === dimension)
  if (!price) return undefined
  const bucket =
    dimension === 'request'
      ? ['per_call', 'per_image'].indexOf(price.unit)
      : audio
        ? 1
        : 0
  return { bucket, amount: price.amount }
}

export function compareReferencePrices(
  a: ReferencePrice[],
  b: ReferencePrice[],
  sort: PriceSort,
  audioA = false,
  audioB = false
): number {
  const left = referenceSortKey(a, sort, audioA),
    right = referenceSortKey(b, sort, audioB)
  if (!left || !right) return Number(!left) - Number(!right)
  return (
    left.bucket - right.bucket ||
    (sort.endsWith('-desc') ? -1 : 1) *
      compareDecimal(left.amount, right.amount)
  )
}
