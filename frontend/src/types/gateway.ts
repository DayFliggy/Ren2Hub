export type ConversionPolicy = 'safe' | 'allow' | 'strict'
export type GatewaySource = 'user' | 'group' | 'system'
export interface GatewayOverride {
  concurrency: number | null
  wait_ms: number | null
  queue_size: number | null
}
export interface GatewayRateLimit {
  rpm: number
  tpm: number
}
export interface GatewayConfig {
  total_queue_size: number
  retry_budget_enabled: boolean
  retry_budget_percent: number
  retry_budget_burst: number
  version: number
  user_concurrency: number
  wait_ms: number
  user_queue_size: number
  channel_queue_size: number
  groups: Record<string, GatewayOverride>
  channel_rates: Record<string, GatewayRateLimit>
  credential_rates: Record<string, GatewayRateLimit>
}
export interface GatewayAdmission {
  user_id: number
  group: string
  config_version: number
  override: GatewayOverride
  effective: {
    concurrency: number
    wait_ms: number
    queue_size: number
    channel_queue_size: number
  }
  sources: Record<keyof GatewayAdmission['effective'], GatewaySource>
}
export interface GatewayRuntime {
  capacity: number
  config_version: number
  revision: number
  started_at: number
  draining: boolean
  in_flight: number
  queued: number
}
export interface GatewayRateRuntime {
  rpm_used: number
  tokens_used: number
  tokens_reserved: number
  cooldown_ms: number
}
export interface GatewayChannelRuntime extends GatewayRuntime {
  channel_id: number
  capacity: number
  rate: GatewayRateRuntime | null
  rate_status: 'available' | 'unavailable'
}
