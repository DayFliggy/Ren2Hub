import {
  parsePublicPriceVersion,
  parsePriceLines,
  type PublicPriceVersion,
  type UnitPrice,
} from './modelPricing'
import { isDecimal } from '@/utils/decimal'
import type {
  DrawingLogItem,
  LogItem,
  LogType,
  OperationLogItem,
  OperationLogKind,
  RelayTaskLogItem,
  TokenSummary,
} from '@/types/console'

import {
  invalidResponse,
  isRecord,
  parsePage,
  parseStringArray,
  requiredInteger,
  requiredStrictInteger,
  requiredBoolean,
  requiredQuota,
  requiredNumber,
  requiredString,
} from './contracts'
import { ApiError, type PageResult } from './types'
import { parseConversionPolicy } from './gateway'

export interface UsageRow {
  model_name: string
  created_at: number
  count: number
  quota: string
  token_used: number
}

export interface PaymentMethodInfo {
  name: string
  type: string
  color?: string
  min_topup?: number
  /**
   * Crypto assets carry the requested token and network. Epay methods leave
   * these unset, and the pair is never inferred from the display name.
   */
  token?: string
  network?: string
  icon?: string
}

export interface TopupInfo {
  enable_online_topup: boolean
  enable_redemption: boolean
  pay_methods: PaymentMethodInfo[]
  min_topup: number
  amount_options: number[]
}

export interface GmpayTopupInfo {
  enable_gmpay_topup: boolean
  pay_methods: PaymentMethodInfo[]
  min_topup: number
  amount_options: number[]
}

/** Optional payment metadata uses an empty string when not configured. */
function optionalMethodField(
  value: unknown,
  endpoint: string
): string | undefined {
  if (value === undefined) return undefined
  const parsed = requiredString(value, endpoint)
  return parsed === '' ? undefined : parsed
}

function optionalBoolean(value: unknown, endpoint: string): boolean {
  if (value === undefined) return false
  if (typeof value !== 'boolean') invalidResponse(endpoint)
  return value
}

export function parseUserModels(value: unknown): string[] {
  const endpoint = '/api/user/models'
  if (Array.isArray(value)) return parseStringArray(value, endpoint)
  if (isRecord(value) && Array.isArray(value.models)) {
    return parseStringArray(value.models, endpoint)
  }
  return invalidResponse(endpoint)
}

export interface PricingModelContract {
  model_name: string
  description: string
  icon: string
  tags: string
  vendor_id: number
  offers: PricingOffer[]
  owner_by: string
  supported_endpoint_types: string[]
}

export interface PricingOffer {
  offer_id: string
  priced_model: string
  price_version: PublicPriceVersion
  conditions: {
    usage_scope?: 'embedding'
    dimensions?: string[]
    service_tiers?: string[]
    context_rule?: string
  }
  adjustments: Array<{ kind: 'channel_multiplier'; multiplier: string }>
  display_prices: UnitPrice[]
  display_flat_fee?: string
  availability: { status: 'available' | 'unavailable'; reason?: string }
}

