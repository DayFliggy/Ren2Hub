import type {
  DrawingLogItem,
  LogItem,
  LogType,
  AuditLogCategory,
  AuditLogItem,
  RelayTaskLogItem,
  TaskArtifact,
  TaskArtifactType,
  TaskArtifactsResult,
  TaskAudioClip,
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
import { safeExternalUrl, safeImageUrl } from '@/utils/safeUrl'
import { ApiError, type PageResult } from './types'
import { parseConversionPolicy } from './gateway'
import { parsePricingValues, type PricingValues } from './pricingConfig'
import { parseUsageReference } from './pricingReference'
import type { UsageReference } from '@/types/pricingReference'
import { isDecimal } from '@/utils/decimal'

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
  pricing_values?: PricingValues
  pricing_version?: string
  usage_reference?: UsageReference
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
    usage_reference?: UsageReference
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
    if (item.pricing_values !== undefined) {
      const values = parsePricingValues(item.pricing_values, endpoint)
      for (const [key, value] of Object.entries(values)) {
        if (
          key === 'billing_setting.billing_mode' ||
          key === 'billing_setting.billing_expr'
        )
          requiredString(value, endpoint)
        else if (!isDecimal(value)) invalidResponse(endpoint)
      }
      result.pricing_values = values
    }
    if (item.pricing_version !== undefined)
      result.pricing_version = requiredString(item.pricing_version, endpoint)
    result.usage_reference = parseUsageReference(item.usage_reference, endpoint)
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
            usage_reference: parseUsageReference(raw.usage_reference, endpoint),
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
    allow_user_quota_query:
      value.allow_user_quota_query === undefined
        ? false
        : requiredBoolean(value.allow_user_quota_query, endpoint),
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
    ...(value.request_id == null || value.request_id === ''
      ? {}
      : { request_id: requiredString(value.request_id, endpoint, false) }),
    ...(value.token_id == null
      ? {}
      : { token_id: requiredInteger(value.token_id, endpoint) }),
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
    quota: requiredQuota(value.quota, endpoint),
    submit_time: requiredInteger(value.submit_time ?? 0, endpoint),
    finish_time: requiredInteger(value.finish_time ?? 0, endpoint),
    // Both flags are omitempty on the wire, so absence means false.
    legacy_video_available: requiredBoolean(
      value.legacy_video_available ?? false,
      endpoint
    ),
    result_discarded: requiredBoolean(
      value.result_discarded ?? false,
      endpoint
    ),
  }
}

export function parseTaskLogPage(value: unknown): PageResult<RelayTaskLogItem> {
  const endpoint = '/api/task/self'
  return parsePage(value, endpoint, parseRelayTaskLog)
}

const TASK_ARTIFACTS_ENDPOINT = '/api/task/:task_id/artifacts'
const TASK_ARTIFACT_TYPES = new Set<string>(['image', 'video', 'audio', 'file'])
const TASK_ARTIFACT_KEY = /^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$/
const TASK_ARTIFACT_CONTENT_PATH =
  /\/v1\/tasks\/[^/]+\/artifacts\/[^/]+\/content$/
// base64url (no padding) of an HMAC-SHA256 digest.
const TASK_ARTIFACT_ACCESS = /^[A-Za-z0-9_-]{43}$/
const MAX_TASK_ARTIFACTS = 64

function isTaskArtifactType(value: string): value is TaskArtifactType {
  return TASK_ARTIFACT_TYPES.has(value)
}

/**
 * Signed content URLs are rendered straight into media src and download
 * links, so only the exact gateway shape is accepted: absolute http(s), no
 * userinfo or fragment, the artifact content path, and a sole access token.
 */
