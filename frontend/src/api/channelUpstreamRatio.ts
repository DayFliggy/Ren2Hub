import { api } from './client'
import { isDecimal } from '@/utils/decimal'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

export type UpstreamRatioKind = 'sub2api' | 'newapi' | 'detect' | 'unsupported'
export type UpstreamRatioSource = '' | 'sub2api' | 'newapi'
export type UpstreamRatioMode = 'auto' | 'manual'
export type UpstreamRatioStatus =
  | ''
  | 'ok'
  | 'needs_mapping'
  | 'unsupported'
  | 'auth_failed'
  | 'rate_limited'
  | 'failed'
export type UpstreamRatioMapping = '' | 'name_match' | 'manual'

export interface UpstreamRatioGroup {
  name: string
  description: string
  ratio: number | null
}

export interface Sub2APIBilling {
  group_rate_multiplier: number
  user_rate_multiplier: number | null
  resolved_rate_multiplier: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  timezone: string
}

/** Display-only upstream ratio; it never changes channel_ratio or routing. */
export interface ChannelUpstreamRatio {
  channel_id: number
  kind: UpstreamRatioKind
  source: UpstreamRatioSource
  mode: UpstreamRatioMode
  status: UpstreamRatioStatus
  ratio: string
  effective_ratio: string
  group_name: string
  mapping: UpstreamRatioMapping
  token_name: string
  stale: boolean
  received_at: number
  next_probe_at: number
  last_error: string
  version: number
  sub2api: Sub2APIBilling | null
  groups: UpstreamRatioGroup[]
}

export interface ChannelUpstreamRatioUpdate {
  mode: UpstreamRatioMode
  ratio?: string
  group_name?: string
  version: number
}

const listEndpoint = '/api/channel/upstream_ratios'
const kinds = ['sub2api', 'newapi', 'detect', 'unsupported']
const sources = ['', 'sub2api', 'newapi']
const statuses = [
  '',
  'ok',
  'needs_mapping',
  'unsupported',
  'auth_failed',
  'rate_limited',
  'failed',
]
const mappings = ['', 'name_match', 'manual']

function requiredEnum<T extends string>(
  value: unknown,
  allowed: string[],
  endpoint: string
): T {
  if (typeof value !== 'string' || !allowed.includes(value))
    invalidResponse(endpoint)
  return value as T
}

function optionalRatio(value: unknown, endpoint: string): string {
  const ratio = requiredString(value, endpoint)
  if (ratio !== '' && !isDecimal(ratio)) invalidResponse(endpoint)
  return ratio
}

function parseGroups(value: unknown, endpoint: string): UpstreamRatioGroup[] {
  if (value === undefined) return []
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => {
    if (!isRecord(item)) invalidResponse(endpoint)
    return {
      name: requiredString(item.name, endpoint),
      description:
        item.description === undefined
          ? ''
          : requiredString(item.description, endpoint),
      ratio:
        item.ratio === undefined
          ? null
          : requiredStrictNumber(item.ratio, endpoint),
    }
  })
}

function parseSub2API(value: unknown, endpoint: string): Sub2APIBilling | null {
  if (value === undefined) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  const text = (field: unknown) =>
    field === undefined ? '' : requiredString(field, endpoint)
  return {
    group_rate_multiplier: requiredStrictNumber(
      value.group_rate_multiplier,
      endpoint
    ),
    user_rate_multiplier:
      value.user_rate_multiplier === undefined
        ? null
        : requiredStrictNumber(value.user_rate_multiplier, endpoint),
    resolved_rate_multiplier: requiredStrictNumber(
      value.resolved_rate_multiplier,
      endpoint
    ),
    peak_rate_enabled: requiredBoolean(value.peak_rate_enabled, endpoint),
    peak_start: text(value.peak_start),
    peak_end: text(value.peak_end),
    peak_rate_multiplier: requiredStrictNumber(
      value.peak_rate_multiplier,
      endpoint
    ),
    timezone: text(value.timezone),
  }
}

export function parseChannelUpstreamRatio(
  value: unknown,
  endpoint = listEndpoint
): ChannelUpstreamRatio {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    channel_id: requiredStrictInteger(value.channel_id, endpoint),
    kind: requiredEnum(value.kind, kinds, endpoint),
    source: requiredEnum(value.source, sources, endpoint),
    mode: requiredEnum(value.mode, ['auto', 'manual'], endpoint),
    status: requiredEnum(value.status, statuses, endpoint),
    ratio: optionalRatio(value.ratio, endpoint),
    effective_ratio: optionalRatio(value.effective_ratio, endpoint),
    group_name: requiredString(value.group_name, endpoint),
    mapping: requiredEnum(value.mapping, mappings, endpoint),
    token_name: requiredString(value.token_name, endpoint),
    stale: requiredBoolean(value.stale, endpoint),
    received_at: requiredStrictInteger(value.received_at, endpoint),
    next_probe_at: requiredStrictInteger(value.next_probe_at, endpoint),
    last_error: requiredString(value.last_error, endpoint),
    version: requiredStrictInteger(value.version, endpoint),
    sub2api: parseSub2API(value.sub2api, endpoint),
    groups: parseGroups(value.groups, endpoint),
  }
}

export const channelUpstreamRatioApi = {
  async list(
    ids: number[],
    signal?: AbortSignal
  ): Promise<ChannelUpstreamRatio[]> {
    if (!ids.length) return []
    const value = await api.get<unknown>(
      listEndpoint,
      { ids: ids.join(',') },
      { signal }
    )
    if (!Array.isArray(value)) invalidResponse(listEndpoint)
    return value.map((item) => parseChannelUpstreamRatio(item))
  },
  async get(id: number, signal?: AbortSignal) {
    const value = await api.get<unknown>(
      listEndpoint,
      { ids: String(id), with_groups: 'true' },
      { signal }
    )
    if (!Array.isArray(value) || value.length !== 1)
      invalidResponse(listEndpoint)
    return parseChannelUpstreamRatio(value[0])
  },
  async refresh(id: number, signal?: AbortSignal) {
    const url = `/api/channel/${id}/upstream_ratio/refresh`
    return parseChannelUpstreamRatio(
      await api.post<unknown>(url, undefined, { signal }),
      url
    )
  },
  async update(
    id: number,
    value: ChannelUpstreamRatioUpdate,
    signal?: AbortSignal
  ) {
    const url = `/api/channel/${id}/upstream_ratio`
    return parseChannelUpstreamRatio(
      await api.put<unknown>(url, value, { signal }),
      url
    )
  },
}
