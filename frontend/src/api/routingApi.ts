import { api } from './console'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import type {
  EligibleRouteChannel,
  RouteCatalog,
  RouteCapabilityState,
  RouteEntry,
  RouteGroup,
  RouteHealthState,
  RouteHealthSummary,
  RoutePolicy,
  RoutePreview,
  RouteProfileInput,
  RouteProfileView,
  RouteRetryMode,
} from '@/types/routing'

const ROUTING_ENDPOINT = '/api/routing'

const capabilityStates = new Set<RouteCapabilityState>([
  'eligible',
  'unresolved',
  'unsupported',
  'disabled',
  'conflict',
])
const retryModes = new Set<RouteRetryMode>([
  'none',
  'same_channel',
  'next_channel',
  'same_then_next',
])

const healthStates = new Set<RouteHealthState>(['closed', 'open', 'half_open'])

function requiredNullableInteger(
  value: unknown,
  endpoint: string
): number | null {
  if (value === null || value === undefined) return null
  return requiredInteger(value, endpoint)
}

function parseCapabilityState(
  value: unknown,
  endpoint: string
): RouteCapabilityState {
  const state = requiredString(value, endpoint, false) as RouteCapabilityState
  if (!capabilityStates.has(state)) invalidResponse(endpoint)
  return state
}

function parseHealthSummary(
  value: unknown,
  endpoint: string
): RouteHealthSummary {
  if (!isRecord(value)) invalidResponse(endpoint)
  const state = requiredString(value.state, endpoint, false) as RouteHealthState
  if (!healthStates.has(state)) invalidResponse(endpoint)
  return {
    state,
    failure_count: requiredInteger(value.failure_count, endpoint),
    cooldown_until: requiredInteger(value.cooldown_until, endpoint),
    health_epoch: requiredInteger(value.health_epoch, endpoint),
    last_latency_ms: requiredInteger(value.last_latency_ms, endpoint),
    first_token_latency_ms: requiredInteger(
      value.first_token_latency_ms,
      endpoint
    ),
    updated_at: requiredInteger(value.updated_at, endpoint),
  }
}

function parsePolicy(value: unknown, endpoint: string): RoutePolicy {
  if (!isRecord(value)) invalidResponse(endpoint)
  const retryMode = requiredString(value.retry_mode, endpoint, false)
  if (!retryModes.has(retryMode as RouteRetryMode)) invalidResponse(endpoint)
  return {
    group_id: requiredInteger(value.group_id, endpoint),
    load_balance: requiredBoolean(value.load_balance, endpoint),
    max_ratio: requiredStrictNumber(value.max_ratio, endpoint),
    retry_mode: retryMode as RouteRetryMode,
    max_same_resource_attempts: requiredInteger(
      value.max_same_resource_attempts,
      endpoint
    ),
    max_failover_attempts: requiredInteger(
      value.max_failover_attempts,
      endpoint
    ),
    sticky: requiredBoolean(value.sticky, endpoint),
  }
}

function parseEntry(value: unknown, endpoint: string): RouteEntry {
  if (!isRecord(value)) invalidResponse(endpoint)
  const source = requiredString(value.source, endpoint, false)
  if (source !== 'platform') invalidResponse(endpoint)
  return {
    id: requiredInteger(value.id, endpoint),
    group_id: requiredInteger(value.group_id, endpoint),
    channel_id: requiredInteger(value.channel_id, endpoint),
    source,
    enabled: requiredBoolean(value.enabled, endpoint),
    position: requiredInteger(value.position, endpoint),
    weight: requiredInteger(value.weight, endpoint),
  }
}

function parseGroup(
  value: unknown,
  endpoint: string,
  allowPartial = false
): RouteGroup {
  if (!isRecord(value)) invalidResponse(endpoint)
  const groupValue = isRecord(value.group) ? value.group : value
  if (!allowPartial && !Array.isArray(value.entries)) invalidResponse(endpoint)
  const kind = requiredString(groupValue.kind, endpoint, false)
  if (kind !== 'manual' && kind !== 'auto_lab') invalidResponse(endpoint)
  const entries = Array.isArray(value.entries)
    ? value.entries.map((entry) => parseEntry(entry, endpoint))
    : []
  return {
    id: requiredInteger(groupValue.id, endpoint),
    profile_id: requiredInteger(groupValue.profile_id, endpoint),
    name: requiredString(groupValue.name, endpoint, false),
    kind,
    enabled: requiredBoolean(groupValue.enabled, endpoint),
    position: requiredInteger(groupValue.position, endpoint),
    entries,
    policy: isRecord(value.policy)
      ? parsePolicy(value.policy, endpoint)
      : {
          group_id: requiredInteger(groupValue.id, endpoint),
          load_balance: false,
          max_ratio: 1,
          retry_mode: 'next_channel',
          max_same_resource_attempts: 0,
          max_failover_attempts: 1,
          sticky: false,
        },
  }
}