function parseArtifactContentUrl(value: unknown): string {
  const endpoint = TASK_ARTIFACTS_ENDPOINT
  if (typeof value !== 'string' || !/^https?:\/\//i.test(value))
    invalidResponse(endpoint)
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f\\#\s]/.test(value)) invalidResponse(endpoint)
  const authority = value.slice(value.indexOf('//') + 2).split('/')[0]
  let url: URL
  try {
    url = new URL(value)
  } catch {
    invalidResponse(endpoint)
  }
  const access = url.searchParams.get('access')
  if (
    !url.hostname ||
    authority.includes('@') ||
    !TASK_ARTIFACT_CONTENT_PATH.test(url.pathname) ||
    access === null ||
    !TASK_ARTIFACT_ACCESS.test(access) ||
    url.search !== `?access=${access}`
  )
    invalidResponse(endpoint)
  return value
}

function parseTaskArtifact(value: unknown): TaskArtifact {
  const endpoint = TASK_ARTIFACTS_ENDPOINT
  if (!isRecord(value)) invalidResponse(endpoint)
  const key = requiredString(value.key, endpoint)
  const type = requiredString(value.type, endpoint)
  if (!TASK_ARTIFACT_KEY.test(key) || !isTaskArtifactType(type))
    invalidResponse(endpoint)
  const artifact: TaskArtifact = {
    key,
    type,
    content_url: parseArtifactContentUrl(value.content_url),
  }
  if (value.mime_type !== undefined) {
    const mimeType = requiredString(value.mime_type, endpoint).trim()
    if (mimeType.length > 255 || /[\r\n]/.test(mimeType))
      invalidResponse(endpoint)
    if (mimeType) artifact.mime_type = mimeType
  }
  return artifact
}

function boundedList(value: unknown): unknown[] {
  if (!Array.isArray(value) || value.length > MAX_TASK_ARTIFACTS)
    invalidResponse(TASK_ARTIFACTS_ENDPOINT)
  return value
}

function optionalText(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value : undefined
}

function optionalSeconds(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : undefined
}

/**
 * Legacy Suno clips are a passthrough of the stored upstream snapshot. The
 * audio URL is required and must be http(s); display fields from that
 * snapshot are best effort and dropped when they have an unexpected type.
 */
function parseTaskAudioClip(value: unknown): TaskAudioClip | null {
  if (!isRecord(value)) return null
  const audioUrl = safeExternalUrl(value.audio_url)
  if (!audioUrl) return null
  const metadata = isRecord(value.metadata) ? value.metadata : {}
  return {
    audio_url: audioUrl,
    clip_id: optionalText(value.clip_id),
    id: optionalText(value.id),
    title: optionalText(value.title),
    tags: optionalText(value.tags) ?? optionalText(metadata.tags),
    duration:
      optionalSeconds(value.duration) ?? optionalSeconds(metadata.duration),
    image_url:
      safeImageUrl(value.image_url) ??
      safeImageUrl(value.image_large_url) ??
      undefined,
  }
}

function audioClipList(value: unknown): unknown[] {
  if (!Array.isArray(value)) invalidResponse(TASK_ARTIFACTS_ENDPOINT)
  return value
}

export function parseTaskArtifactsResult(value: unknown): TaskArtifactsResult {
  const endpoint = TASK_ARTIFACTS_ENDPOINT
  if (!isRecord(value)) invalidResponse(endpoint)
  const artifacts = boundedList(value.artifacts).map(parseTaskArtifact)
  if (new Set(artifacts.map((item) => item.key)).size !== artifacts.length)
    invalidResponse(endpoint)
  const result: TaskArtifactsResult = {
    task_id: requiredString(value.task_id, endpoint),
    artifacts,
  }
  if (value.legacy_content_url !== undefined)
    result.legacy_content_url = parseArtifactContentUrl(
      value.legacy_content_url
    )
  if (value.legacy_audio_clips !== undefined) {
    // Invalid clips are discarded individually before applying the display
    // limit. Stop as soon as 64 valid clips are collected so an oversized
    // upstream snapshot cannot force a full parse of every trailing entry.
    const clips: TaskAudioClip[] = []
    for (const raw of audioClipList(value.legacy_audio_clips)) {
      const clip = parseTaskAudioClip(raw)
      if (!clip) continue
      clips.push(clip)
      if (clips.length === MAX_TASK_ARTIFACTS) break
    }
    result.legacy_audio_clips = clips
  }
  return result
}
