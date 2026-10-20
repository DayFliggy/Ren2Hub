import { parseUsageReference } from './pricingReference'
import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { parseModelCapabilities } from './modelManifest'
import { parsePricingValues } from './pricingConfig'
import { isDecimal } from '@/utils/decimal'
import type { BillingUsageSchema } from '@/features/pricing/types'
import type {
  MarketPricingContract,
  ModelMarketResponse,
} from '@/types/modelMarket'

const endpoint = '/api/user/model-market'

function records(value: unknown): Record<string, unknown>[] {
  if (!Array.isArray(value) || value.some((item) => !isRecord(item)))
    invalidResponse(endpoint)
  return value as Record<string, unknown>[]
}

function positiveId(value: unknown): number {
  const id = requiredStrictInteger(value, endpoint)
  if (id <= 0) invalidResponse(endpoint)
  return id
}

function localizedText(value: unknown): void {
  if (value == null || typeof value === 'string') return
  if (
    !isRecord(value) ||
    Object.values(value).some((text) => typeof text !== 'string')
  )
    invalidResponse(endpoint)
}

function validateUsageSchema(value: unknown): void {
  if (value == null) return
  if (!isRecord(value)) invalidResponse(endpoint)
  for (const field of Object.values(value)) {
    if (!isRecord(field)) invalidResponse(endpoint)
    localizedText(field.description)
    localizedText(field.unitLabel)
    if (field.unit != null) requiredString(field.unit, endpoint)
    if (field.enum != null) parseStringArray(field.enum, endpoint)
  }
}

function validateBillingDetails(value: unknown): void {
  if (value == null) return
  if (!isRecord(value)) invalidResponse(endpoint)
  for (const key of ['image_count', 'audio_text_branches']) {
    if (value[key] != null) requiredBoolean(value[key], endpoint)
  }
  if (value.request_rules != null) {
    for (const rule of records(value.request_rules)) {
      requiredString(rule.condition, endpoint, false)
      if (!isDecimal(rule.multiplier)) invalidResponse(endpoint)
    }
  }
}

function parsePricing(raw: Record<string, unknown>): MarketPricingContract {
  const values = parsePricingValues(raw.values, endpoint)
  for (const [key, value] of Object.entries(values)) {
    if (key === 'billing_setting.billing_expr') {
      requiredString(value, endpoint)
    } else if (key === 'billing_setting.billing_mode') {
      if (value !== 'ratio' && value !== 'tiered_expr')
        invalidResponse(endpoint)
    } else if (!isDecimal(value)) invalidResponse(endpoint)
  }
  const configured = requiredBoolean(raw.configured, endpoint)
  if (configured) {
    if (values['billing_setting.billing_mode'] === 'tiered_expr') {
      requiredString(values['billing_setting.billing_expr'], endpoint, false)
    } else if (
      values.ModelPrice === undefined &&
      values.ModelRatio === undefined
    ) {
      invalidResponse(endpoint)
    }
  }
  validateUsageSchema(raw.usage_schema)
  validateBillingDetails(raw.billing_details)
  if (
    raw.cache_write_mode != null &&
    !['none', 'standard', 'claude_ttl'].includes(String(raw.cache_write_mode))
  )
    invalidResponse(endpoint)
  return {
    id: requiredString(raw.id, endpoint, false),
    version: requiredString(raw.version, endpoint, false),
    configured,
    values,
    usage_reference: parseUsageReference(raw.usage_reference, endpoint),
    supported_endpoint_types: parseStringArray(
      raw.supported_endpoint_types,
      endpoint
    ),
    ...(raw.plugin_key == null
      ? {}
      : { plugin_key: requiredString(raw.plugin_key, endpoint) }),
    ...(raw.plugin_name == null
      ? {}
      : { plugin_name: requiredString(raw.plugin_name, endpoint) }),
    ...(raw.usage_schema == null
      ? {}
      : { usage_schema: raw.usage_schema as BillingUsageSchema }),
    ...(raw.cache_write_mode == null
      ? {}
      : { cache_write_mode: requiredString(raw.cache_write_mode, endpoint) }),
    ...(raw.billing_details == null
      ? {}
      : { billing_details: raw.billing_details as Record<string, unknown> }),
  }
}

export function parseModelMarket(value: unknown): ModelMarketResponse {
  if (
    !isRecord(value) ||
    value.schema_version !== 1 ||
    !isRecord(value.telemetry) ||
    value.telemetry.status !== 'unavailable' ||
    value.telemetry.reason !== 'channel_metrics_not_collected' ||
    !isRecord(value.supported_endpoints)
  )
    invalidResponse(endpoint)
  const supportedEndpoints = Object.fromEntries(
    Object.entries(value.supported_endpoints).map(([type, raw]) => {
      if (!isRecord(raw)) invalidResponse(endpoint)
      return [
        type,
        {
          path: requiredString(raw.path, endpoint, false),
          method: requiredString(raw.method, endpoint, false),
        },
      ]
    })
  )
  const models = records(value.models).map((raw) => ({
    id: requiredString(raw.id, endpoint, false),
    description: requiredString(raw.description, endpoint),
    vendor: requiredString(raw.vendor, endpoint),
    icon: requiredString(raw.icon, endpoint),
    capabilities: parseModelCapabilities(raw.capabilities, endpoint),
    supported_endpoint_types: parseStringArray(
      raw.supported_endpoint_types,
      endpoint
    ),
    baselines: records(raw.baselines).map(parsePricing),
  }))
  const channels = records(value.channels).map((raw) => {
    if (!isDecimal(raw.ratio)) invalidResponse(endpoint)
    return {
      id: positiveId(raw.id),
      name: requiredString(raw.name, endpoint),
      ratio: raw.ratio,
    }
  })
  const offers = records(value.offers).map((raw) => ({
    id: requiredString(raw.id, endpoint, false),
    model_id: requiredString(raw.model_id, endpoint, false),
    channel_id: positiveId(raw.channel_id),
    pricing_id: requiredString(raw.pricing_id, endpoint, false),
    supported_endpoint_types: parseStringArray(
      raw.supported_endpoint_types,
      endpoint
    ),
  }))
  const byModel = new Map(models.map((model) => [model.id, model]))
  const channelIds = new Set(channels.map((channel) => channel.id))
  if (
    byModel.size !== models.length ||
    channelIds.size !== channels.length ||
    new Set(offers.map((offer) => offer.id)).size !== offers.length
  )
    invalidResponse(endpoint)
  for (const model of models) {
    if (
      new Set(model.baselines.map((price) => price.id)).size !==
      model.baselines.length
    )
      invalidResponse(endpoint)
  }
  for (const offer of offers) {
    const model = byModel.get(offer.model_id)
    const price = model?.baselines.find((item) => item.id === offer.pricing_id)
    if (
      !model ||
      !price ||
      !channelIds.has(offer.channel_id) ||
      !offer.supported_endpoint_types.length ||
      offer.supported_endpoint_types.some(
        (type) =>
          !model.supported_endpoint_types.includes(type) ||
          !price.supported_endpoint_types.includes(type)
      )
    )
      invalidResponse(endpoint)
  }
  return {
    schema_version: 1,
    models,
    channels,
    offers,
    supported_endpoints: supportedEndpoints,
    telemetry: {
      status: 'unavailable',
      reason: 'channel_metrics_not_collected',
    },
  }
}

export async function getModelMarket(
  signal?: AbortSignal
): Promise<ModelMarketResponse> {
  return parseModelMarket(
    await api.get<unknown>(endpoint, undefined, { signal })
  )
}