export function parseRouteProfileView(
  value: unknown,
  endpoint: string
): RouteProfileView {
  if (
    !isRecord(value) ||
    !Array.isArray(value.groups) ||
    !isRecord(value.profile)
  ) {
    invalidResponse(endpoint)
  }
  const mode = requiredString(value.profile.mode, endpoint, false)
  if (mode !== 'legacy' && mode !== 'manual' && mode !== 'auto_lab')
    invalidResponse(endpoint)
  return {
    profile: {
      id: requiredInteger(value.profile.id, endpoint),
      user_id: requiredInteger(value.profile.user_id, endpoint),
      token_id: requiredInteger(value.profile.token_id, endpoint),
      mode,
      active_group_id: requiredNullableInteger(
        value.profile.active_group_id,
        endpoint
      ),
      version: requiredInteger(value.profile.version, endpoint),
      status: requiredInteger(value.profile.status, endpoint),
      created_at: requiredInteger(value.profile.created_at, endpoint),
      updated_at: requiredInteger(value.profile.updated_at, endpoint),
    },
    groups: value.groups.map((group) => parseGroup(group, endpoint)),
  }
}

function parseEligibleChannel(
  value: unknown,
  endpoint: string
): EligibleRouteChannel {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    id: requiredInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint, false),
    type: requiredInteger(value.type, endpoint),
    status: requiredInteger(value.status, endpoint),
    models: requiredString(value.models, endpoint),
    priority: requiredStrictNumber(value.priority, endpoint),
    weight: requiredInteger(value.weight, endpoint),
    snapshot_version: requiredInteger(value.snapshot_version, endpoint),
    catalog_version: requiredString(value.catalog_version, endpoint),
    capability_state: parseCapabilityState(value.capability_state, endpoint),
    filter_reason:
      value.filter_reason === undefined
        ? undefined
        : requiredString(value.filter_reason, endpoint),
  }
}

function parseCatalog(value: unknown): RouteCatalog {
  const endpoint = `${ROUTING_ENDPOINT}/catalog`
  if (
    !isRecord(value) ||
    !Array.isArray(value.items) ||
    !Array.isArray(value.catalog_versions)
  ) {
    invalidResponse(endpoint)
  }
  if (value.catalog_versions.some((version) => typeof version !== 'string'))
    invalidResponse(endpoint)
  return {
    catalog_version: requiredString(value.catalog_version, endpoint),
    catalog_versions: [...value.catalog_versions],
    items: value.items.map((item) => {
      if (!isRecord(item)) invalidResponse(endpoint)
      return {
        id: requiredInteger(item.id, endpoint),
        channel_id: requiredInteger(item.channel_id, endpoint),
        request_model: requiredString(item.request_model, endpoint, false),
        actual_model: requiredString(item.actual_model, endpoint),
        lab_slug: requiredString(item.lab_slug, endpoint),
        confidence: requiredStrictNumber(item.confidence, endpoint),
        source: requiredString(item.source, endpoint, false),
        catalog_version: requiredString(item.catalog_version, endpoint),
        snapshot_version: requiredInteger(item.snapshot_version, endpoint),
        state: parseCapabilityState(item.state, endpoint),
      }
    }),
  }
}

