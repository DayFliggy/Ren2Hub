import type {
  DrawingLogItem,
  LogItem,
  LogType,
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
  requiredStrictNumber,
  requiredBoolean,
  requiredQuota,
  requiredNumber,
  requiredString,
} from './contracts'
import { safeExternalUrl, safeImageUrl } from '@/utils/safeUrl'
import { ApiError, type PageResult } from './types'

export interface UsageRow {
  model_name: string
  created_at: number
  count: number
  quota: string
  token_used: number
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
  ordinary_pricing?: boolean
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
export interface PerfSuccessRatePoint {
  ts: number
  success_rate: number
}
export interface PerfBucketPoint {
  ts: number
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}
export interface PerfModelSummaryContract {
  model_name: string
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_series?: PerfSuccessRatePoint[]
}
export interface PerfModelDetailContract {
  model_name: string
  summary: {
    avg_latency_ms: number
    success_rate: number
    avg_tps: number
  } | null
  series: PerfBucketPoint[]
  window_start: number
  window_end: number
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
        const number = requiredStrictNumber(item[key], endpoint)
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
    if (item.ordinary_pricing !== undefined) {
      if (typeof item.ordinary_pricing !== 'boolean') invalidResponse(endpoint)
      result.ordinary_pricing = item.ordinary_pricing
    }
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

function parseSuccessRateSeries(
  value: unknown,
  endpoint: string
): PerfSuccessRatePoint[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const ts = requiredInteger(item.ts, endpoint)
    const success_rate = requiredNumber(item.success_rate, endpoint)
    if (ts < 0 || success_rate < 0 || success_rate > 100)
      invalidResponse(endpoint)
    return { ts, success_rate }
  })
}

/** pkg/perf_metrics ModelSummary: success_rate is a 0–100 percentage and
 * avg_tps is 0 when no generation timing was sampled. */
export function parsePerfMetricsSummary(
  value: unknown
): PerfModelSummaryContract[] {
  const endpoint = '/api/perf-metrics/summary'
  if (!isRecord(value) || !Array.isArray(value.models)) {
    invalidResponse(endpoint)
  }
  const topSeries = isRecord(value.recent_success_series)
    ? value.recent_success_series
    : undefined
  return value.models.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const model_name = requiredString(item.model_name, endpoint, false)
    const result: PerfModelSummaryContract = {
      model_name,
      avg_latency_ms: requiredInteger(item.avg_latency_ms, endpoint),
      success_rate: requiredNumber(item.success_rate, endpoint),
      avg_tps: requiredNumber(item.avg_tps, endpoint),
    }
    if (
      result.avg_latency_ms < 0 ||
      result.success_rate < 0 ||
      result.success_rate > 100 ||
      result.avg_tps < 0
    )
      invalidResponse(endpoint)
    const rawSeries = item.recent_success_series ?? topSeries?.[model_name]
    if (rawSeries !== undefined) {
      result.recent_success_series = parseSuccessRateSeries(rawSeries, endpoint)
    }
    return result
  })
}

/** pkg/perf_metrics QueryResult: model-wide hourly buckets across all channels. */
export function parsePerfMetricsDetail(
  value: unknown
): PerfModelDetailContract {
  const endpoint = '/api/perf-metrics'
  if (!isRecord(value) || !Array.isArray(value.series)) {
    invalidResponse(endpoint)
  }
  const model_name = requiredString(value.model_name, endpoint, false)
  const window_start = requiredInteger(value.window_start, endpoint)
  const window_end = requiredInteger(value.window_end, endpoint)
  let summary: PerfModelDetailContract['summary'] = null
  if (value.summary !== null && value.summary !== undefined) {
    if (!isRecord(value.summary)) invalidResponse(endpoint)
    const avg_latency_ms = requiredInteger(
      value.summary.avg_latency_ms,
      endpoint
    )
    const success_rate = requiredNumber(value.summary.success_rate, endpoint)
    const avg_tps = requiredNumber(value.summary.avg_tps, endpoint)
    if (
      avg_latency_ms < 0 ||
      success_rate < 0 ||
      success_rate > 100 ||
      avg_tps < 0
    )
      invalidResponse(endpoint)
    summary = { avg_latency_ms, success_rate, avg_tps }
  }
  const series = value.series.map((item): PerfBucketPoint => {
    if (!isRecord(item)) invalidResponse(endpoint)
    const point: PerfBucketPoint = {
      ts: requiredInteger(item.ts, endpoint),
      avg_ttft_ms: requiredInteger(item.avg_ttft_ms ?? 0, endpoint),
      avg_latency_ms: requiredInteger(item.avg_latency_ms, endpoint),
      success_rate: requiredNumber(item.success_rate, endpoint),
      avg_tps: requiredNumber(item.avg_tps, endpoint),
    }
    if (
      point.ts < 0 ||
      point.avg_ttft_ms < 0 ||
      point.avg_latency_ms < 0 ||
      point.success_rate < 0 ||
      point.success_rate > 100 ||
      point.avg_tps < 0
    )
      invalidResponse(endpoint)
    return point
  })
  return { model_name, summary, series, window_start, window_end }
}