export function parsePricingOffer(
  value: unknown,
  endpoint: string
): PricingOffer {
  if (
    !isRecord(value) ||
    !isRecord(value.conditions) ||
    !Array.isArray(value.adjustments) ||
    !isRecord(value.availability)
  )
    invalidResponse(endpoint)
  const version = parsePublicPriceVersion(value.price_version, endpoint)
  const pricedModel = requiredString(value.priced_model, endpoint, false)
  if (version.model !== pricedModel) invalidResponse(endpoint)
  const conditions: PricingOffer['conditions'] = {}
  if (value.conditions.usage_scope !== undefined) {
    if (value.conditions.usage_scope !== 'embedding') invalidResponse(endpoint)
    conditions.usage_scope = 'embedding'
  }
  for (const key of ['dimensions', 'service_tiers'] as const) {
    if (value.conditions[key] !== undefined)
      conditions[key] = parseStringArray(value.conditions[key], endpoint)
  }
  if (value.conditions.context_rule !== undefined)
    conditions.context_rule = requiredString(
      value.conditions.context_rule,
      endpoint
    )
  if (!['available', 'unavailable'].includes(String(value.availability.status)))
    invalidResponse(endpoint)
  const displayPrices = parsePriceLines(value.display_prices, endpoint)
  if (displayPrices.length !== version.prices.length) invalidResponse(endpoint)
  for (const [index, line] of displayPrices.entries()) {
    const base = version.prices[index]!
    if (
      line.quantity !== base.quantity ||
      line.unit !== base.unit ||
      line.tier_mode !== base.tier_mode ||
      Boolean(line.tiers) !== Boolean(base.tiers)
    )
      invalidResponse(endpoint)
    if (line.tiers) {
      if (
        line.tiers.length !== base.tiers?.length ||
        line.tiers.some(
          (tier, i) =>
            !isDecimal(tier.unit_price) || tier.up_to !== base.tiers?.[i]?.up_to
        )
      )
        invalidResponse(endpoint)
    } else if (!isDecimal(line.price)) invalidResponse(endpoint)
  }
  if (
    value.display_flat_fee !== undefined &&
    !isDecimal(value.display_flat_fee)
  )
    invalidResponse(endpoint)
  const adjustments = value.adjustments.map((adjustment) => {
    if (
      !isRecord(adjustment) ||
      adjustment.kind !== 'channel_multiplier' ||
      !isDecimal(adjustment.multiplier) ||
      /^0(?:\.0+)?$/.test(adjustment.multiplier)
    )
      invalidResponse(endpoint)
    return {
      kind: 'channel_multiplier' as const,
      multiplier: adjustment.multiplier,
    }
  })
  if (adjustments.length !== 1) invalidResponse(endpoint)
  return {
    offer_id: requiredString(value.offer_id, endpoint, false),
    priced_model: pricedModel,
    price_version: version,
    conditions,
    adjustments,
    display_prices: displayPrices,
    ...(value.display_flat_fee === undefined
      ? {}
      : { display_flat_fee: value.display_flat_fee }),
    availability: {
      status: value.availability
        .status as PricingOffer['availability']['status'],
      ...(value.availability.reason === undefined
        ? {}
        : { reason: requiredString(value.availability.reason, endpoint) }),
    },
  }
}

export interface PerfModelSummaryContract {
  model_name: string
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}

export function parsePricingModels(value: unknown): PricingModelContract[] {
  const endpoint = '/api/pricing'
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const endpoints = item.supported_endpoint_types ?? []
    if (
      !Array.isArray(endpoints) ||
      endpoints.some((entry) => typeof entry !== 'string')
    ) {
      invalidResponse(endpoint)
    }
    const modelName = requiredString(item.model_name, endpoint, false)
    if (!Array.isArray(item.offers) || !item.offers.length)
      invalidResponse(endpoint)
    const offers = item.offers.map((offer) =>
      parsePricingOffer(offer, endpoint)
    )
    if (new Set(offers.map((offer) => offer.offer_id)).size !== offers.length)
      invalidResponse(endpoint)
    return {
      model_name: modelName,
      description: requiredString(item.description ?? '', endpoint),
      icon: requiredString(item.icon ?? '', endpoint),
      tags: requiredString(item.tags ?? '', endpoint),
      vendor_id: requiredInteger(item.vendor_id ?? 0, endpoint),
      offers,
      owner_by: requiredString(item.owner_by ?? '', endpoint),
      supported_endpoint_types: [...endpoints],
    }
  })
}

export interface PricingCatalogIssue {
  model_name: string | null
  offer_id: string | null
  reason: 'invalid_price_offer' | 'invalid_model_record'
}

// One unsupported offer must not hide unrelated prices. Keep the failure
// separate from validated offers so it cannot be mistaken for a free price.
export function parsePricingModelsPartial(value: unknown): {
  models: PricingModelContract[]
  issues: PricingCatalogIssue[]
} {
  const endpoint = '/api/pricing'
  if (!Array.isArray(value)) invalidResponse(endpoint)
  const models: PricingModelContract[] = []
  const issues: PricingCatalogIssue[] = []
  for (const raw of value) {
    const modelName =
      isRecord(raw) && typeof raw.model_name === 'string'
        ? raw.model_name
        : null
    if (!isRecord(raw) || !Array.isArray(raw.offers) || !raw.offers.length) {
      issues.push({
        model_name: modelName,
        offer_id: null,
        reason: 'invalid_model_record',
      })
      continue
    }
    let model: PricingModelContract | undefined
    const seen = new Set<string>()
    for (const offer of raw.offers) {
      try {
        const parsed = parsePricingModels([{ ...raw, offers: [offer] }])[0]!
        const parsedOffer = parsed.offers[0]!
        if (seen.has(parsedOffer.offer_id)) invalidResponse(endpoint)
        seen.add(parsedOffer.offer_id)
        if (model) model.offers.push(parsedOffer)
        else model = parsed
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== 'INVALID_RESPONSE')
          throw error
        issues.push({
          model_name: modelName,
          offer_id:
            isRecord(offer) && typeof offer.offer_id === 'string'
              ? offer.offer_id
              : null,
          reason: 'invalid_price_offer',
        })
      }
    }
    if (model) models.push(model)
  }
  return { models, issues }
}

