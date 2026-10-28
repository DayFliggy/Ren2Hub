// Absorbed from upstream web/src/features/system-settings/models/
// upstream-price-adjustment.ts: a source quoted in the site currency is
// converted back to the USD base price before it fills the draft. Only the
// base price and expression price leaves change; relative ratios never do.
import type { PricingValues } from '@/api/pricingConfig'
import {
  compareDecimal,
  divideDecimalSignificant,
  isDecimal,
  multiplyDecimal,
} from '@/utils/decimal'
import { decimalText } from './draft'
import { compileBillingExpression } from './lib/billing-expression/parser'
import {
  expressionDependencies,
  type ExpressionNode,
} from './lib/billing-expression/types'
import {
  parseVisualBillingDocument,
  serializeVisualBillingDocument,
} from './lib/billing-expression/visual'

export type PriceAdjustment = 'keep' | 'divide' | 'multiply'
export type PriceAdjustmentError = 'rate' | 'range' | 'unsupported' | 'unset'
export type AdjustedPrice =
  | { values: PricingValues; error?: never }
  | { values?: never; error: PriceAdjustmentError }

// Always derive from the immutable source quote, never from a previous result.
export function adjustPrice(
  values: PricingValues,
  mode: PriceAdjustment,
  rate: number | null
): AdjustedPrice {
  if (mode === 'keep') return { values }
  if (rate === null || !Number.isFinite(rate) || rate <= 0) {
    return { error: 'rate' }
  }
  if (values['billing_setting.billing_mode'] === 'tiered_expr') {
    return adjustExpression(values, mode, rate)
  }
  const field = values.ModelPrice !== undefined ? 'ModelPrice' : 'ModelRatio'
  const amount = values[field]
  if (amount === undefined || amount === '') return { error: 'unset' }
  const price = adjustPriceAmount(amount, mode, rate)
  if (price === null) return { error: 'range' }
  return { values: { ...values, [field]: price } }
}

function adjustExpression(
  values: PricingValues,
  mode: PriceAdjustment,
  rate: number
): AdjustedPrice {
  const source = values['billing_setting.billing_expr']
  if (typeof source !== 'string' || source === '') return { error: 'unset' }
  const compiled = compileBillingExpression(source)
  if (compiled.status !== 'ready') return { error: 'unsupported' }
  const stack: ExpressionNode[] = [compiled.ast]
  const patches: { start: number; end: number; text: string }[] = []
  while (stack.length) {
    const node = stack.pop()
    if (!node) break
    if (node.kind === 'conditional') {
      const calls = expressionDependencies(node.condition).functions
      if (calls.has('tier') || calls.has('fixed')) {
        return { error: 'unsupported' }
      }
      stack.push(node.yes, node.no)
      continue
    }
    if (node.kind === 'binary' && node.operator === '*') {
      const factor = [node.left, node.right].find(
        (candidate) =>
          (candidate.kind === 'variable' && candidate.name === 'image_count') ||
          compiled.requestRules.some((rule) => rule.node === candidate)
      )
      if (!factor) return { error: 'unsupported' }
      stack.push(factor === node.left ? node.right : node.left)
      continue
    }
    if (node.kind !== 'call' || node.name !== 'tier') {
      return { error: 'unsupported' }
    }
    // The shared visual adapter validates the whole price leaf and keeps zero
    // fields and source spans; nonlinear bodies are refused, not guessed.
    const document = parseVisualBillingDocument(
      source.slice(node.start, node.end)
    )
    if (!document || document.root.kind !== 'tier') {
      return { error: 'unsupported' }
    }
    const tier = document.root
    if (tier.billingUnit === 'request') {
      const price = adjustPriceAmount(tier.fixedPrice, mode, rate)
      if (price === null) return { error: 'range' }
      tier.fixedPrice = String(price)
    } else {
      for (const entry of tier.prices) {
        const price = adjustPriceAmount(entry.value, mode, rate)
        if (price === null) return { error: 'range' }
        entry.value = String(price)
      }
    }
    const serialized = serializeVisualBillingDocument(document)
    if (!serialized.ok) return { error: 'range' }
    patches.push({ start: node.start, end: node.end, text: serialized.source })
  }
  let expression = source
  for (const patch of patches.sort((a, b) => b.start - a.start)) {
    expression =
      expression.slice(0, patch.start) +
      patch.text +
      expression.slice(patch.end)
  }
  if (compileBillingExpression(expression).status !== 'ready') {
    return { error: 'unsupported' }
  }
  return { values: { ...values, 'billing_setting.billing_expr': expression } }
}

function adjustPriceAmount(
  amount: number | string,
  mode: PriceAdjustment,
  rate: number
): number | string | null {
  const amountText = exactDecimal(amount)
  const rateText = exactDecimal(rate)
  if (!amountText || !rateText || compareDecimal(rateText, '0') <= 0) {
    return null
  }
  let priceText: string
  try {
    priceText =
      mode === 'divide'
        ? divideDecimalSignificant(amountText, rateText)
        : multiplyDecimal(amountText, rateText)
  } catch {
    return null
  }
  if (
    !isDecimal(priceText) ||
    compareDecimal(priceText, '0') < 0 ||
    (compareDecimal(amountText, '0') > 0 &&
      compareDecimal(priceText, '0') === 0)
  ) {
    return null
  }
  const price = Number(priceText)
  if (!Number.isFinite(price) || (price === 0 && priceText !== '0')) return null
  return String(price) === priceText ? price : priceText
}

function exactDecimal(value: number | string): string | null {
  if (typeof value === 'number' && (!Number.isFinite(value) || value < 0)) {
    return null
  }
  const text = typeof value === 'string' ? value : decimalText(value)
  return isDecimal(text) ? text : null
}
