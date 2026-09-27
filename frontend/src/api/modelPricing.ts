import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredString,
  requiredBoolean,
} from './contracts'
import { compareDecimal, isDecimal } from '@/utils/decimal'
import { parseBillingCharge, type BillingCharge } from './billing'

export const PRICE_UNITS = [
  'per_million_tokens',
  'per_thousand_calls',
  'per_call',
  'per_image',
  'per_second',
  'per_minute',
  'per_hour',
  'per_character',
  'per_million_characters',
  'per_credit',
] as const
export const PRICE_QUANTITIES = [
  'p',
  'c',
  'cr',
  'cc',
  'cc1h',
  'img',
  'img_o',
  'img_cr',
  'ai',
  'ai_cr',
  'ao',
  'calls',
  'images',
  'seconds',
  'reasoning',
  'characters',
  'credits',
] as const
export const PRICE_SCOPE_KINDS = ['global_model', 'channel_model'] as const
export const PRICE_TIER_MODES = ['volume', 'graduated'] as const
export type PriceScopeKind = (typeof PRICE_SCOPE_KINDS)[number]
export type PriceTierMode = (typeof PRICE_TIER_MODES)[number]
export type PriceMode = 'unit_price' | 'expression' | 'free'
export interface PriceTier {
  up_to?: string
  unit_price: string
}
export interface UnitPrice {
  quantity: string
  unit: string
  price: string
  tier_mode?: PriceTierMode
  tiers?: PriceTier[]
}
export interface PriceProvenance {
  kind: 'source' | 'evidence' | 'retail'
  snapshot_id?: number
  parser_version?: string
  fields?: Array<{
    target: string
    source_path: string
    raw_value: string
    source_unit: string
    conversion: string
  }>
  retail_reason?: string
}
export interface PriceSupport {
  dimensions: string[]
  service_tiers: string[]
}
export interface PriceVersion {
  metering?: PriceMetering
  provenance?: PriceProvenance
  support?: PriceSupport
  contract_version?: 3
  calculation_version?: 'billing-v3'
  result_unit?: 'micro_usd' | 'usd'
  usage_schema?: UsageSchema
  usage_scope?: 'embedding'
  scope_kind?: PriceScopeKind
  channel_id?: number
  id: number
  model: string
  mode: PriceMode
  prices: UnitPrice[]
  flat_fee?: string
  expression?: string
  created_at: number
  effective_from?: string
  effective_to?: string
  hash: string
}
export interface PriceMetering {
  request_kind: 'speech' | 'transcription'
  quantity: 'characters' | 'seconds'
  source: 'request_fixed' | 'completion_fact'
  method: 'unicode_codepoints' | 'provider_usage' | 'provider_duration'
  reservation_quantity?: string
}
export function parsePriceMetering(
  value: unknown,
  endpoint: string
): PriceMetering {
  if (!isRecord(value)) invalidResponse(endpoint)
  const validSpeech =
    value.request_kind === 'speech' &&
    value.quantity === 'characters' &&
    ((value.source === 'request_fixed' &&
      value.method === 'unicode_codepoints') ||
      (value.source === 'completion_fact' && value.method === 'provider_usage'))
  const validTranscription =
    value.request_kind === 'transcription' &&
    value.quantity === 'seconds' &&
    value.source === 'completion_fact' &&
    value.method === 'provider_duration'
  if (!validSpeech && !validTranscription) invalidResponse(endpoint)
  if (
    value.source === 'completion_fact'
      ? !isDecimal(value.reservation_quantity) ||
        compareDecimal(value.reservation_quantity, '0') <= 0
      : value.reservation_quantity !== undefined
  )
    invalidResponse(endpoint)
  return {
    request_kind: value.request_kind as PriceMetering['request_kind'],
    quantity: value.quantity as PriceMetering['quantity'],
    source: value.source as PriceMetering['source'],
    method: value.method as PriceMetering['method'],
    ...(value.reservation_quantity === undefined
      ? {}
      : { reservation_quantity: value.reservation_quantity as string }),
  }
}
export interface UsageField {
  type: 'number' | 'string' | 'boolean'
  unit?: string
  source: 'request_fixed' | 'estimate_only' | 'completion_fact'
  required: boolean
  integer?: boolean
  minimum?: string
  maximum?: string
  enum?: string[]
}
export interface UsageSchema {
  version: 1
  fields: Record<string, UsageField>
  hash: string
}
export interface PricePreviewInput {
  price: PriceVersion
  usage: Record<string, unknown>
  attributes: Record<string, string | number | boolean>
  request_facts: Record<string, string | number | boolean>
  channel_ratio: string
  frozen_at: string
}
export interface PricePreview {
  quote: Record<string, unknown>
  charge: BillingCharge
}