export function parsePerfMetricsSummary(
  value: unknown
): PerfModelSummaryContract[] {
  const endpoint = '/api/perf-metrics/summary'
  if (!isRecord(value) || !Array.isArray(value.models)) {
    invalidResponse(endpoint)
  }
  return value.models.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    return {
      model_name: requiredString(item.model_name, endpoint, false),
      avg_latency_ms: requiredInteger(item.avg_latency_ms, endpoint),
      success_rate: requiredNumber(item.success_rate, endpoint),
      avg_tps: requiredNumber(item.avg_tps, endpoint),
    }
  })
}

export interface LogStatContract {
  total_requests: number
  total_quota: string
  today_requests: number
  today_quota: string
}

export function parseLogStat(value: unknown): LogStatContract {
  const endpoint = '/api/log/self/stat'
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    total_requests: requiredInteger(value.total_requests, endpoint),
    total_quota: requiredQuota(value.total_quota, endpoint),
    today_requests: requiredInteger(value.today_requests, endpoint),
    today_quota: requiredQuota(value.today_quota, endpoint),
  }
}

export function parseToken(value: unknown, endpoint: string): TokenSummary {
  if (!isRecord(value)) invalidResponse(endpoint)
  const type = requiredString(value.type, endpoint)
  if (type !== 'auto' && type !== 'manual') invalidResponse(endpoint)
  const allowIps = value.allow_ips
  if (
    allowIps !== undefined &&
    allowIps !== null &&
    typeof allowIps !== 'string'
  ) {
    invalidResponse(endpoint)
  }
  const limits = requiredString(value.model_limits ?? '', endpoint)
  const unlimited = requiredBoolean(value.unlimited_quota, endpoint)
  if (unlimited && value.available_quota !== null) invalidResponse(endpoint)
  const version = requiredStrictInteger(value.quota_version, endpoint)
  if (version < 0) invalidResponse(endpoint)
  return {
    id: requiredInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint, false),
    key_preview: requiredString(value.key, endpoint),
    type,
    conversion_policy: parseConversionPolicy(value.conversion_policy ?? 'safe'),
    status: requiredInteger(value.status, endpoint) === 1 ? 1 : 2,
    used_quota: requiredQuota(value.used_quota, endpoint),
    reserved_quota: requiredQuota(value.reserved_quota, endpoint),
    quota_version: version,
    available_quota: unlimited
      ? null
      : requiredQuota(value.available_quota, endpoint),
    unlimited,
    model_limits: optionalBoolean(value.model_limits_enabled, endpoint)
      ? limits
          .split(',')
          .map((item) => item.trim())
          .filter(Boolean)
      : [],
    ip_limits:
      typeof allowIps === 'string'
        ? allowIps
            .split(/[,\n]/)
            .map((item) => item.trim())
            .filter(Boolean)
        : [],
    rate_limit: 0,
    load_balance: false,
    channels: [],
    expired_time: requiredInteger(value.expired_time, endpoint),
    created_time: requiredInteger(value.created_time, endpoint),
  }
}

export function parseTokenPage(value: unknown): PageResult<TokenSummary> {
  const endpoint = '/api/token/'
  return parsePage(value, endpoint, parseToken)
}

export function parseUsageRows(value: unknown): UsageRow[] {
  const endpoint = '/api/data/self'
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    return {
      model_name: requiredString(item.model_name, endpoint, false),
      created_at: requiredInteger(item.created_at, endpoint),
      count: requiredInteger(item.count, endpoint),
      quota: requiredQuota(item.quota, endpoint),
      token_used: requiredInteger(item.token_used, endpoint),
    }
  })
}

export function parseTopupInfo(value: unknown): TopupInfo {
  const endpoint = '/api/next/wallet/config'
  if (!isRecord(value) || !Array.isArray(value.pay_methods)) {
    invalidResponse(endpoint)
  }
  const payMethods = value.pay_methods.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const minTopup = requiredStrictInteger(item.min_topup, endpoint)
    const type = requiredString(item.type, endpoint, false)
    if (minTopup <= 0 || type.includes(':')) {
      invalidResponse(endpoint)
    }
    return {
      name: requiredString(item.name, endpoint, false),
      type,
      color: optionalMethodField(item.color, endpoint),
      min_topup: minTopup,
    }
  })
  return {
    enable_online_topup: requiredBoolean(value.enable_online_topup, endpoint),
    enable_redemption: requiredBoolean(value.enable_redemption, endpoint),
    pay_methods: payMethods,
    ...parseTopupAmounts(value, endpoint),
  }
}

