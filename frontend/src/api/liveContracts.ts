import type {
  DrawingLogItem,
  LogItem,
  LogType,
  AuditLogCategory,
  AuditLogItem,
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
  owner_by: string
  supported_endpoint_types: string[]
  quota_type: number
  model_ratio?: number
  model_price?: number
  completion_ratio?: number
  cache_ratio?: number
  create_cache_ratio?: number
  image_ratio?: number
  audio_ratio?: number
  audio_completion_ratio?: number
  billing_mode?: string
  billing_expr?: string
  billing_usage_schema?: import('@/features/pricing/types').BillingUsageSchema
  billing_plugin_variants?: Array<{
    plugin_key: string
    plugin_name: string
    billing_expr: string
    billing_mode: 'ratio' | 'tiered_expr' | 'unconfigured'
  }>
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
    const quotaType = requiredStrictInteger(item.quota_type, endpoint)
    if (quotaType !== 0 && quotaType !== 1) invalidResponse(endpoint)
    const result: PricingModelContract = {
      model_name: requiredString(item.model_name, endpoint, false),
      description: requiredString(item.description ?? '', endpoint),
      icon: requiredString(item.icon ?? '', endpoint),
      tags: requiredString(item.tags ?? '', endpoint),
      vendor_id: requiredInteger(item.vendor_id ?? 0, endpoint),
      owner_by: requiredString(item.owner_by ?? '', endpoint),
      supported_endpoint_types: parseStringArray(
        item.supported_endpoint_types ?? [],
        endpoint
      ),
      quota_type: quotaType,
    }
    for (const key of [
      'model_ratio',
      'model_price',
      'completion_ratio',
      'cache_ratio',
      'create_cache_ratio',
      'image_ratio',
      'audio_ratio',
      'audio_completion_ratio',
    ] as const) {
      if (item[key] !== undefined) {
        const number = requiredNumber(item[key], endpoint)
        if (number < 0) invalidResponse(endpoint)
        result[key] = number
      }
    }
    for (const key of ['billing_mode', 'billing_expr'] as const)
      if (item[key] !== undefined)
        result[key] = requiredString(item[key], endpoint)
    if (
      result.billing_mode === 'unconfigured'
        ? false
        : result.billing_mode === 'tiered_expr'
          ? !result.billing_expr?.trim()
          : result.quota_type === 1
            ? result.model_price === undefined
            : result.model_ratio === undefined
    )
      invalidResponse(endpoint)
    if (item.billing_usage_schema !== undefined) {
      if (!isRecord(item.billing_usage_schema)) invalidResponse(endpoint)
      result.billing_usage_schema =
        item.billing_usage_schema as PricingModelContract['billing_usage_schema']
    }
    if (item.billing_plugin_variants !== undefined) {
      if (!Array.isArray(item.billing_plugin_variants))
        invalidResponse(endpoint)
      result.billing_plugin_variants = item.billing_plugin_variants.map(
        (raw) => {
          if (!isRecord(raw)) invalidResponse(endpoint)
          const mode = requiredString(raw.billing_mode, endpoint)
          if (
            mode !== 'ratio' &&
            mode !== 'tiered_expr' &&
            mode !== 'unconfigured'
          )
            invalidResponse(endpoint)
          const expression = requiredString(raw.billing_expr ?? '', endpoint)
          if (mode === 'tiered_expr' && !expression.trim())
            invalidResponse(endpoint)
          return {
            plugin_key: requiredString(raw.plugin_key, endpoint),
            plugin_name: requiredString(raw.plugin_name, endpoint),
            billing_expr: expression,
            billing_mode: mode,
          }
        }
      )
    }
    return result
  })
}
export interface PricingCatalogIssue {
  model_name: string | null
  offer_id: null
  reason: 'invalid_model_record'
}
export function parsePricingModelsPartial(value: unknown): {
  models: PricingModelContract[]
  issues: PricingCatalogIssue[]
} {
  if (!Array.isArray(value)) invalidResponse('/api/pricing')
  const models: PricingModelContract[] = [],
    issues: PricingCatalogIssue[] = []
  for (const raw of value) {
    try {
      models.push(...parsePricingModels([raw]))
    } catch (error) {
      if (!(error instanceof ApiError) || error.code !== 'INVALID_RESPONSE')
        throw error
      issues.push({
        model_name:
          isRecord(raw) && typeof raw.model_name === 'string'
            ? raw.model_name
            : null,
        offer_id: null,
        reason: 'invalid_model_record',
      })
    }
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

const AUDIT_LOG_CATEGORIES = new Set<AuditLogCategory>([
  'login',
  'security',
  'operation',
  'access_token',
  'system',
])

const AUDIT_ACTOR_ROLES = new Set([1, 10, 100])

function optionalAuditString(value: unknown, endpoint: string): string {
  if (value === undefined || value === null) return ''
  return requiredString(value, endpoint)
}

function parseAuditLog(value: unknown, endpoint: string): AuditLogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  const category = requiredString(value.category, endpoint) as AuditLogCategory
  if (!AUDIT_LOG_CATEGORIES.has(category)) invalidResponse(endpoint)
  const other = value.other ?? {}
  if (!isRecord(other)) invalidResponse(endpoint)
  const op = other.op ?? {}
  if (!isRecord(op)) invalidResponse(endpoint)
  const params = op.params ?? {}
  if (!isRecord(params)) invalidResponse(endpoint)
  const role = requiredInteger(value.actor_role, endpoint)
  const status = requiredInteger(value.status, endpoint)
  if (typeof value.success !== 'boolean') invalidResponse(endpoint)

  return {
    event_id: requiredString(value.event_id, endpoint),
    created_at: requiredInteger(value.created_at, endpoint),
    category,
    action: optionalAuditString(value.action, endpoint),
    params: { ...params },
    content: optionalAuditString(value.content, endpoint),
    actor: {
      id: requiredInteger(value.user_id, endpoint),
      username: optionalAuditString(value.username, endpoint),
      role: AUDIT_ACTOR_ROLES.has(role) ? role : null,
      auth_method: optionalAuditString(value.auth_method, endpoint),
    },
    token_ref: optionalAuditString(value.token_ref, endpoint),
    request_id: optionalAuditString(value.request_id, endpoint),
    ip: optionalAuditString(value.ip, endpoint),
    user_agent: optionalAuditString(value.user_agent, endpoint),
    request: {
      method: optionalAuditString(value.method, endpoint),
      route: optionalAuditString(value.route, endpoint),
      status: status > 0 ? status : null,
      success: value.success,
    },
  }
}

export function parseAuditLogPage(value: unknown): PageResult<AuditLogItem> {
  return parsePage(value, '/api/audit', parseAuditLog)
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