/** The native global aggregate is weighted by request count, independently of models. */
export function parsePerfMetricsSummaryResult(value: unknown) {
  const models = parsePerfMetricsSummary(value)
  if (!isRecord(value)) invalidResponse('/api/perf-metrics/summary')
  const summary = value.summary
  if (summary === null) return { models, summary: null }
  if (!isRecord(summary)) invalidResponse('/api/perf-metrics/summary')
  const avgLatency = requiredStrictInteger(
    summary.avg_latency_ms,
    '/api/perf-metrics/summary'
  )
  if (avgLatency < 0) invalidResponse('/api/perf-metrics/summary')
  return { models, summary: { avg_latency_ms: avgLatency } }
}

export interface LogStatContract {
  quota: string
  rpm: number
  tpm: number
}
export function parseLogStat(value: unknown): LogStatContract {
  const endpoint = '/api/log/self/stat'
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    quota: requiredQuota(value.quota, endpoint),
    rpm: requiredInteger(value.rpm, endpoint),
    tpm: requiredInteger(value.tpm, endpoint),
  }
}

export function parseToken(value: unknown, endpoint: string): TokenSummary {
  if (!isRecord(value)) invalidResponse(endpoint)
  const type = requiredString(value.type, endpoint)
  if (type !== 'auto' && type !== 'ordered') invalidResponse(endpoint)
  const routeChannelCount = requiredStrictInteger(
    value.route_channel_count,
    endpoint
  )
  if (routeChannelCount < 0) invalidResponse(endpoint)
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
  return {
    id: requiredInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint, false),
    key_preview: requiredString(value.key, endpoint),
    type,
    selected_only: requiredBoolean(value.selected_only, endpoint),
    route_channel_count: routeChannelCount,
    allow_user_quota_query:
      value.allow_user_quota_query === undefined
        ? false
        : requiredBoolean(value.allow_user_quota_query, endpoint),
    status: requiredInteger(value.status, endpoint),
    used_quota: requiredQuota(value.used_quota, endpoint),
    remain_quota: requiredStrictInteger(value.remain_quota, endpoint),
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

/**
 * controller/user.go TopUp: data is the credited total of the code and
 * bonus_quota the bonus included in it.
 */
export function parseRedemption(value: unknown): {
  quota: string
  bonus_quota: string
} {
  const endpoint = '/api/user/topup'
  if (!isRecord(value)) return invalidResponse(endpoint)
  const quota = requiredQuota(value.data, endpoint)
  const bonus = requiredQuota(value.bonus_quota, endpoint)
  if (BigInt(bonus) < 0n || BigInt(bonus) >= BigInt(quota))
    return invalidResponse(endpoint)
  return { quota, bonus_quota: bonus }
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

function optionalLogNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
    ? value
    : undefined
}

function parseLog(value: unknown, endpoint: string): LogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  const numericType = requiredInteger(value.type, endpoint)
  const type = LOG_TYPES[numericType]
  if (!type) invalidResponse(endpoint)
  const other = parseLogOther(value.other, endpoint)
  const firstToken = isRecord(other) ? Number(other.frt) : Number.NaN
  const cacheCreationTokens12h = isRecord(other)
    ? optionalLogNumber(other.cache_creation_tokens_12h)
    : undefined
  const cacheCreationRatio12h = isRecord(other)
    ? optionalLogNumber(other.cache_creation_ratio_12h)
    : undefined
  const promptTokens = requiredInteger(value.prompt_tokens, endpoint)
  const completionTokens = requiredInteger(value.completion_tokens, endpoint)
  const latency = requiredInteger(value.use_time, endpoint)
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
    ...(cacheCreationTokens12h !== undefined
      ? { cache_creation_tokens_12h: cacheCreationTokens12h }
      : {}),
    ...(cacheCreationRatio12h !== undefined
      ? { cache_creation_ratio_12h: cacheCreationRatio12h }
      : {}),
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

const AUDIT_ACTOR_ROLES = new Set([1, 10, 100])

function optionalAuditString(value: unknown, endpoint: string): string {
  if (value === undefined || value === null) return ''
  return requiredString(value, endpoint)
}

function parseAuditLog(value: unknown, endpoint: string): AuditLogItem {
  if (!isRecord(value)) invalidResponse(endpoint)
  // Like upstream, the category is an open string: historical rows keep
  // categories the filter no longer offers.
  const category = requiredString(value.category, endpoint, false)
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
