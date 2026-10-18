import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
export interface RoutingObservation {
  channel_id: number
  model: string
  stream: boolean
  samples: number
  error_rate: number
  latency_ms: number
  known: boolean
  updated_at: number
}
export interface RoutingSnapshot {
  users: { id: number; limit: number; in_flight: number }[]
  channels: { id: number; limit: number; in_flight: number }[]
  observations: RoutingObservation[]
}
export interface PerformanceStats {
  memory_stats: {
    alloc: number
    total_alloc: number
    sys: number
    num_gc: number
    num_goroutine: number
  }
  cache_stats: Record<string, unknown>
  disk_cache_info: Record<string, unknown>
  disk_space_info: Record<string, unknown>
  config: Record<string, unknown>
}
export function parseRoutingSnapshot(value: unknown): RoutingSnapshot {
  const endpoint = '/api/performance/routing'
  if (!isRecord(value)) invalidResponse(endpoint)
  const rows = (key: string) => {
    const result = value[key]
    if (!Array.isArray(result) || result.some((row) => !isRecord(row)))
      invalidResponse(endpoint)
    return result as Record<string, unknown>[]
  }
  const limits = (key: string) =>
    rows(key).map((row) => ({
      id: requiredStrictInteger(row.id, endpoint),
      limit: requiredStrictInteger(row.limit, endpoint),
      in_flight: requiredStrictInteger(row.in_flight, endpoint),
    }))
  return {
    users: limits('users'),
    channels: limits('channels'),
    observations: rows('observations').map((row) => ({
      channel_id: requiredStrictInteger(row.channel_id, endpoint),
      model: requiredString(row.model, endpoint),
      stream: requiredBoolean(row.stream, endpoint),
      samples: requiredStrictInteger(row.samples, endpoint),
      error_rate: requiredStrictNumber(row.error_rate, endpoint),
      latency_ms: requiredStrictNumber(row.latency_ms, endpoint),
      known: requiredBoolean(row.known, endpoint),
      updated_at: requiredStrictInteger(row.updated_at, endpoint),
    })),
  }
}
export function parsePerformanceStats(value: unknown): PerformanceStats {
  const endpoint = '/api/performance/stats'
  if (
    !isRecord(value) ||
    !isRecord(value.memory_stats) ||
    !isRecord(value.cache_stats) ||
    !isRecord(value.disk_cache_info) ||
    !isRecord(value.disk_space_info) ||
    !isRecord(value.config)
  )
    invalidResponse(endpoint)
  const memory = value.memory_stats
  return {
    memory_stats: {
      alloc: requiredStrictNumber(memory.alloc, endpoint),
      total_alloc: requiredStrictNumber(memory.total_alloc, endpoint),
      sys: requiredStrictNumber(memory.sys, endpoint),
      num_gc: requiredStrictInteger(memory.num_gc, endpoint),
      num_goroutine: requiredStrictInteger(memory.num_goroutine, endpoint),
    },
    cache_stats: value.cache_stats,
    disk_cache_info: value.disk_cache_info,
    disk_space_info: value.disk_space_info,
    config: value.config,
  }
}
export type PerformanceAction = 'gc' | 'disk_cache' | 'reset_stats'
export const opsMonitorApi = {
  execute: (action: PerformanceAction) =>
    action === 'disk_cache'
      ? api.delete('/api/performance/disk_cache')
      : api.post(`/api/performance/${action}`),
  stats: async (signal?: AbortSignal) =>
    parsePerformanceStats(
      await api.get('/api/performance/stats', undefined, { signal })
    ),
  routing: async (signal?: AbortSignal) =>
    parseRoutingSnapshot(
      await api.get('/api/performance/routing', undefined, { signal })
    ),
}
