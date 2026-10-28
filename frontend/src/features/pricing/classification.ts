import type { PricingModelContract } from '@/api/liveContracts'
import { compileBillingExpression } from './lib/billing-expression/parser'
import { visitExpression } from './lib/billing-expression/types'
import type { ExpressionNode } from './lib/billing-expression/types'

export type PricingCatalogMode = 'task' | 'request' | 'token' | 'unconfigured'

function isFixedTierPrice(node: ExpressionNode | undefined): boolean {
  return (
    node?.kind === 'call' && node.name === 'fixed' && node.args.length === 1
  )
}

function expressionMode(expression: string): 'request' | 'token' | null {
  const compiled = compileBillingExpression(expression)
  if (compiled.status !== 'ready') return null

  const tierPrices: ExpressionNode[] = []
  visitExpression(compiled.ast, (node) => {
    if (node.kind === 'call' && node.name === 'tier') {
      const price = node.args[1]
      if (price) tierPrices.push(price)
    }
  })

  if (tierPrices.length) {
    if (tierPrices.every(isFixedTierPrice)) return 'request'
    return 'token'
  }

  if ([...compiled.variables].some((variable) => variable !== 'image_count'))
    return 'token'
  return null
}

/** Classify pricing with task schemas taking precedence over expression shape. */
export function pricingCatalogMode(
  model: PricingModelContract
): PricingCatalogMode {
  if (Object.keys(model.billing_usage_schema ?? {}).length) return 'task'
  if (model.billing_mode === 'unconfigured') return 'unconfigured'

  if (model.billing_mode === 'tiered_expr') {
    const mode = expressionMode(model.billing_expr ?? '')
    if (mode) return mode
  }

  return model.quota_type === 1 ? 'request' : 'token'
}

/** Counts the available categories using the same classifier as filtering. */
export function countPricingCatalogModes(
  models: PricingModelContract[]
): Record<PricingCatalogMode, number> {
  const counts: Record<PricingCatalogMode, number> = {
    task: 0,
    request: 0,
    token: 0,
    unconfigured: 0,
  }
  for (const model of models) counts[pricingCatalogMode(model)]++
  return counts
}

export function filterPricingCatalogByMode(
  models: PricingModelContract[],
  mode: string
): PricingModelContract[] {
  return mode
    ? models.filter((model) => pricingCatalogMode(model) === mode)
    : models
}