export function parseUsageSchema(
  value: unknown,
  endpoint: string
): UsageSchema {
  if (!isRecord(value) || value.version !== 1 || !isRecord(value.fields))
    invalidResponse(endpoint)
  const fields: Record<string, UsageField> = {}
  if (
    !Object.keys(value.fields).length ||
    Object.keys(value.fields).length > 64
  )
    invalidResponse(endpoint)
  for (const [key, raw] of Object.entries(value.fields)) {
    if (
      !/^[a-z][a-z0-9_]{0,63}$/.test(key) ||
      !isRecord(raw) ||
      !['number', 'string', 'boolean'].includes(String(raw.type)) ||
      !['request_fixed', 'estimate_only', 'completion_fact'].includes(
        String(raw.source)
      )
    )
      invalidResponse(endpoint)
    const field: UsageField = {
      type: raw.type as UsageField['type'],
      source: raw.source as UsageField['source'],
      required: requiredBoolean(raw.required, endpoint),
    }
    if (raw.unit !== undefined)
      field.unit = requiredString(raw.unit, endpoint, false)
    if (raw.integer !== undefined)
      field.integer = requiredBoolean(raw.integer, endpoint)
    for (const bound of ['minimum', 'maximum'] as const) {
      if (raw[bound] !== undefined) {
        if (!isDecimal(raw[bound])) invalidResponse(endpoint)
        field[bound] = raw[bound]
      }
    }
    if (raw.enum !== undefined) {
      if (
        !Array.isArray(raw.enum) ||
        raw.enum.some(
          (item) => typeof item !== 'string' || item.length > 128
        ) ||
        new Set(raw.enum).size !== raw.enum.length
      )
        invalidResponse(endpoint)
      field.enum = raw.enum as string[]
    }
    if (field.type === 'number' && !field.unit) invalidResponse(endpoint)
    fields[key] = field
  }
  return { version: 1, fields, hash: requiredString(value.hash, endpoint) }
}
export interface PriceSeed {
  prices: PriceVersion[]
  unconfigured: string[]
  source: string
}
export interface TaskPriceSchema {
  model: string
  plugin_key: string
  plugin_version?: string
  source_hash?: string
  usage_schema: UsageSchema | null
  manifest_schema: unknown
  review_required: boolean
  plugins: Array<{ key: string; name: string; version: string }>
}

