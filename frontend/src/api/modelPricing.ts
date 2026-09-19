import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { isDecimal } from '@/utils/decimal'

export const PRICE_UNITS = [
  'per_million_tokens',
  'per_thousand_calls',
  'per_call',
  'per_image',
  'per_second',
] as const
export const PRICE_QUANTITIES = [
  'p',
  'c',
  'cr',
  'cc',
  'cc1h',
  'img',
  'img_o',
  'ai',
  'ao',
  'calls',
  'images',
  'seconds',
] as const
export type PriceMode = 'unit_price' | 'expression' | 'free'
export interface UnitPrice {
  quantity: string
  unit: (typeof PRICE_UNITS)[number]
  price: string
}
export interface PriceVersion {
  id: number
  model: string
  mode: PriceMode
  prices: UnitPrice[]
  expression?: string
  created_at: number
  hash: string
}
export interface PriceSeed {
  prices: PriceVersion[]
  unconfigured: string[]
  source: string
}

export function validatePriceVersion(
  value: Pick<PriceVersion, 'mode' | 'prices' | 'expression'>
): void {
  if (!['unit_price', 'expression', 'free'].includes(value.mode))
    throw new Error('Invalid price mode')
  if (value.mode === 'free' && (value.prices.length || value.expression))
    throw new Error('Free mode cannot contain charges')
  if (value.mode === 'expression' && !value.expression?.trim())
    throw new Error('Expression is required')
  if (value.mode === 'unit_price' && (!value.prices.length || value.expression))
    throw new Error('Unit prices are required')
  if ((value.expression?.length ?? 0) > 16384)
    throw new Error('Expression is too long')
  const seen = new Set<string>()
  for (const price of value.prices) {
    if (
      !(PRICE_QUANTITIES as readonly string[]).includes(price.quantity) &&
      !/^tool:[a-zA-Z0-9_.:-]+$/.test(price.quantity)
    )
      throw new Error('Unknown quantity')
    if (!PRICE_UNITS.includes(price.unit) || !isDecimal(price.price))
      throw new Error('Invalid unit price')
    const tokenQuantity =
      !['calls', 'images', 'seconds'].includes(price.quantity) &&
      !price.quantity.startsWith('tool:')
    if (
      price.quantity.length > 128 ||
      (value.mode === 'expression' && tokenQuantity)
    )
      throw new Error('Expression owns token pricing')
    const expectedUnit = tokenQuantity
      ? 'per_million_tokens'
      : (
          {
            calls: 'per_call',
            images: 'per_image',
            seconds: 'per_second',
          } as Record<string, string>
        )[price.quantity]
    if (
      price.quantity.startsWith('tool:')
        ? !['per_call', 'per_thousand_calls'].includes(price.unit)
        : price.unit !== expectedUnit
    )
      throw new Error('Incompatible quantity and unit')
    if (seen.has(price.quantity)) throw new Error('Duplicate quantity')
    seen.add(price.quantity)
  }
}

export function parsePriceVersion(
  value: unknown,
  endpoint: string
): PriceVersion {
  if (!isRecord(value) || !Array.isArray(value.prices))
    invalidResponse(endpoint)
  const result: PriceVersion = {
    id: requiredStrictInteger(value.id, endpoint),
    model: requiredString(value.model, endpoint, false),
    mode: requiredString(value.mode, endpoint) as PriceMode,
    prices: value.prices.map((line) => {
      if (!isRecord(line)) invalidResponse(endpoint)
      return {
        quantity: requiredString(line.quantity, endpoint),
        unit: requiredString(line.unit, endpoint) as UnitPrice['unit'],
        price: requiredString(line.price, endpoint),
      }
    }),
    expression:
      value.expression === undefined
        ? undefined
        : requiredString(value.expression, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    hash: requiredString(value.hash, endpoint),
  }
  try {
    validatePriceVersion(result)
  } catch {
    invalidResponse(endpoint)
  }
  return result
}

export const modelPricingApi = {
  async load(signal?: AbortSignal): Promise<PriceVersion[]> {
    const endpoint = '/api/billing/prices'
    const value = await api.get(endpoint, undefined, { signal })
    if (!Array.isArray(value)) invalidResponse(endpoint)
    return value.map((row) => parsePriceVersion(row, endpoint))
  },
  async publish(
    price: PriceVersion,
    expectedVersion: number
  ): Promise<PriceVersion> {
    validatePriceVersion(price)
    const endpoint = '/api/billing/prices'
    return parsePriceVersion(
      await api.post(endpoint, { price, expected_version: expectedVersion }),
      endpoint
    )
  },
  async seed(signal?: AbortSignal): Promise<PriceSeed> {
    const endpoint = '/api/billing/prices/seed'
    const value = await api.get(endpoint, undefined, { signal })
    if (
      !isRecord(value) ||
      !Array.isArray(value.prices) ||
      !Array.isArray(value.unconfigured) ||
      value.unconfigured.some((name) => typeof name !== 'string')
    )
      invalidResponse(endpoint)
    return {
      prices: value.prices.map((row) => parsePriceVersion(row, endpoint)),
      unconfigured: value.unconfigured as string[],
      source: requiredString(value.source, endpoint),
    }
  },
}
