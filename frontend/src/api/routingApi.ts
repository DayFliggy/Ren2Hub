import { api } from './console'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredInteger,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import type {
  EligibleRouteChannel,
  RouteCatalog,
  RouteCapabilityState,
  RouteDecision,
  RouteDiagnosticWarningCode,
  RouteEntry,
  RouteGroup,
  RouteHealthState,
  RouteHealthSummary,
  RouteScoreBreakdown,
  RoutingDiagnostics,
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
const diagnosticWarningCodes = new Set<RouteDiagnosticWarningCode>([
  'route_lease_failures',
  'route_decision_events_dropped',
  'capability_refresh_lagging',
  'route_score_metrics_unavailable',
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
  if (!retryModes.has(retryMode as RouteRetryMode)) invalidResponse(endpoint)
  return {
    group_id: requiredStrictInteger(value.group_id, endpoint),
    load_balance: requiredBoolean(value.load_balance, endpoint),
    max_ratio: requiredStrictNumber(value.max_ratio, endpoint),
    retry_mode: retryMode as RouteRetryMode,
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
  if (mode !== 'manual' && mode !== 'auto_lab') invalidResponse(endpoint)
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
  const entryIDs = new Set<number>()
  const channelIDs = new Set<number>()
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
    assertRouteEntryInvariants(
      group.entries,
      group.id,
      group.policy.load_balance,
      endpoint,
      entryIDs,
      channelIDs
    )
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
  endpoint: string,
  globalEntryIDs?: Set<number>,
  globalChannelIDs?: Set<number>
): void {
  const entryIDs = new Set<number>()
  const channelIDs = new Set<number>()
  let previousPosition = -1
  let previousWeight = Number.POSITIVE_INFINITY
  let previousChannelID = -1
  let previousID = -1
  for (const entry of entries) {
    if (
      entry.group_id !== groupID ||
      entryIDs.has(entry.id) ||
      channelIDs.has(entry.channel_id) ||
      globalEntryIDs?.has(entry.id) ||
      globalChannelIDs?.has(entry.channel_id) ||
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
        (loadBalance
          ? entry.channel_id < previousChannelID ||
            (entry.channel_id === previousChannelID && entry.id <= previousID)
          : entry.id <= previousID)
      ) {
        invalidResponse(endpoint)
      }
    }
    entryIDs.add(entry.id)
    channelIDs.add(entry.channel_id)
    globalEntryIDs?.add(entry.id)
    globalChannelIDs?.add(entry.channel_id)
    previousPosition = entry.position
    previousWeight = entry.weight
    previousChannelID = entry.channel_id
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

export function parseEligibleRouteChannel(
  value: unknown,
  endpoint: string
): EligibleRouteChannel {
  if (!isRecord(value)) invalidResponse(endpoint)
  const channelRPM = requiredStrictInteger(value.channel_rpm, endpoint)
  const channelTPM = requiredStrictInteger(value.channel_tpm, endpoint)
  if (
    channelRPM < 0 ||
    channelRPM > 10_000_000 ||
    channelTPM < 0 ||
    channelTPM > 10_000_000_000
  ) {
    invalidResponse(endpoint)
  }
  return {
    id: requiredInteger(value.id, endpoint),
    name: requiredString(value.name, endpoint, false),
    type: requiredInteger(value.type, endpoint),
    status: requiredInteger(value.status, endpoint),
    request_models: parseStringArray(value.request_models, endpoint),
    priority: requiredStrictNumber(value.priority, endpoint),
    weight: requiredInteger(value.weight, endpoint),
    channel_rpm: channelRPM,
    channel_tpm: channelTPM,
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

function optionalString(value: unknown, endpoint: string): string | undefined {
  return value === undefined
    ? undefined
    : requiredString(value, endpoint, false)
}

function optionalPositiveInteger(
  value: unknown,
  endpoint: string
): number | undefined {
  if (value === undefined) return undefined
  const parsed = requiredStrictInteger(value, endpoint)
  if (parsed <= 0) invalidResponse(endpoint)
  return parsed
}

function optionalBoolean(
  value: unknown,
  endpoint: string
): boolean | undefined {
  return value === undefined ? undefined : requiredBoolean(value, endpoint)
}

function parseRouteScoreBreakdown(
  value: unknown,
  endpoint: string
): RouteScoreBreakdown {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    priority_layer: requiredStrictInteger(value.priority_layer, endpoint),
    weight_score: requiredStrictNumber(value.weight_score, endpoint),
    error_score: requiredStrictNumber(value.error_score, endpoint),
    error_known: requiredBoolean(value.error_known, endpoint),
    latency_score: requiredStrictNumber(value.latency_score, endpoint),
    latency_known: requiredBoolean(value.latency_known, endpoint),
    ttft_score: requiredStrictNumber(value.ttft_score, endpoint),
    ttft_known: requiredBoolean(value.ttft_known, endpoint),
    rate_limit_score: requiredStrictNumber(value.rate_limit_score, endpoint),
    rate_limit_known: requiredBoolean(value.rate_limit_known, endpoint),
    quota_score: requiredStrictNumber(value.quota_score, endpoint),
    quota_known: requiredBoolean(value.quota_known, endpoint),
    circuit_breaker_score: requiredStrictNumber(
      value.circuit_breaker_score,
      endpoint
    ),
    sticky_score: requiredStrictNumber(value.sticky_score, endpoint),
    total: requiredStrictNumber(value.total, endpoint),
  }
}

function parseRouteDecision(value: unknown, endpoint: string): RouteDecision {
  if (!isRecord(value) || !Array.isArray(value.candidates)) {
    invalidResponse(endpoint)
  }
  const routeSource = requiredString(value.route_source, endpoint, false)
  if (
    routeSource !== 'unavailable' &&
    routeSource !== 'manual' &&
    routeSource !== 'auto_lab'
  ) {
    invalidResponse(endpoint)
  }
  const event = requiredString(value.event, endpoint, false)
  if (event !== 'route_decision') invalidResponse(endpoint)
  const scoringMode = optionalString(value.scoring_mode, endpoint)
  if (
    scoringMode !== undefined &&
    scoringMode !== 'off' &&
    scoringMode !== 'live'
  ) {
    invalidResponse(endpoint)
  }
  const candidates = value.candidates.map((candidate) => {
    if (!isRecord(candidate)) invalidResponse(endpoint)
    const channelID = requiredStrictInteger(candidate.channel_id, endpoint)
    const priority = requiredStrictInteger(candidate.priority, endpoint)
    const position = requiredStrictInteger(candidate.position, endpoint)
    const weight = requiredStrictInteger(candidate.weight, endpoint)
    if (channelID <= 0 || position < 0 || weight < 0) {
      invalidResponse(endpoint)
    }
    return {
      channel_id: channelID,
      request_model: optionalString(candidate.request_model, endpoint),
      actual_model: optionalString(candidate.actual_model, endpoint),
      filter_reason: optionalString(candidate.filter_reason, endpoint),
      priority,
      position,
      weight,
      snapshot_version: optionalPositiveInteger(
        candidate.snapshot_version,
        endpoint
      ),
      health_epoch: optionalPositiveInteger(candidate.health_epoch, endpoint),
      catalog_version: optionalString(candidate.catalog_version, endpoint),
      lease_state: optionalString(candidate.lease_state, endpoint),
      score_breakdown:
        candidate.score_breakdown === undefined
          ? undefined
          : parseRouteScoreBreakdown(candidate.score_breakdown, endpoint),
    }
  })
  const selectedChannelID = optionalPositiveInteger(
    value.selected_channel_id,
    endpoint
  )
  if (
    selectedChannelID !== undefined &&
    !candidates.some((candidate) => candidate.channel_id === selectedChannelID)
  ) {
    invalidResponse(endpoint)
  }
  const retryAttempt = requiredStrictInteger(value.retry_attempt, endpoint)
  const sameResourceRetryAttempt = requiredStrictInteger(
    value.same_resource_retry_attempt,
    endpoint
  )
  const failoverAttempt = requiredStrictInteger(
    value.failover_attempt,
    endpoint
  )
  const generatedAt = requiredStrictInteger(value.generated_at, endpoint)
  if (
    retryAttempt < 0 ||
    sameResourceRetryAttempt < 0 ||
    failoverAttempt < 0 ||
    generatedAt <= 0
  ) {
    invalidResponse(endpoint)
  }
  return {
    event,
    request_id: requiredString(value.request_id, endpoint, false),
    route_source: routeSource,
    configuration_version: optionalPositiveInteger(
      value.configuration_version,
      endpoint
    ),
    request_model: requiredString(value.request_model, endpoint, false),
    actual_model: optionalString(value.actual_model, endpoint),
    lab_slug: optionalString(value.lab_slug, endpoint),
    catalog_version: optionalString(value.catalog_version, endpoint),
    snapshot_version: optionalPositiveInteger(value.snapshot_version, endpoint),
    scoring_mode: scoringMode,
    dynamic_score_applied: requiredBoolean(
      value.dynamic_score_applied,
      endpoint
    ),
    score_metrics_unavailable: optionalBoolean(
      value.score_metrics_unavailable,
      endpoint
    ),
    static_preferred_channel_id: optionalPositiveInteger(
      value.static_preferred_channel_id,
      endpoint
    ),
    scored_preferred_channel_id: optionalPositiveInteger(
      value.scored_preferred_channel_id,
      endpoint
    ),
    candidates,
    selected_channel_id: selectedChannelID,
    retry_attempt: retryAttempt,
    same_resource_retry_attempt: sameResourceRetryAttempt,
    failover_attempt: failoverAttempt,
    lease_state: optionalString(value.lease_state, endpoint),
    final_error: optionalString(value.final_error, endpoint),
    generated_at: generatedAt,
  }
}

export function parseRoutingDiagnostics(value: unknown): RoutingDiagnostics {
  const endpoint = `${ROUTING_ENDPOINT}/diagnostics`
  if (
    !isRecord(value) ||
    !isRecord(value.lease_metrics) ||
    !Array.isArray(value.warnings) ||
    !isRecord(value.limits) ||
    !Array.isArray(value.recent_decisions)
  ) {
    invalidResponse(endpoint)
  }
  const diagnostics: RoutingDiagnostics = {
    lease_metrics: {
      route_lease_acquire_failure_total: requiredStrictInteger(
        value.lease_metrics.route_lease_acquire_failure_total,
        endpoint
      ),
      route_lease_renew_failure_total: requiredStrictInteger(
        value.lease_metrics.route_lease_renew_failure_total,
        endpoint
      ),
      route_lease_release_failure_total: requiredStrictInteger(
        value.lease_metrics.route_lease_release_failure_total,
        endpoint
      ),
    },
    decision_events_dropped: requiredStrictInteger(
      value.decision_events_dropped,
      endpoint
    ),
    capability_refresh_lag_seconds: requiredStrictInteger(
      value.capability_refresh_lag_seconds,
      endpoint
    ),
    score_metrics_available: requiredBoolean(
      value.score_metrics_available,
      endpoint
    ),
    degraded: requiredBoolean(value.degraded, endpoint),
    warnings: value.warnings.map((warning) => {
      const code = requiredString(
        warning,
        endpoint,
        false
      ) as RouteDiagnosticWarningCode
      if (!diagnosticWarningCodes.has(code)) invalidResponse(endpoint)
      return code
    }),
    limits: {
      candidate_top_k: requiredStrictInteger(
        value.limits.candidate_top_k,
        endpoint
      ),
      candidate_probe_limit: requiredStrictInteger(
        value.limits.candidate_probe_limit,
        endpoint
      ),
      total_send_attempts: requiredStrictInteger(
        value.limits.total_send_attempts,
        endpoint
      ),
      max_precommit_events: requiredStrictInteger(
        value.limits.max_precommit_events,
        endpoint
      ),
      max_precommit_bytes: requiredStrictInteger(
        value.limits.max_precommit_bytes,
        endpoint
      ),
    },
    recent_decisions: value.recent_decisions.map((decision) =>
      parseRouteDecision(decision, endpoint)
    ),
    generated_at: requiredStrictInteger(value.generated_at, endpoint),
  }
  if (
    Object.values(diagnostics.lease_metrics).some((count) => count < 0) ||
    diagnostics.decision_events_dropped < 0 ||
    diagnostics.capability_refresh_lag_seconds < 0 ||
    diagnostics.generated_at <= 0 ||
    Object.values(diagnostics.limits).some((limit) => limit <= 0)
  ) {
    invalidResponse(endpoint)
  }
  return diagnostics
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

function parseStringArray(value: unknown, endpoint: string): string[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => requiredString(item, endpoint, false))
}

export const routingApi = {
  async diagnostics(signal?: AbortSignal): Promise<RoutingDiagnostics> {
    const endpoint = `${ROUTING_ENDPOINT}/diagnostics`
    return parseRoutingDiagnostics(
      await api.get<unknown>(
        endpoint,
        undefined,
        signal ? { signal } : undefined
      )
    )
  },
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
    return value.map((channel) => parseEligibleRouteChannel(channel, endpoint))
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
    input: { model: string; path: string },
    signal?: AbortSignal
  ): Promise<RoutePreview> {
    return parseRoutePreview(
      await api.post<unknown>(
        `${ROUTING_ENDPOINT}/profiles/${id}/preview`,
        input,
        signal ? { signal } : undefined
      )
    )
  },
}
