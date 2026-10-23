import {
  PRICING_KEYS,
  isNumericPricingKey,
  parsePricingValues,
  pricingNumber,
  serializePricing,
  type ModelPricingConfig,
  type ModelPricingChange,
  type PricingValues,
  type PricingKey,
  type FetchedModelPrice,
} from '@/api/pricingConfig'
import { isRecord } from '@/api/contracts'
import { isDecimal, multiplyDecimal, divideDecimal } from '@/utils/decimal'
import { modelRatioToPrice, priceToModelRatio } from '@/utils/quota'

export type PricingDraft = Record<string, PricingValues>
export type PricingMode = 'tiered_expr' | 'per-token' | 'per-request'
export const TOKEN_LANES = [
  { key: 'CompletionRatio', label: 'output' },
  { key: 'CacheRatio', label: 'cache' },
  { key: 'CreateCacheRatio', label: 'cacheWrite' },
  { key: 'ImageRatio', label: 'image' },
  { key: 'AudioRatio', label: 'audio' },
  { key: 'AudioCompletionRatio', label: 'audioOutput' },
] as const
export function clonePricing<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}
export function decimalText(value: string | number): string {
  const text = String(value)
  if (!/[eE]/.test(text)) return text
  const [coefficient = '', exponent = '0'] = text.toLowerCase().split('e')
  const [whole = '', fraction = ''] = coefficient.split('.')
  const digits = whole + fraction,
    point = whole.length + Number(exponent)
  if (point <= 0) return `0.${'0'.repeat(-point)}${digits}`
  if (point >= digits.length) return digits + '0'.repeat(point - digits.length)
  return `${digits.slice(0, point)}.${digits.slice(point)}`
}
export function pricingMode(value: PricingValues): PricingMode {
  return value['billing_setting.billing_mode'] === 'tiered_expr'
    ? 'tiered_expr'
    : value.ModelPrice !== undefined
      ? 'per-request'
      : 'per-token'
}
// Drafts may hold decimal text while the backend returns numbers, so change
// detection compares the serialized request values.
export function priceFingerprint(value: PricingValues): string {
  const serialized = serializePricing(value)
  return JSON.stringify(
    PRICING_KEYS.map((key) => [
      key,
      key === 'billing_setting.plugin_billing_expr'
        ? Object.entries(serialized[key] ?? {}).sort(([a], [b]) =>
            a.localeCompare(b)
          )
        : serialized[key],
    ])
  )
}
export function initialPricingDraft(config: ModelPricingConfig): PricingDraft {
  return Object.fromEntries(
    config.entries
      .filter((entry) => Object.keys(entry.configured).length)
      .map((entry) => [entry.model_name, clonePricing(entry.configured)])
  )
}
export function pricingChanges(
  config: ModelPricingConfig,
  draft: PricingDraft
): ModelPricingChange[] {
  const baseline = new Map(
    config.entries.map((entry) => [entry.model_name, entry])
  )
  return [...new Set([...baseline.keys(), ...Object.keys(draft)])].flatMap(
    (model_name) => {
      const previous = baseline.get(model_name)
      const pricing = draft[model_name] ?? {}
      if (
        priceFingerprint(previous?.configured ?? {}) ===
        priceFingerprint(pricing)
      )
        return []
      return [
        {
          model_name,
          expected_version: previous?.version ?? config.empty_version,
          pricing: clonePricing(pricing),
          ...(!Object.keys(pricing).length ? { reset: true } : {}),
        },
      ]
    }
  )
}
export function fillPriceDraft(
  draft: PricingDraft,
  rows: FetchedModelPrice[]
): PricingDraft {
  const names = new Set<string>()
  for (const row of rows) {
    if (row.unsupported_reason || !Object.keys(row.pricing).length)
      throw new Error('unsupportedPrice')
    if (names.has(row.model_name)) throw new Error('duplicate')
    names.add(row.model_name)
  }
  return {
    ...draft,
    ...Object.fromEntries(
      rows.map((row) => [row.model_name, clonePricing(row.pricing)])
    ),
  }
}
// JSON mode shows the same numbers the save request sends.
export function pricingJSON(draft: PricingDraft): Record<PricingKey, string> {
  const serialized = Object.entries(draft).map(
    ([name, pricing]) => [name, serializePricing(pricing)] as const
  )
  return Object.fromEntries(
    PRICING_KEYS.map((key) => [
      key,
      JSON.stringify(
        Object.fromEntries(
          serialized
            .filter(([, pricing]) => pricing[key] !== undefined)
            .map(([name, pricing]) => [name, pricing[key]])
        ),
        null,
        2
      ),
    ])
  ) as Record<PricingKey, string>
}
export function parsePricingJSON(
  raw: Record<PricingKey, string>
): PricingDraft {
  const result: PricingDraft = Object.create(null) as PricingDraft
  for (const key of PRICING_KEYS) {
    const values: unknown = JSON.parse(raw[key])
    if (!isRecord(values)) throw new Error('badJson')
    for (const [name, value] of Object.entries(values)) {
      if (!name.trim()) throw new Error('unknownModel')
      // Prices must be finite, non-negative JSON numbers, as in the save request.
      if (isNumericPricingKey(key))
        pricingNumber(typeof value === 'number' ? value : Number.NaN)
      Object.assign((result[name] ??= {}), parsePricingValues({ [key]: value }))
    }
  }
  return result
}
export function tokenPrices(value: PricingValues): Record<string, string> {
  const input =
    value.ModelRatio === undefined
      ? ''
      : modelRatioToPrice(decimalText(value.ModelRatio))
  const prices: Record<string, string> = { input }
  for (const lane of TOKEN_LANES) {
    const base = lane.key === 'AudioCompletionRatio' ? prices.AudioRatio : input
    prices[lane.key] =
      value[lane.key] === undefined || !base
        ? ''
        : multiplyDecimal(decimalText(value[lane.key]!), base)
  }
  return prices
}
export function tokenPricing(prices: Record<string, string>): PricingValues {
  if (!isDecimal(prices.input)) throw new Error('invalid')
  const value: PricingValues = {
    ModelRatio: priceToModelRatio(prices.input),
  }
  for (const lane of TOKEN_LANES) {
    const price = prices[lane.key]
    if (!price) continue
    if (!isDecimal(price)) throw new Error('invalid')
    const base =
      lane.key === 'AudioCompletionRatio' ? prices.AudioRatio : prices.input
    if (!base || !isDecimal(base) || /^0(?:\.0+)?$/.test(base)) {
      if (base && /^0(?:\.0+)?$/.test(price)) {
        value[lane.key] = '0'
        continue
      }
      throw new Error('dependency')
    }
    value[lane.key] = divideDecimal(price, base)
  }
  return value
}
export function validatePricing(value: PricingValues): void {
  if (!Object.keys(value).length) return
  if (pricingMode(value) === 'tiered_expr') {
    if (!String(value['billing_setting.billing_expr'] ?? '').trim())
      throw new Error('exprRequired')
  } else if (value.ModelPrice === undefined && value.ModelRatio === undefined) {
    if (!Object.keys(value['billing_setting.plugin_billing_expr'] ?? {}).length)
      throw new Error('invalid')
  }
  serializePricing(value)
}
