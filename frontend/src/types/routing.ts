export type RouteMode = 'manual' | 'auto_lab'
export type RouteGroupKind = 'manual' | 'auto_lab'
export type RouteEntrySource = 'platform'
export type RouteRetryMode =
  'none' | 'same_channel' | 'next_channel' | 'same_then_next'
export type RouteCapabilityState =
  'eligible' | 'unresolved' | 'unsupported' | 'disabled' | 'conflict'
export type RouteHealthState = 'closed' | 'open' | 'half_open'

export interface RoutePolicy {
  group_id: number
  load_balance: boolean
  max_ratio: number
  retry_mode: RouteRetryMode
  max_same_resource_attempts: number
  max_failover_attempts: number
  sticky: boolean
}

export interface RouteEntry {
  id: number
  group_id: number
  channel_id: number
  source: RouteEntrySource
  enabled: boolean
  position: number
  weight: number
}

export interface RouteGroup {
  id: number
  profile_id: number
  name: string
  kind: RouteGroupKind
  enabled: boolean
  position: number
  entries: RouteEntry[]
  policy: RoutePolicy
}

export interface RouteProfile {
  id: number
  user_id: number
  token_id: number
  mode: RouteMode
  active_group_id: number | null
  version: number
  status: number
  created_at: number
  updated_at: number
}

export interface RouteProfileView {
  profile: RouteProfile
  groups: RouteGroup[]
}

export interface EligibleRouteChannel {
  id: number
  name: string
  type: number
  status: number
  request_models: string[]
  priority: number
  weight: number
  channel_rpm: number
  channel_tpm: number
  snapshot_version: number
  catalog_version: string
  capability_state: RouteCapabilityState
  filter_reason?: string
}

export interface RouteCatalogItem {
  id: number
  channel_id: number
  request_model: string
  actual_model: string
  lab_slug: string
  confidence: number
  source: string
  catalog_version: string
  snapshot_version: number
  state: RouteCapabilityState
}

export interface RouteCatalog {
  catalog_version: string
  catalog_versions: string[]
  items: RouteCatalogItem[]
}

export interface RouteLeaseMetrics {
  route_lease_acquire_failure_total: number
  route_lease_renew_failure_total: number
  route_lease_release_failure_total: number
}

export type RouteDiagnosticWarningCode =
  | 'route_lease_failures'
  | 'route_decision_events_dropped'
  | 'capability_refresh_lagging'
  | 'route_score_metrics_unavailable'

export interface RouteDiagnosticLimits {
  candidate_top_k: number
  candidate_probe_limit: number
  total_send_attempts: number
  max_precommit_events: number
  max_precommit_bytes: number
}

export interface RouteScoreBreakdown {
  priority_layer: number
  weight_score: number
  error_score: number
  error_known: boolean
  latency_score: number
  latency_known: boolean
  ttft_score: number
  ttft_known: boolean
  rate_limit_score: number
  rate_limit_known: boolean
  quota_score: number
  quota_known: boolean
  circuit_breaker_score: number
  sticky_score: number
  total: number
}

export interface RouteDecisionCandidate {
  channel_id: number
  request_model?: string
  actual_model?: string
  filter_reason?: string
  priority: number
  position: number
  weight: number
  snapshot_version?: number
  health_epoch?: number
  catalog_version?: string
  lease_state?: string
  score_breakdown?: RouteScoreBreakdown
}

export interface RouteDecision {
  event: 'route_decision'
  request_id: string
  route_source: 'unavailable' | RouteMode
  configuration_version?: number
  request_model: string
  actual_model?: string
  lab_slug?: string
  catalog_version?: string
  snapshot_version?: number
  scoring_mode?: 'off' | 'live'
  dynamic_score_applied: boolean
  score_metrics_unavailable?: boolean
  static_preferred_channel_id?: number
  scored_preferred_channel_id?: number
  candidates: RouteDecisionCandidate[]
  selected_channel_id?: number
  retry_attempt: number
  same_resource_retry_attempt: number
  failover_attempt: number
  lease_state?: string
  final_error?: string
  generated_at: number
}

export interface RoutingDiagnostics {
  lease_metrics: RouteLeaseMetrics
  decision_events_dropped: number
  capability_refresh_lag_seconds: number
  score_metrics_available: boolean
  degraded: boolean
  warnings: RouteDiagnosticWarningCode[]
  limits: RouteDiagnosticLimits
  recent_decisions: RouteDecision[]
  generated_at: number
}

export interface RoutePreviewEntry {
  entry_id: number
  channel_id: number
  position: number
  weight: number
  request_model: string
  actual_model: string
  lab_slug: string
  snapshot_version: number
  catalog_version: string
  capability_state: RouteCapabilityState
  health: RouteHealthSummary
  filter_reason?: string
}

export interface RouteHealthSummary {
  state: RouteHealthState
  failure_count: number
  cooldown_until: number
  health_epoch: number
  last_latency_ms: number
  first_token_latency_ms: number
  updated_at: number
}

export interface RoutePreview {
  profile_id: number
  profile_version: number
  request_model: string
  normalized_model: string
  path: string
  endpoint_type: string
  active_group?: RouteGroup
  policy?: RoutePolicy
  entries: RoutePreviewEntry[]
  candidate_channel_ids: number[]
  selection_mode: 'ordered' | 'weighted'
  preferred_channel_id?: number
  filter_reason_counts: Record<string, number>
  has_mixed: boolean
  runtime_recheck_required: boolean
  runtime_recheck_reasons: string[]
  live_selection: false
}

export interface RouteProfileInput {
  token_id?: number
  mode: RouteMode
  active_group_id?: number | null
  version?: number
  groups: Array<{
    id?: number
    name: string
    kind?: RouteGroupKind
    enabled: boolean
    position: number
    entries: Array<{
      id?: number
      channel_id: number
      source: RouteEntrySource
      enabled: boolean
      position: number
      weight: number
    }>
    policy: Omit<RoutePolicy, 'group_id'>
  }>
}