export function validatePriceVersion(
  value: Pick<
    PriceVersion,
    | 'mode'
    | 'prices'
    | 'expression'
    | 'usage_schema'
    | 'usage_scope'
    | 'contract_version'
    | 'calculation_version'
    | 'result_unit'
    | 'scope_kind'
    | 'channel_id'
    | 'flat_fee'
    | 'effective_from'
    | 'effective_to'
    | 'metering'
  >,
  publicProjection = false
): void {
  if (value.metering !== undefined) {
    parsePriceMetering(value.metering, 'price metering')
    if (
      value.mode !== 'unit_price' ||
      value.usage_schema ||
      !value.prices.some(
        (line) => line.quantity === value.metering?.quantity
      ) ||
      value.prices.some(
        (line) => ![value.metering?.quantity, 'calls'].includes(line.quantity)
      )
    )
      throw new Error('Invalid metering price dimensions')
  }
  if (value.usage_scope !== undefined && value.usage_scope !== 'embedding')
    throw new Error('Unsupported price usage scope')
  if (
    (value.contract_version !== undefined && value.contract_version !== 3) ||
    (value.calculation_version !== undefined &&
      value.calculation_version !== 'billing-v3')
  )
    throw new Error('Unsupported price contract')
  const scope = value.scope_kind ?? 'global_model'
  if (!(PRICE_SCOPE_KINDS as readonly string[]).includes(scope))
    throw new Error('Unsupported price scope')
  const channelId = value.channel_id ?? 0
  if (!Number.isSafeInteger(channelId) || channelId < 0)
    throw new Error('Invalid price channel')
  if (scope === 'global_model' && channelId !== 0)
    throw new Error('Global prices cannot have a channel')
  if (!publicProjection && scope === 'channel_model' && channelId <= 0)
    throw new Error('Channel prices require a channel')
  const decimalPrice = (raw: unknown): raw is string => {
    if (!isDecimal(raw) || raw.length > 64) return false
    const [whole = '', fraction = ''] = raw.split('.')
    return whole.length <= 19 && fraction.length <= 18
  }
  if (value.flat_fee !== undefined && !decimalPrice(value.flat_fee))
    throw new Error('Invalid flat fee')
  const parseTime = (raw: unknown): number => {
    if (
      typeof raw !== 'string' ||
      !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(
        raw
      ) ||
      !Number.isFinite(Date.parse(raw))
    )
      throw new Error('Invalid price window')
    return Date.parse(raw)
  }
  if (value.effective_from !== undefined) parseTime(value.effective_from)
  if (value.effective_to !== undefined) parseTime(value.effective_to)
  if (
    value.effective_from !== undefined &&
    value.effective_to !== undefined &&
    parseTime(value.effective_from) > parseTime(value.effective_to)
  )
    throw new Error('Invalid price window')
  if (
    value.mode === 'expression' &&
    value.expression?.startsWith('v2:') &&
    !value.usage_schema
  )
    throw new Error('Task expressions require a usage schema')
  if (!['unit_price', 'expression', 'free'].includes(value.mode))
    throw new Error('Invalid price mode')
  if (
    value.mode === 'free' &&
    (value.prices.length ||
      value.expression ||
      (value.flat_fee !== undefined &&
        compareDecimal(value.flat_fee, '0') !== 0))
  )
    throw new Error('Free mode cannot contain charges')
  if (value.mode === 'expression' && !value.expression?.trim())
    throw new Error('Expression is required')
  if (
    value.mode === 'unit_price' &&
    ((!value.prices.length && value.flat_fee === undefined) || value.expression)
  )
    throw new Error('Unit prices or a flat fee are required')
  if ((value.expression?.length ?? 0) > 16384)
    throw new Error('Expression is too long')
  const seen = new Set<string>()
  for (const price of value.prices) {
    if (seen.has(price.quantity)) throw new Error('Duplicate quantity')
    seen.add(price.quantity)
    if (price.quantity.length > 128) throw new Error('Invalid quantity')
    const tiered = price.tier_mode !== undefined
    if (
      price.tier_mode !== undefined &&
      !(PRICE_TIER_MODES as readonly string[]).includes(price.tier_mode)
    )
      throw new Error('Invalid tier mode')
    if (price.tiers !== undefined) {
      if (
        !tiered ||
        !price.tiers.length ||
        price.tiers.length > 32 ||
        price.price !== ''
      )
        throw new Error('Invalid price tiers')
      let previous = '0'
      for (const [index, tier] of price.tiers.entries()) {
        if (!decimalPrice(tier.unit_price) || tier.unit_price === '')
          throw new Error('Invalid tier price')
        if (tier.up_to === undefined) {
          if (index !== price.tiers.length - 1)
            throw new Error('Unbounded tier must be last')
          continue
        }
        if (!decimalPrice(tier.up_to) || tier.up_to === '0')
          throw new Error('Invalid tier boundary')
        if (compareDecimal(tier.up_to, previous) <= 0)
          throw new Error('Tier boundaries must increase')
        previous = tier.up_to
      }
    } else if (price.tier_mode !== undefined) {
      throw new Error('Tier mode requires tiers')
    }
    const field = value.usage_schema?.fields[price.quantity]
    if (value.usage_schema) {
      if (
        !field ||
        field.type !== 'number' ||
        field.source === 'estimate_only' ||
        price.unit !== `per_${field.unit}` ||
        (!tiered && !decimalPrice(price.price))
      )
        throw new Error('Invalid schema unit price')
      continue
    }
    if (
      !(PRICE_QUANTITIES as readonly string[]).includes(price.quantity) &&
      !/^tool:[a-zA-Z0-9_.:-]+$/.test(price.quantity)
    )
      throw new Error('Unknown quantity')
    if (
      !(PRICE_UNITS as readonly string[]).includes(price.unit) ||
      (!tiered && !decimalPrice(price.price)) ||
      (tiered && price.price !== '')
    )
      throw new Error('Invalid unit price')
    const tokenQuantity =
      !['calls', 'images', 'seconds', 'characters', 'credits'].includes(
        price.quantity
      ) && !price.quantity.startsWith('tool:')
    if (
      price.quantity.length > 128 ||
      (value.mode === 'expression' && tokenQuantity)
    )
      throw new Error('Expression owns token pricing')
    const expectedUnits = tokenQuantity
      ? ['per_million_tokens']
      : (
          {
            calls: ['per_call', 'per_thousand_calls'],
            images: ['per_image'],
            seconds: ['per_second', 'per_minute', 'per_hour'],
            characters: ['per_character', 'per_million_characters'],
            credits: ['per_credit'],
          } as Record<string, string[]>
        )[price.quantity]
    if (
      price.quantity.startsWith('tool:')
        ? !['per_call', 'per_thousand_calls'].includes(price.unit)
        : !expectedUnits?.includes(price.unit)
    )
      throw new Error('Incompatible quantity and unit')
    if (
      price.quantity === 'characters' &&
      value.metering?.quantity !== 'characters'
    )
      throw new Error('Characters require explicit metering')
  }
}

