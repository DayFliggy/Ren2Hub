export type RouteMode = 'legacy' | 'manual' | 'auto_lab'
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
