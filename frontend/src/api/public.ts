import { createApiClient } from './createClient'
import { httpTransport } from './httpTransport'

export interface PublicStatus {
  version?: string
  system_name?: string
  logo?: string
  docs_link?: string
  register_enabled?: boolean
  user_agreement_enabled?: boolean
  privacy_policy_enabled?: boolean
  uptime_kuma_enabled?: boolean
  HeaderNavModules?: unknown
}

export interface PricingModel {
  id: number
}

export interface UptimeMonitor {
  uptime: number
  status: number
}

export interface UptimeGroup {
  monitors: UptimeMonitor[]
}

const publicClient = createApiClient(httpTransport)

export const publicApi = {
  status: (signal?: AbortSignal) =>
    publicClient.get<PublicStatus>('/api/status', undefined, { signal }),
  notice: (signal?: AbortSignal) =>
    publicClient.get<string>('/api/notice', undefined, { signal }),
  pricing: (signal?: AbortSignal) =>
    publicClient.get<PricingModel[]>('/api/pricing', undefined, { signal }),
  uptime: (signal?: AbortSignal) =>
    publicClient.get<UptimeGroup[]>('/api/uptime/status', undefined, {
      signal,
    }),
}
