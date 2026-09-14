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
  rate: GatewayRateRuntime
}