export function parseRoutePreview(value: unknown): RoutePreview {
  const endpoint = `${ROUTING_ENDPOINT}/profiles/:id/preview`
  if (
    !isRecord(value) ||
    !Array.isArray(value.entries) ||
    !Array.isArray(value.runtime_recheck_reasons)
  ) {
    invalidResponse(endpoint)
  }
  const selectionMode = requiredString(value.selection_mode, endpoint, false)
  if (selectionMode !== 'ordered' && selectionMode !== 'weighted')
    invalidResponse(endpoint)
  if (value.live_selection !== false) invalidResponse(endpoint)
  return {
    profile_id: requiredInteger(value.profile_id, endpoint),
    profile_version: requiredInteger(value.profile_version, endpoint),
    request_model: requiredString(value.request_model, endpoint, false),
    normalized_model: requiredString(value.normalized_model, endpoint, false),
    path: requiredString(value.path, endpoint, false),
    endpoint_type: requiredString(value.endpoint_type, endpoint),
    active_group:
      value.active_group === undefined
        ? undefined
        : parseGroup(value.active_group, endpoint, true),
    policy:
      value.policy === undefined
        ? undefined
        : parsePolicy(value.policy, endpoint),
    entries: value.entries.map((entry) => {
      if (!isRecord(entry)) invalidResponse(endpoint)
      return {
        entry_id: requiredInteger(entry.entry_id, endpoint),
        channel_id: requiredInteger(entry.channel_id, endpoint),
        position: requiredInteger(entry.position, endpoint),
        weight: requiredInteger(entry.weight, endpoint),
        request_model: requiredString(entry.request_model, endpoint, false),
        actual_model: requiredString(entry.actual_model, endpoint),
        lab_slug: requiredString(entry.lab_slug, endpoint),
        snapshot_version: requiredInteger(entry.snapshot_version, endpoint),
        catalog_version: requiredString(entry.catalog_version, endpoint),
        capability_state: parseCapabilityState(
          entry.capability_state,
          endpoint
        ),
        health: parseHealthSummary(entry.health, endpoint),
        filter_reason:
          entry.filter_reason === undefined
            ? undefined
            : requiredString(entry.filter_reason, endpoint),
      }
    }),
    candidate_channel_ids: parseIntegerArray(
      value.candidate_channel_ids,
      endpoint
    ),
    selection_mode: selectionMode,
    preferred_channel_id:
      value.preferred_channel_id === undefined
        ? undefined
        : requiredInteger(value.preferred_channel_id, endpoint),
    filter_reason_counts: parseReasonCounts(
      value.filter_reason_counts,
      endpoint
    ),
    has_mixed: requiredBoolean(value.has_mixed, endpoint),
    runtime_recheck_required: requiredBoolean(
      value.runtime_recheck_required,
      endpoint
    ),
    runtime_recheck_reasons: value.runtime_recheck_reasons.map((reason) =>
      requiredString(reason, endpoint, false)
    ),
    live_selection: false,
  }
}

function parseReasonCounts(
  value: unknown,
  endpoint: string
): Record<string, number> {
  if (!isRecord(value)) invalidResponse(endpoint)
  const result: Record<string, number> = {}
  for (const [key, count] of Object.entries(value))
    result[key] = requiredInteger(count, endpoint)
  return result
}

function parseIntegerArray(value: unknown, endpoint: string): number[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => requiredInteger(item, endpoint))
}

export const routingApi = {
  async profiles(): Promise<RouteProfileView[]> {
    const endpoint = `${ROUTING_ENDPOINT}/profiles`
    const value = await api.get<unknown>(endpoint)
    if (!Array.isArray(value)) invalidResponse(endpoint)
    return value.map((profile) => parseRouteProfileView(profile, endpoint))
  },
  async profile(id: number): Promise<RouteProfileView> {
    const endpoint = `${ROUTING_ENDPOINT}/profiles/${id}`
    return parseRouteProfileView(await api.get<unknown>(endpoint), endpoint)
  },
  async create(input: RouteProfileInput): Promise<RouteProfileView> {
    const endpoint = `${ROUTING_ENDPOINT}/profiles`
    return parseRouteProfileView(
      await api.post<unknown>(endpoint, input),
      endpoint
    )
  },
  async update(
    id: number,
    input: RouteProfileInput
  ): Promise<RouteProfileView> {
    const endpoint = `${ROUTING_ENDPOINT}/profiles/${id}`
    return parseRouteProfileView(
      await api.put<unknown>(endpoint, input),
      endpoint
    )
  },
  async remove(id: number): Promise<void> {
    await api.delete(`${ROUTING_ENDPOINT}/profiles/${id}`)
  },
  async eligibleChannels(): Promise<EligibleRouteChannel[]> {
    const endpoint = `${ROUTING_ENDPOINT}/eligible-channels`
    const value = await api.get<unknown>(endpoint)
    if (!Array.isArray(value)) invalidResponse(endpoint)
    return value.map((channel) => parseEligibleChannel(channel, endpoint))
  },
  async catalog(model?: string): Promise<RouteCatalog> {
    const value = await api.get<unknown>(
      `${ROUTING_ENDPOINT}/catalog`,
      model ? { model } : undefined
    )
    return parseCatalog(value)
  },
  async preview(
    id: number,
    input: { model: string; path: string }
  ): Promise<RoutePreview> {
    return parseRoutePreview(
      await api.post<unknown>(
        `${ROUTING_ENDPOINT}/profiles/${id}/preview`,
        input
      )
    )
  },
}