export function parseGmpayTopupInfo(value: unknown): GmpayTopupInfo {
  const endpoint = '/api/next/wallet/gmpay/config'
  if (!isRecord(value) || !Array.isArray(value.pay_methods))
    invalidResponse(endpoint)
  const payMethods = value.pay_methods.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const token = requiredString(item.token, endpoint, false)
    const network = requiredString(item.network, endpoint, false)
    const type = requiredString(item.type, endpoint, false)
    const minimum = requiredString(item.min_topup, endpoint, false)
    const minTopup = Number(minimum)
    if (
      type !== `gmpay:${token}.${network}` ||
      !/^[a-z0-9_-]+$/.test(token) ||
      !/^[a-z0-9_-]+$/.test(network) ||
      !/^[1-9]\d*$/.test(minimum) ||
      !Number.isSafeInteger(minTopup)
    ) {
      invalidResponse(endpoint)
    }
    return {
      name: requiredString(item.name, endpoint, false),
      type,
      token,
      network,
      min_topup: minTopup,
      color: optionalMethodField(item.color, endpoint),
      icon: optionalMethodField(item.icon, endpoint),
    }
  })
  return {
    enable_gmpay_topup: requiredBoolean(value.enable_gmpay_topup, endpoint),
    pay_methods: payMethods,
    ...parseTopupAmounts(value, endpoint),
  }
}

function parseTopupAmounts(value: Record<string, unknown>, endpoint: string) {
  const amountOptions = value.amount_options
  if (!Array.isArray(amountOptions)) invalidResponse(endpoint)
  const parsedAmounts = amountOptions.map((item) =>
    requiredStrictInteger(item, endpoint)
  )
  if (parsedAmounts.some((amount) => amount <= 0)) invalidResponse(endpoint)
  const minTopup = requiredStrictInteger(value.min_topup, endpoint)
  if (minTopup <= 0) invalidResponse(endpoint)
  return {
    min_topup: minTopup,
    amount_options: parsedAmounts,
  }
}

export function parseRedeemedQuota(value: unknown): string {
  const endpoint = '/api/user/topup'
  if (typeof value === 'string') return requiredQuota(value, endpoint)
  if (isRecord(value) && value.quota !== undefined) {
    return requiredQuota(value.quota, endpoint)
  }
  return invalidResponse(endpoint)
}

const LOG_TYPES: Record<number, LogType> = {
  1: 'topup',
  2: 'consume',
  3: 'manage',
  4: 'system',
  5: 'error',
  6: 'refund',
  7: 'login',
}

function parseLogOther(value: unknown, endpoint: string): LogItem['other'] {
  if (value === undefined || value === null || value === '') return null
  if (isRecord(value)) return value
  if (typeof value === 'string') {
    try {
      const parsed: unknown = JSON.parse(value)
      return isRecord(parsed) ? parsed : value
    } catch {
      return value
    }
  }
  return invalidResponse(endpoint)
}

function parseLog(value: unknown, endpoint: string): LogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  const numericType = requiredInteger(value.type, endpoint)
  const type = LOG_TYPES[numericType]
  if (!type) invalidResponse(endpoint)
  const other = parseLogOther(value.other, endpoint)
  const firstToken = isRecord(other) ? Number(other.frt) : Number.NaN
  const promptTokens = requiredInteger(value.prompt_tokens, endpoint)
  const completionTokens = requiredInteger(value.completion_tokens, endpoint)
  const latencyMs = requiredInteger(value.use_time, endpoint)
  const latency = latencyMs / 1000
  return {
    id: requiredInteger(value.id, endpoint),
    type,
    token_name: requiredString(value.token_name, endpoint),
    model: requiredString(value.model_name, endpoint),
    channel: requiredString(value.channel_name, endpoint),
    prompt_tokens: promptTokens,
    completion_tokens: completionTokens,
    other,
    quota: requiredQuota(value.quota, endpoint),
    latency,
    first_token_latency: Number.isFinite(firstToken) ? firstToken / 1000 : null,
    request_mode: optionalBoolean(value.is_stream, endpoint)
      ? 'stream'
      : 'sync',
    tps:
      latency > 0 && completionTokens > 0
        ? Math.round((completionTokens / latency) * 100) / 100
        : 0,
    content: requiredString(value.content, endpoint),
    created: requiredInteger(value.created_at, endpoint),
  }
}

export function parseLogPage(value: unknown): PageResult<LogItem> {
  const endpoint = '/api/log/self'
  return parsePage(value, endpoint, parseLog)
}

