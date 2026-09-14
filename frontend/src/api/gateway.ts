import { api } from './console'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import type {
  ConversionPolicy,
  GatewayAdmission,
  GatewayChannelRuntime,
  GatewayConfig,
  GatewayOverride,
  GatewayRateLimit,
  GatewayRateRuntime,
  GatewayRuntime,
  GatewaySource,
} from '@/types/gateway'

const endpoint = '/api/gateway'
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

function bounded(value: unknown, maximum: number, minimum = 0): number {
  const result = requiredStrictInteger(value, endpoint)
  if (result < minimum || result > maximum) invalidResponse(endpoint)
  return result
}

export function parseConversionPolicy(value: unknown): ConversionPolicy {
  if (value !== 'safe' && value !== 'allow' && value !== 'strict')
    invalidResponse('/api/token/')
  return value
}

export function parseGatewayOverride(value: unknown): GatewayOverride {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    concurrency:
      value.concurrency === null ? null : bounded(value.concurrency, 1000000),
    wait_ms: value.wait_ms === null ? null : bounded(value.wait_ms, 5000),
    queue_size:
      value.queue_size === null ? null : bounded(value.queue_size, 20),
  }
}

function parseRate(value: unknown): GatewayRateLimit {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    rpm: bounded(value.rpm, 1000000000),
    tpm: bounded(value.tpm, 1000000000),
  }
}

export function parseGatewayConfig(value: unknown): GatewayConfig {
  if (
    !isRecord(value) ||
    !isRecord(value.groups) ||
    !isRecord(value.channel_rates) ||
    !isRecord(value.credential_rates)
  )
    invalidResponse(endpoint)
  const groups = Object.fromEntries(
    Object.entries(value.groups).map(([key, item]) => {
      if (!key || key.trim() !== key || key.length > 64)
        invalidResponse(endpoint)
      return [key, parseGatewayOverride(item)]
    })
  )
  const channelRates = Object.fromEntries(
    Object.entries(value.channel_rates).map(([key, item]) => {
      if (!/^[1-9]\d*$/.test(key) || Number(key) > 2147483647)
        invalidResponse(endpoint)
      return [key, parseRate(item)]
    })
  )
  const credentialRates = Object.fromEntries(
    Object.entries(value.credential_rates).map(([key, item]) => {
      if (!uuidPattern.test(key)) invalidResponse(endpoint)
      return [key, parseRate(item)]
    })
  )
  if (
    [groups, channelRates, credentialRates].some(
      (map) => Object.keys(map).length > 10000
    )
  )
    invalidResponse(endpoint)
  return {
    version: bounded(value.version, Number.MAX_SAFE_INTEGER, 1),
    user_concurrency: bounded(value.user_concurrency, 1000000),
    wait_ms: bounded(value.wait_ms, 5000),
    user_queue_size: bounded(value.user_queue_size, 20),
    channel_queue_size: bounded(value.channel_queue_size, 10000),
    groups,
    channel_rates: channelRates,
    credential_rates: credentialRates,
  }
}

export function parseGatewayAdmission(value: unknown): GatewayAdmission {
  if (
    !isRecord(value) ||
    !isRecord(value.effective) ||
    !isRecord(value.sources)
  )
    invalidResponse(endpoint)
  const source = (key: string): GatewaySource => {
    if (!isRecord(value.sources)) invalidResponse(endpoint)
    const result = value.sources[key]
    if (result !== 'user' && result !== 'group' && result !== 'system')
      invalidResponse(endpoint)
    return result
  }
  return {
    user_id: bounded(value.user_id, 2147483647, 1),
    group: requiredString(value.group, endpoint, false),
    config_version: bounded(value.config_version, Number.MAX_SAFE_INTEGER, 1),
    override: parseGatewayOverride(value.override),
    effective: {
      concurrency: bounded(value.effective.concurrency, 1000000),
      wait_ms: bounded(value.effective.wait_ms, 5000),
      queue_size: bounded(value.effective.queue_size, 20),
      channel_queue_size: bounded(value.effective.channel_queue_size, 10000),
    },
    sources: {
      concurrency: source('concurrency'),
      wait_ms: source('wait_ms'),
      queue_size: source('queue_size'),
      channel_queue_size: source('channel_queue_size'),
    },
  }
}

