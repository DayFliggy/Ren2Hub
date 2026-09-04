import { api } from './console'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredInteger,
  requiredNumber,
  requiredStrictInteger,
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
} from '@/types/routing'

const ROUTING_ENDPOINT = '/api/routing'

const capabilityStates = new Set<RouteCapabilityState>([
  'eligible',
  'unresolved',
  'unsupported',
  'disabled',
  'conflict',
])

const healthStates = new Set<RouteHealthState>(['closed', 'open', 'half_open'])

const retryModes = new Set([
  'none',
  'same_channel',
  'next_channel',
  'same_then_next',
])

function requiredNullableInteger(
  value: unknown,
  endpoint: string
): number | null {
  if (value === null || value === undefined) return null
  return requiredStrictInteger(value, endpoint)
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
    failure_count: requiredStrictInteger(value.failure_count, endpoint),
    cooldown_until: requiredStrictInteger(value.cooldown_until, endpoint),
    health_epoch: requiredStrictInteger(value.health_epoch, endpoint),
    last_latency_ms: requiredStrictInteger(value.last_latency_ms, endpoint),
    first_token_latency_ms: requiredStrictInteger(
      value.first_token_latency_ms,
      endpoint
    ),
    updated_at: requiredStrictInteger(value.updated_at, endpoint),
  }
}