const OPERATION_LOG_KINDS = new Set<OperationLogKind>([
  'manage',
  'system',
  'login',
])

function optionalOperationString(value: unknown, endpoint: string): string {
  if (value === undefined || value === null) return ''
  return requiredString(value, endpoint)
}

function optionalOperationInteger(
  value: unknown,
  endpoint: string
): number | null {
  if (value === undefined || value === null) return null
  return requiredInteger(value, endpoint)
}

function optionalOperationBoolean(
  value: unknown,
  endpoint: string
): boolean | null {
  if (value === undefined || value === null) return null
  if (typeof value !== 'boolean') invalidResponse(endpoint)
  return value
}

function parseOperationLog(value: unknown, endpoint: string): OperationLogItem {
  if (!isRecord(value) || !isRecord(value.actor)) invalidResponse(endpoint)
  const kind = requiredString(value.kind, endpoint) as OperationLogKind
  if (!OPERATION_LOG_KINDS.has(kind)) invalidResponse(endpoint)

  const params = value.params ?? {}
  if (!isRecord(params)) invalidResponse(endpoint)

  let request: OperationLogItem['request'] = null
  if (value.request !== undefined && value.request !== null) {
    if (!isRecord(value.request)) invalidResponse(endpoint)
    request = {
      method: optionalOperationString(value.request.method, endpoint),
      route: optionalOperationString(value.request.route, endpoint),
      path: optionalOperationString(value.request.path, endpoint),
      status: optionalOperationInteger(value.request.status, endpoint),
      success: optionalOperationBoolean(value.request.success, endpoint),
    }
  }

  return {
    id: requiredInteger(value.id, endpoint),
    created_at: requiredInteger(value.created_at, endpoint),
    kind,
    action: optionalOperationString(value.action, endpoint),
    params: { ...params },
    content: requiredString(value.content, endpoint),
    actor: {
      id: requiredInteger(value.actor.id, endpoint),
      username: requiredString(value.actor.username, endpoint),
      role: optionalOperationInteger(value.actor.role, endpoint),
      auth_method: optionalOperationString(value.actor.auth_method, endpoint),
    },
    ip: optionalOperationString(value.ip, endpoint),
    user_agent: optionalOperationString(value.user_agent, endpoint),
    request,
  }
}

export function parseOperationLogPage(
  value: unknown
): PageResult<OperationLogItem> {
  const endpoint = '/api/next/admin/operation-logs'
  return parsePage(value, endpoint, parseOperationLog)
}

function parseDrawingLog(value: unknown, endpoint: string): DrawingLogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredInteger(value.id, endpoint),
    mj_id: requiredString(value.mj_id ?? '', endpoint),
    action: requiredString(value.action ?? '', endpoint),
    prompt: requiredString(value.prompt ?? '', endpoint),
    prompt_en: requiredString(value.prompt_en ?? '', endpoint),
    status: requiredString(value.status ?? '', endpoint),
    progress: requiredString(value.progress ?? '', endpoint),
    fail_reason: requiredString(value.fail_reason ?? '', endpoint),
    image_url: requiredString(value.image_url ?? '', endpoint),
    video_url: requiredString(value.video_url ?? '', endpoint),
    quota: requiredQuota(value.quota, endpoint),
    submit_time: requiredInteger(value.submit_time ?? 0, endpoint),
    finish_time: requiredInteger(value.finish_time ?? 0, endpoint),
  }
}

export function parseDrawingLogPage(
  value: unknown
): PageResult<DrawingLogItem> {
  const endpoint = '/api/mj/self'
  return parsePage(value, endpoint, parseDrawingLog)
}

function parseRelayTaskLog(value: unknown, endpoint: string): RelayTaskLogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredInteger(value.id, endpoint),
    task_id: requiredString(value.task_id ?? '', endpoint),
    platform: requiredString(value.platform ?? '', endpoint),
    action: requiredString(value.action ?? '', endpoint),
    status: requiredString(value.status ?? '', endpoint),
    progress: requiredString(value.progress ?? '', endpoint),
    fail_reason: requiredString(value.fail_reason ?? '', endpoint),
    result_url: requiredString(value.result_url ?? '', endpoint),
    quota: requiredQuota(value.quota, endpoint),
    submit_time: requiredInteger(value.submit_time ?? 0, endpoint),
    finish_time: requiredInteger(value.finish_time ?? 0, endpoint),
  }
}

export function parseTaskLogPage(value: unknown): PageResult<RelayTaskLogItem> {
  const endpoint = '/api/task/self'
  return parsePage(value, endpoint, parseRelayTaskLog)
}