export function parseGatewayRuntime(value: unknown): GatewayRuntime {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    in_flight: bounded(value.in_flight, Number.MAX_SAFE_INTEGER),
    queued: bounded(value.queued, Number.MAX_SAFE_INTEGER),
  }
}

export function parseGatewayRateRuntime(value: unknown): GatewayRateRuntime {
  if (!isRecord(value)) invalidResponse(endpoint)
  return {
    rpm_used: bounded(value.rpm_used, Number.MAX_SAFE_INTEGER),
    tokens_used: bounded(value.tokens_used, Number.MAX_SAFE_INTEGER),
    tokens_reserved: bounded(value.tokens_reserved, Number.MAX_SAFE_INTEGER),
    cooldown_ms: bounded(value.cooldown_ms, Number.MAX_SAFE_INTEGER),
  }
}

export const gatewayApi = {
  async config(signal?: AbortSignal) {
    return parseGatewayConfig(
      await api.get<unknown>(`${endpoint}/config`, undefined, { signal })
    )
  },
  async saveConfig(config: GatewayConfig, signal?: AbortSignal) {
    return parseGatewayConfig(
      await api.put<unknown>(`${endpoint}/config`, parseGatewayConfig(config), {
        signal,
      })
    )
  },
  async admission(userId?: number, signal?: AbortSignal) {
    return parseGatewayAdmission(
      await api.get<unknown>(
        userId ? `${endpoint}/users/${userId}` : `${endpoint}/self`,
        undefined,
        { signal }
      )
    )
  },
  async saveUser(
    userId: number,
    override: GatewayOverride,
    signal?: AbortSignal
  ) {
    return parseGatewayOverride(
      await api.put<unknown>(
        `${endpoint}/users/${userId}`,
        parseGatewayOverride(override),
        { signal }
      )
    )
  },
  async userRuntime(userId?: number, signal?: AbortSignal) {
    return parseGatewayRuntime(
      await api.get<unknown>(
        userId
          ? `${endpoint}/runtime/users/${userId}`
          : `${endpoint}/self/runtime`,
        undefined,
        { signal }
      )
    )
  },
  async channelRuntime(
    channelId: number,
    signal?: AbortSignal
  ): Promise<GatewayChannelRuntime> {
    const value = await api.get<unknown>(
      `${endpoint}/runtime/channels/${channelId}`,
      undefined,
      { signal }
    )
    if (!isRecord(value)) invalidResponse(endpoint)
    return {
      ...parseGatewayRuntime(value),
      channel_id: bounded(value.channel_id, 2147483647, 1),
      capacity: bounded(value.capacity, 1000000),
      rate: parseGatewayRateRuntime(value.rate),
    }
  },
  async credentials(
    channelId: number,
    signal?: AbortSignal
  ): Promise<string[]> {
    const value = await api.get<unknown>(
      `${endpoint}/channels/${channelId}/credentials`,
      undefined,
      { signal }
    )
    if (!Array.isArray(value)) invalidResponse(endpoint)
    return value.map((item) => {
      if (
        !isRecord(item) ||
        typeof item.id !== 'string' ||
        !uuidPattern.test(item.id)
      )
        invalidResponse(endpoint)
      return item.id
    })
  },
  async credentialRuntime(id: string, signal?: AbortSignal) {
    return parseGatewayRateRuntime(
      await api.get<unknown>(
        `${endpoint}/runtime/credentials/${encodeURIComponent(id)}`,
        undefined,
        { signal }
      )
    )
  },
}