function parsePolicy(value: unknown, endpoint: string): RoutePolicy {
  if (!isRecord(value)) invalidResponse(endpoint)
  const retryMode = requiredString(value.retry_mode, endpoint, false)
  if (!retryModes.has(retryMode)) invalidResponse(endpoint)
  return {
    group_id: requiredStrictInteger(value.group_id, endpoint),
    load_balance: requiredBoolean(value.load_balance, endpoint),
    max_ratio: requiredStrictNumber(value.max_ratio, endpoint),
    retry_mode: retryMode,
    max_same_resource_attempts: requiredStrictInteger(
      value.max_same_resource_attempts,
      endpoint
    ),
    max_failover_attempts: requiredStrictInteger(
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
    id: requiredStrictInteger(value.id, endpoint),
    group_id: requiredStrictInteger(value.group_id, endpoint),
    channel_id: requiredStrictInteger(value.channel_id, endpoint),
    source,
    enabled: requiredBoolean(value.enabled, endpoint),
    position: requiredStrictInteger(value.position, endpoint),
    weight: requiredStrictInteger(value.weight, endpoint),
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
    id: requiredStrictInteger(groupValue.id, endpoint),
    profile_id: requiredStrictInteger(groupValue.profile_id, endpoint),
    name: requiredString(groupValue.name, endpoint, false),
    kind,
    enabled: requiredBoolean(groupValue.enabled, endpoint),
    position: requiredStrictInteger(groupValue.position, endpoint),
    entries,
    policy: isRecord(value.policy)
      ? parsePolicy(value.policy, endpoint)
      : allowPartial
        ? {
            group_id: requiredStrictInteger(groupValue.id, endpoint),
            load_balance: false,
            max_ratio: 1,
            retry_mode: 'next_channel',
            max_same_resource_attempts: 0,
            max_failover_attempts: 1,
            sticky: false,
          }
        : invalidResponse(endpoint),
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
  const mode = requiredString(
    value.profile.mode,
    endpoint,
    false
  ) as RouteProfileView['profile']['mode']
  if (mode !== 'legacy' && mode !== 'manual' && mode !== 'auto_lab')
    invalidResponse(endpoint)
  const profile = {
    id: requiredStrictInteger(value.profile.id, endpoint),
    user_id: requiredStrictInteger(value.profile.user_id, endpoint),
    token_id: requiredStrictInteger(value.profile.token_id, endpoint),
    mode,
    active_group_id: requiredNullableInteger(
      value.profile.active_group_id,
      endpoint
    ),
    version: requiredStrictInteger(value.profile.version, endpoint),
    status: requiredStrictInteger(value.profile.status, endpoint),
    created_at: requiredStrictInteger(value.profile.created_at, endpoint),
    updated_at: requiredStrictInteger(value.profile.updated_at, endpoint),
  }
  const groups = value.groups.map((group) => parseGroup(group, endpoint))
  assertRouteProfileInvariants(profile, groups, endpoint)
  return { profile, groups }
}

function assertRouteProfileInvariants(
  profile: RouteProfileView['profile'],
  groups: RouteGroup[],
  endpoint: string
): void {
  const groupIDs = new Set<number>()
  const groupPositions = new Set<number>()
  let previousPosition = -1
  let previousID = -1
  for (const group of groups) {
    if (
      group.profile_id !== profile.id ||
      group.policy.group_id !== group.id ||
      groupIDs.has(group.id) ||
      groupPositions.has(group.position) ||
      !isPositionOrdered(group.position, group.id, previousPosition, previousID)
    ) {
      invalidResponse(endpoint)
    }
    groupIDs.add(group.id)
    groupPositions.add(group.position)
    previousPosition = group.position
    previousID = group.id
    assertRouteEntryInvariants(group.entries, group.id, false, endpoint)
  }
  if (
    profile.active_group_id !== null &&
    !groupIDs.has(profile.active_group_id)
  ) {
    invalidResponse(endpoint)
  }
}

function assertRouteEntryInvariants(
  entries: RouteEntry[],
  groupID: number,
  loadBalance: boolean,
  endpoint: string
): void {
  const entryIDs = new Set<number>()
  const channelIDs = new Set<number>()
  let previousPosition = -1
  let previousWeight = Number.POSITIVE_INFINITY
  let previousID = -1
  for (const entry of entries) {
    if (
      entry.group_id !== groupID ||
      entryIDs.has(entry.id) ||
      channelIDs.has(entry.channel_id) ||
      entry.position < 0 ||
      entry.weight < 0
    ) {
      invalidResponse(endpoint)
    }
    if (entry.position < previousPosition) invalidResponse(endpoint)
    if (entry.position === previousPosition) {
      if (loadBalance && entry.weight > previousWeight)
        invalidResponse(endpoint)
      if (
        (!loadBalance || entry.weight === previousWeight) &&
        entry.id <= previousID
      ) {
        invalidResponse(endpoint)
      }
    }
    entryIDs.add(entry.id)
    channelIDs.add(entry.channel_id)
    previousPosition = entry.position
    previousWeight = entry.weight
    previousID = entry.id
  }
}

function isPositionOrdered(
  position: number,
  id: number,
  previousPosition: number,
  previousID: number
): boolean {
  return (
    position >= 0 &&
    (position > previousPosition ||
      (position === previousPosition && id > previousID))
  )
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
    priority: requiredNumber(value.priority, endpoint),
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
        confidence: requiredNumber(item.confidence, endpoint),
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
  const activeGroup =
    value.active_group === undefined
      ? undefined
      : parseGroup(value.active_group, endpoint, true)
  const policy =
    value.policy === undefined ? undefined : parsePolicy(value.policy, endpoint)
  const entries = value.entries.map((entry) => {
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
      capability_state: parseCapabilityState(entry.capability_state, endpoint),
      health: parseHealthSummary(entry.health, endpoint),
      filter_reason:
        entry.filter_reason === undefined
          ? undefined
          : requiredString(entry.filter_reason, endpoint),
    }
  })
  const candidateChannelIDs = parseIntegerArray(
    value.candidate_channel_ids,
    endpoint
  )
  const preferredChannelID =
    value.preferred_channel_id === undefined
      ? undefined
      : requiredInteger(value.preferred_channel_id, endpoint)
  assertRoutePreviewInvariants(
    activeGroup,
    policy,
    entries,
    candidateChannelIDs,
    preferredChannelID,
    selectionMode,
    endpoint
  )
  return {
    profile_id: requiredInteger(value.profile_id, endpoint),
    profile_version: requiredInteger(value.profile_version, endpoint),
    request_model: requiredString(value.request_model, endpoint, false),
    normalized_model: requiredString(value.normalized_model, endpoint, false),
    path: requiredString(value.path, endpoint, false),
    endpoint_type: requiredString(value.endpoint_type, endpoint),
    active_group: activeGroup,
    policy,
    entries,
    candidate_channel_ids: candidateChannelIDs,
    selection_mode: selectionMode,
    preferred_channel_id: preferredChannelID,
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

function assertRoutePreviewInvariants(
  activeGroup: RouteGroup | undefined,
  policy: RoutePolicy | undefined,
  entries: RoutePreview['entries'],
  candidateChannelIDs: number[],
  preferredChannelID: number | undefined,
  selectionMode: RoutePreview['selection_mode'],
  endpoint: string
): void {
  if ((activeGroup === undefined) !== (policy === undefined)) {
    invalidResponse(endpoint)
  }
  if (activeGroup && policy && policy.group_id !== activeGroup.id) {
    invalidResponse(endpoint)
  }
  const channelIDs = new Set<number>()
  const entryIDs = new Set<number>()
  const candidateSet = new Set<number>()
  let previousPosition = -1
  let previousWeight = Number.POSITIVE_INFINITY
  let previousChannelID = -1
  for (const entry of entries) {
    if (
      entry.entry_id <= 0 ||
      entry.channel_id <= 0 ||
      entry.position < 0 ||
      entry.weight < 0 ||
      entryIDs.has(entry.entry_id) ||
      channelIDs.has(entry.channel_id)
    ) {
      invalidResponse(endpoint)
    }
    if (entry.position < previousPosition) invalidResponse(endpoint)
    if (entry.position === previousPosition) {
      if (selectionMode === 'weighted' && entry.weight > previousWeight) {
        invalidResponse(endpoint)
      }
      if (
        (selectionMode !== 'weighted' || entry.weight === previousWeight) &&
        entry.channel_id <= previousChannelID
      ) {
        invalidResponse(endpoint)
      }
    }
    entryIDs.add(entry.entry_id)
    channelIDs.add(entry.channel_id)
    previousPosition = entry.position
    previousWeight = entry.weight
    previousChannelID = entry.channel_id
  }
  for (const channelID of candidateChannelIDs) {
    if (candidateSet.has(channelID)) invalidResponse(endpoint)
    const entry = entries.find((item) => item.channel_id === channelID)
    if (!entry || entry.filter_reason) invalidResponse(endpoint)
    candidateSet.add(channelID)
  }
  if (
    preferredChannelID !== undefined &&
    !candidateSet.has(preferredChannelID)
  ) {
    invalidResponse(endpoint)
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