export function parsePriceLines(value: unknown, endpoint: string): UnitPrice[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((line) => {
    if (!isRecord(line)) invalidResponse(endpoint)
    const parsed: UnitPrice = {
      quantity: requiredString(line.quantity, endpoint),
      unit: requiredString(line.unit, endpoint) as UnitPrice['unit'],
      price: requiredString(line.price, endpoint),
    }
    if (line.tier_mode !== undefined) {
      if (
        !(PRICE_TIER_MODES as readonly string[]).includes(
          String(line.tier_mode)
        )
      )
        invalidResponse(endpoint)
      parsed.tier_mode = line.tier_mode as PriceTierMode
    }
    if (line.tiers !== undefined) {
      if (!Array.isArray(line.tiers)) invalidResponse(endpoint)
      parsed.tiers = line.tiers.map((rawTier) => {
        if (!isRecord(rawTier)) invalidResponse(endpoint)
        const tier: PriceTier = {
          unit_price: requiredString(rawTier.unit_price, endpoint),
        }
        if (rawTier.up_to !== undefined)
          tier.up_to = requiredString(rawTier.up_to, endpoint)
        return tier
      })
    }
    return parsed
  })
}

export function parsePriceVersion(
  value: unknown,
  endpoint: string
): PriceVersion {
  return parsePriceDefinition(value, endpoint, false)
}
export type PublicPriceVersion = Omit<PriceVersion, 'channel_id' | 'provenance'>
export function parsePublicPriceVersion(
  value: unknown,
  endpoint: string
): PublicPriceVersion {
  if (
    isRecord(value) &&
    (value.channel_id !== undefined || value.provenance !== undefined)
  )
    invalidResponse(endpoint)
  return parsePriceDefinition(value, endpoint, true)
}
function parsePriceDefinition(
  value: unknown,
  endpoint: string,
  publicProjection: boolean
): PriceVersion {
  if (!isRecord(value) || !Array.isArray(value.prices))
    invalidResponse(endpoint)
  if (
    value.contract_version !== 3 ||
    value.calculation_version !== 'billing-v3' ||
    !['micro_usd', 'usd'].includes(String(value.result_unit))
  )
    invalidResponse(endpoint)
  const result: PriceVersion = {
    contract_version: 3,
    calculation_version: 'billing-v3',
    result_unit: value.result_unit as 'micro_usd' | 'usd',
    id: requiredStrictInteger(value.id, endpoint),
    model: requiredString(value.model, endpoint, false),
    mode: requiredString(value.mode, endpoint) as PriceMode,
    prices: parsePriceLines(value.prices, endpoint),
    ...(value.flat_fee === undefined
      ? {}
      : { flat_fee: requiredString(value.flat_fee, endpoint) }),
    expression:
      value.expression === undefined
        ? undefined
        : requiredString(value.expression, endpoint),
    created_at: requiredStrictInteger(value.created_at, endpoint),
    ...(value.effective_from === undefined
      ? {}
      : { effective_from: requiredString(value.effective_from, endpoint) }),
    ...(value.effective_to === undefined
      ? {}
      : { effective_to: requiredString(value.effective_to, endpoint) }),
    hash: requiredString(value.hash, endpoint),
  }
  if (value.scope_kind !== undefined)
    result.scope_kind = requiredString(
      value.scope_kind,
      endpoint
    ) as PriceScopeKind
  if (value.channel_id !== undefined)
    result.channel_id = requiredStrictInteger(value.channel_id, endpoint)
  if (value.usage_schema !== undefined)
    result.usage_schema = parseUsageSchema(value.usage_schema, endpoint)
  if (value.metering !== undefined)
    result.metering = parsePriceMetering(value.metering, endpoint)
  if (value.usage_scope !== undefined) {
    if (value.usage_scope !== 'embedding') invalidResponse(endpoint)
    result.usage_scope = value.usage_scope
  }
  if (value.provenance !== undefined) {
    const raw = value.provenance
    if (
      !isRecord(raw) ||
      !['source', 'evidence', 'retail'].includes(String(raw.kind))
    )
      invalidResponse(endpoint)
    const provenance: PriceProvenance = {
      kind: raw.kind as PriceProvenance['kind'],
    }
    if (raw.snapshot_id !== undefined)
      provenance.snapshot_id = requiredStrictInteger(raw.snapshot_id, endpoint)
    if (raw.parser_version !== undefined)
      provenance.parser_version = requiredString(raw.parser_version, endpoint)
    if (raw.retail_reason !== undefined)
      provenance.retail_reason = requiredString(raw.retail_reason, endpoint)
    if (raw.fields !== undefined) {
      if (!Array.isArray(raw.fields)) invalidResponse(endpoint)
      provenance.fields = raw.fields.map((field) => {
        if (!isRecord(field)) invalidResponse(endpoint)
        return {
          target: requiredString(field.target, endpoint),
          source_path: requiredString(field.source_path, endpoint),
          raw_value: requiredString(field.raw_value, endpoint),
          source_unit: requiredString(field.source_unit, endpoint),
          conversion: requiredString(field.conversion, endpoint),
        }
      })
    }
    result.provenance = provenance
  }
  if (value.support !== undefined) {
    if (!isRecord(value.support)) invalidResponse(endpoint)
    for (const key of ['dimensions', 'service_tiers'] as const) {
      if (
        !Array.isArray(value.support[key]) ||
        value.support[key].some((item: unknown) => typeof item !== 'string')
      )
        invalidResponse(endpoint)
    }
    result.support = {
      dimensions: value.support.dimensions as string[],
      service_tiers: value.support.service_tiers as string[],
    }
  }
  try {
    validatePriceVersion(result, publicProjection)
  } catch {
    invalidResponse(endpoint)
  }
  return result
}

export const modelPricingApi = {
  async taskSchema(
    model: string,
    pluginKey?: string,
    signal?: AbortSignal
  ): Promise<TaskPriceSchema> {
    const endpoint = '/api/billing/prices/task-schema'
    const value = await api.get(
      endpoint,
      { model, ...(pluginKey ? { plugin_key: pluginKey } : {}) },
      { signal }
    )
    if (!isRecord(value) || !Array.isArray(value.plugins))
      invalidResponse(endpoint)
    return {
      model: requiredString(value.model, endpoint, false),
      plugin_key: requiredString(value.plugin_key, endpoint),
      plugin_version:
        value.plugin_version === undefined
          ? undefined
          : requiredString(value.plugin_version, endpoint),
      source_hash:
        value.source_hash === undefined
          ? undefined
          : requiredString(value.source_hash, endpoint),
      usage_schema:
        value.usage_schema === null
          ? null
          : parseUsageSchema(value.usage_schema, endpoint),
      manifest_schema: value.manifest_schema,
      review_required: requiredBoolean(value.review_required, endpoint),
      plugins: value.plugins.map((plugin) => {
        if (!isRecord(plugin)) invalidResponse(endpoint)
        return {
          key: requiredString(plugin.key, endpoint, false),
          name: requiredString(plugin.name, endpoint),
          version: requiredString(plugin.version, endpoint),
        }
      }),
    }
  },
  async preview(input: PricePreviewInput): Promise<PricePreview> {
    validatePriceVersion(input.price)
    const endpoint = '/api/billing/prices/preview'
    const value = await api.post(endpoint, input)
    if (!isRecord(value) || !isRecord(value.quote)) invalidResponse(endpoint)
    const charge = parseBillingCharge(value.charge, endpoint)
    if (!charge) invalidResponse(endpoint)
    return { quote: value.quote, charge }
  },
  async load(signal?: AbortSignal): Promise<PriceVersion[]> {
    const endpoint = '/api/billing/prices'
    const value = await api.get(endpoint, undefined, { signal })
    if (!Array.isArray(value)) invalidResponse(endpoint)
    return value.map((row) => parsePriceVersion(row, endpoint))
  },
  async publish(
    price: PriceVersion,
    expectedVersion: number,
    pluginKey?: string
  ): Promise<PriceVersion> {
    validatePriceVersion(price)
    const endpoint = '/api/billing/prices'
    return parsePriceVersion(
      await api.post(endpoint, {
        price,
        expected_version: expectedVersion,
        ...(pluginKey ? { plugin_key: pluginKey } : {}),
      }),
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
