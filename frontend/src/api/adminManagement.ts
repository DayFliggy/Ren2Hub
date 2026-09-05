import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parsePage,
  parseStringArray,
  requiredBoolean,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'

export interface ModelMetadata {
  id: number
  model_name: string
  description: string
  icon: string
  tags: string
  vendor_id: number
  endpoints: string
  status: number
  sync_official: number
  name_rule: number
  bound_channels: { name: string; type: number }[]
  enable_groups: string[]
  matched_models: string[]
}
export interface ModelVendor {
  id: number
  name: string
  description: string
  icon: string
  status: number
}
export interface PrefillGroup {
  id: number
  name: string
  type: 'model' | 'tag' | 'endpoint'
  items: string[]
  description: string
}
export type ModelInput = Omit<
  ModelMetadata,
  'id' | 'bound_channels' | 'enable_groups' | 'matched_models'
> & { id?: number }
export type VendorInput = Omit<ModelVendor, 'id'> & { id?: number }
export type PrefillInput = Omit<PrefillGroup, 'id'> & { id?: number }

function record(value: unknown, endpoint: string): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}
function array<T>(
  value: unknown,
  endpoint: string,
  parse: (item: unknown, endpoint: string) => T
): T[] {
  if (!Array.isArray(value)) invalidResponse(endpoint)
  return value.map((item) => parse(item, endpoint))
}
function optionalText(value: unknown, endpoint: string): string {
  return value === undefined || value === null
    ? ''
    : requiredString(value, endpoint)
}
function flag(value: unknown, endpoint: string): number {
  const result = requiredStrictInteger(value, endpoint)
  if (result !== 0 && result !== 1) invalidResponse(endpoint)
  return result
}

export function parseModelMetadata(
  value: unknown,
  endpoint = '/api/models/'
): ModelMetadata {
  const row = record(value, endpoint)
  const nameRule = requiredStrictInteger(row.name_rule, endpoint)
  if (nameRule < 0 || nameRule > 3) invalidResponse(endpoint)
  return {
    id: requiredStrictInteger(row.id, endpoint),
    model_name: requiredString(row.model_name, endpoint, false),
    description: optionalText(row.description, endpoint),
    icon: optionalText(row.icon, endpoint),
    tags: optionalText(row.tags, endpoint),
    vendor_id:
      row.vendor_id == null
        ? 0
        : requiredStrictInteger(row.vendor_id, endpoint),
    endpoints: optionalText(row.endpoints, endpoint),
    status: flag(row.status, endpoint),
    sync_official: flag(row.sync_official, endpoint),
    name_rule: nameRule,
    bound_channels: array(row.bound_channels ?? [], endpoint, (item) => {
      const channel = record(item, endpoint)
      return {
        name: requiredString(channel.name, endpoint),
        type: requiredStrictInteger(channel.type, endpoint),
      }
    }),
    enable_groups: parseStringArray(row.enable_groups ?? [], endpoint),
    matched_models: parseStringArray(row.matched_models ?? [], endpoint),
  }
}
export function parseModelVendor(
  value: unknown,
  endpoint = '/api/vendors/'
): ModelVendor {
  const row = record(value, endpoint)
  return {
    id: requiredStrictInteger(row.id, endpoint),
    name: requiredString(row.name, endpoint, false),
    description: optionalText(row.description, endpoint),
    icon: optionalText(row.icon, endpoint),
    status: flag(row.status, endpoint),
  }
}
export function parsePrefillGroup(
  value: unknown,
  endpoint = '/api/prefill_group/'
): PrefillGroup {
  const row = record(value, endpoint)
  if (row.type !== 'model' && row.type !== 'tag' && row.type !== 'endpoint')
    invalidResponse(endpoint)
  let items = row.items
  if (typeof items === 'string') {
    try {
      items = JSON.parse(items)
    } catch {
      invalidResponse(endpoint)
    }
  }
  return {
    id: requiredStrictInteger(row.id, endpoint),
    name: requiredString(row.name, endpoint, false),
    type: row.type,
    items: parseStringArray(items, endpoint),
    description: optionalText(row.description, endpoint),
  }
}

export interface SyncPreview {
  missing: string[]
  conflicts: {
    model_name: string
    fields: {
      field: string
      local: string | number
      upstream: string | number
    }[]
  }[]
}
export function parseSyncPreview(
  value: unknown,
  endpoint = '/api/models/sync_upstream/preview'
): SyncPreview {
  const data = record(value, endpoint)
  return {
    missing: parseStringArray(data.missing ?? [], endpoint),
    conflicts: array(data.conflicts ?? [], endpoint, (item) => {
      const conflict = record(item, endpoint)
      return {
        model_name: requiredString(conflict.model_name, endpoint, false),
        fields: array(conflict.fields, endpoint, (item) => {
          const field = record(item, endpoint)
          if (
            (typeof field.local !== 'string' &&
              typeof field.local !== 'number') ||
            (typeof field.upstream !== 'string' &&
              typeof field.upstream !== 'number')
          )
            invalidResponse(endpoint)
          return {
            field: requiredString(field.field, endpoint, false),
            local: field.local,
            upstream: field.upstream,
          }
        }),
      }
    }),
  }
}
export interface SyncResult {
  created_models: number
  updated_models: number
  created_vendors: number
  skipped_models: string[]
}
export const metadataApi = {
  async models(params: Record<string, unknown>, signal?: AbortSignal) {
    const endpoint = '/api/models/search'
    return parsePage(
      await api.get(endpoint, params, { signal }),
      endpoint,
      parseModelMetadata
    )
  },
  async vendors(params: Record<string, unknown> = {}, signal?: AbortSignal) {
    const endpoint = '/api/vendors/search'
    return parsePage(
      await api.get(endpoint, params, { signal }),
      endpoint,
      parseModelVendor
    )
  },
  async allVendors(signal?: AbortSignal) {
    const result: ModelVendor[] = []
    for (let p = 1; ; p++) {
      const page = await metadataApi.vendors({ p, page_size: 100 }, signal)
      result.push(...page.items)
      if (result.length >= page.total) return result
      if (!page.items.length) invalidResponse('/api/vendors/search')
    }
  },
  async groups(signal?: AbortSignal) {
    const endpoint = '/api/prefill_group/'
    return array(
      await api.get(endpoint, undefined, { signal }),
      endpoint,
      parsePrefillGroup
    )
  },
  async missing(signal?: AbortSignal) {
    return parseStringArray(
      (await api.get('/api/models/missing', undefined, { signal })) ?? [],
      '/api/models/missing'
    )
  },
  saveModel: (input: ModelInput) =>
    input.id ? api.put('/api/models/', input) : api.post('/api/models/', input),
  saveVendor: (input: VendorInput) =>
    input.id
      ? api.put('/api/vendors/', input)
      : api.post('/api/vendors/', input),
  saveGroup: (input: PrefillInput) =>
    input.id
      ? api.put('/api/prefill_group/', input)
      : api.post('/api/prefill_group/', input),
  deleteModel: (id: number) => api.delete(`/api/models/${id}`),
  deleteVendor: (id: number) => api.delete(`/api/vendors/${id}`),
  deleteGroup: (id: number) => api.delete(`/api/prefill_group/${id}`),
  setModelStatus: (id: number, status: number) =>
    api.put('/api/models/?status_only=true', { id, status }),
  async preview(source: string, locale: string, signal?: AbortSignal) {
    return parseSyncPreview(
      await api.get(
        '/api/models/sync_upstream/preview',
        { source, locale },
        { signal }
      )
    )
  },
  async sync(
    source: string,
    locale: string,
    overwrite: { model_name: string; fields: string[] }[]
  ): Promise<SyncResult> {
    const endpoint = '/api/models/sync_upstream'
    const data = record(
      await api.post(endpoint, { source, locale, overwrite }),
      endpoint
    )
    return {
      created_models: requiredStrictInteger(data.created_models, endpoint),
      updated_models: requiredStrictInteger(data.updated_models, endpoint),
      created_vendors: requiredStrictInteger(data.created_vendors, endpoint),
      skipped_models: parseStringArray(data.skipped_models ?? [], endpoint),
    }
  },
}

export interface Deployment {
  id: string
  deployment_name: string
  status: string
  hardware_name: string
  hardware_quantity: number
  compute_minutes_remaining: number
  completed_percent: number
}
export interface DeploymentConfig {
  image_url: string
  traffic_port: number
  entrypoint: string[]
  env_variables: Record<string, string>
}
export interface DeploymentDetail extends Deployment {
  amount_paid: number
  brand_name: string
  total_containers: number
  total_gpus: number
  locations: { id: number; name: string }[]
  container_config: DeploymentConfig
}
export interface DeploymentContainer {
  container_id: string
  device_id: string
  status: string
  hardware: string
  uptime_percent: number
  gpus_per_container: number
  public_url: string
  events: { time: number; message: string }[]
}
export interface Hardware {
  id: number
  name: string
  max_gpus: number
  available: boolean
  hourly_rate: number
}
export interface DeploymentLocation {
  id: number
  name: string
}
export interface Replica {
  location_id: number
  location_name: string
  available_count: number
}
export interface DeploymentPrice {
  estimated_cost: number
  currency: string
  estimation_valid: boolean
  hourly_rate: number
}
export interface DeploymentQuoteInput {
  hardware_id: number
  location_ids: number[]
  gpus_per_container: number
  duration_hours: number
  replica_count: number
  currency: string
}
export interface DeploymentCreateInput extends Omit<
  DeploymentQuoteInput,
  'replica_count' | 'currency'
> {
  resource_private_name: string
  container_config: Omit<DeploymentConfig, 'image_url'> & {
    replica_count: number
    args: string[]
    secret_env_variables?: Record<string, string>
  }
  registry_config: {
    image_url: string
    registry_username?: string
    registry_secret?: string
  }
}
export function parseDeployment(
  value: unknown,
  endpoint = '/api/deployments/'
): Deployment {
  const row = record(value, endpoint)
  return {
    id: requiredString(row.id, endpoint, false),
    deployment_name: requiredString(row.deployment_name, endpoint),
    status: requiredString(row.status, endpoint, false),
    hardware_name: requiredString(row.hardware_name, endpoint),
    hardware_quantity: requiredStrictInteger(
      row.hardware_quantity ?? row.total_gpus,
      endpoint
    ),
    compute_minutes_remaining: requiredStrictInteger(
      row.compute_minutes_remaining,
      endpoint
    ),
    completed_percent: requiredStrictNumber(row.completed_percent, endpoint),
  }
}
export function parseDeploymentContainer(
  value: unknown,
  endpoint = '/api/deployments/:id/containers'
): DeploymentContainer {
  const row = record(value, endpoint)
  return {
    container_id: requiredString(row.container_id, endpoint, false),
    device_id: requiredString(row.device_id, endpoint),
    status: requiredString(row.status, endpoint),
    hardware: requiredString(row.hardware, endpoint),
    uptime_percent: requiredStrictNumber(row.uptime_percent, endpoint),
    gpus_per_container: requiredStrictInteger(row.gpus_per_container, endpoint),
    public_url: requiredString(row.public_url, endpoint),
    events: array(row.events ?? [], endpoint, (item) => {
      const event = record(item, endpoint)
      return {
        time: requiredStrictNumber(event.time, endpoint),
        message: requiredString(event.message, endpoint),
      }
    }),
  }
}
export function parseDeploymentPrice(
  value: unknown,
  endpoint = '/api/deployments/price-estimation'
): DeploymentPrice {
  const data = record(value, endpoint)
  const breakdown = record(data.price_breakdown, endpoint)
  const price = {
    estimated_cost: requiredStrictNumber(data.estimated_cost, endpoint),
    currency: requiredString(data.currency, endpoint, false),
    estimation_valid: requiredBoolean(data.estimation_valid, endpoint),
    hourly_rate: requiredStrictNumber(breakdown.hourly_rate, endpoint),
  }
  if (price.estimated_cost < 0 || price.hourly_rate < 0)
    invalidResponse(endpoint)
  return price
}

const deploymentPath = (id: string) =>
  `/api/deployments/${encodeURIComponent(id)}`
export const deploymentsApi = {
  async settings(signal?: AbortSignal) {
    const endpoint = '/api/deployments/settings'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    return {
      enabled: requiredBoolean(data.enabled, endpoint),
      configured: requiredBoolean(data.configured, endpoint),
      can_connect: requiredBoolean(data.can_connect, endpoint),
    }
  },
  async list(params: Record<string, unknown>, signal?: AbortSignal) {
    const endpoint = '/api/deployments/search'
    return parsePage(
      await api.get(endpoint, params, { signal }),
      endpoint,
      parseDeployment
    )
  },
  async detail(id: string, signal?: AbortSignal): Promise<DeploymentDetail> {
    const endpoint = deploymentPath(id)
    const row = record(await api.get(endpoint, undefined, { signal }), endpoint)
    const config = record(row.container_config, endpoint)
    const env = record(config.env_variables ?? {}, endpoint)
    const variables: Record<string, string> = {}
    for (const [key, value] of Object.entries(env))
      variables[key] = requiredString(value, endpoint)
    return {
      ...parseDeployment(row, endpoint),
      amount_paid: requiredStrictNumber(row.amount_paid, endpoint),
      brand_name: requiredString(row.brand_name, endpoint),
      total_containers: requiredStrictInteger(row.total_containers, endpoint),
      total_gpus: requiredStrictInteger(row.total_gpus, endpoint),
      locations: array(row.locations ?? [], endpoint, (item) => {
        const location = record(item, endpoint)
        return {
          id: requiredStrictInteger(location.id, endpoint),
          name: requiredString(location.name, endpoint),
        }
      }),
      container_config: {
        image_url: requiredString(config.image_url, endpoint),
        traffic_port: requiredStrictInteger(config.traffic_port, endpoint),
        entrypoint: parseStringArray(config.entrypoint ?? [], endpoint),
        env_variables: variables,
      },
    }
  },
  async hardware(signal?: AbortSignal) {
    const endpoint = '/api/deployments/hardware-types'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    return array(data.hardware_types, endpoint, (item) => {
      const hardware = record(item, endpoint)
      return {
        id: requiredStrictInteger(hardware.id, endpoint),
        name: requiredString(hardware.name, endpoint),
        max_gpus: requiredStrictInteger(hardware.max_gpus, endpoint),
        available: requiredBoolean(hardware.available, endpoint),
        hourly_rate: requiredStrictNumber(hardware.hourly_rate, endpoint),
      }
    })
  },
  async locations(signal?: AbortSignal) {
    const endpoint = '/api/deployments/locations'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    return array(data.locations, endpoint, (item) => {
      const location = record(item, endpoint)
      return {
        id: requiredStrictInteger(location.id, endpoint),
        name: requiredString(location.name, endpoint),
      }
    })
  },
  async replicas(
    hardware_id: number,
    gpu_count: number,
    signal?: AbortSignal
  ): Promise<Replica[]> {
    const endpoint = '/api/deployments/available-replicas'
    const data = record(
      await api.get(endpoint, { hardware_id, gpu_count }, { signal }),
      endpoint
    )
    return array(data.replicas ?? [], endpoint, (item) => {
      const replica = record(item, endpoint)
      return {
        location_id: requiredStrictInteger(replica.location_id, endpoint),
        location_name: requiredString(replica.location_name, endpoint),
        available_count: requiredStrictInteger(
          replica.available_count,
          endpoint
        ),
      }
    })
  },
  async quote(input: DeploymentQuoteInput, signal?: AbortSignal) {
    return parseDeploymentPrice(
      await api.post(
        '/api/deployments/price-estimation',
        {
          ...input,
          duration_type: 'hour',
          duration_qty: input.duration_hours,
          hardware_qty: input.gpus_per_container,
        },
        { signal }
      )
    )
  },
  async checkName(name: string, signal?: AbortSignal) {
    const endpoint = '/api/deployments/check-name'
    const data = record(await api.get(endpoint, { name }, { signal }), endpoint)
    return requiredBoolean(data.available, endpoint)
  },
  async containers(id: string, signal?: AbortSignal) {
    const endpoint = `${deploymentPath(id)}/containers`
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    return array(data.containers, endpoint, parseDeploymentContainer)
  },
  async container(id: string, containerId: string, signal?: AbortSignal) {
    const endpoint = `${deploymentPath(id)}/containers/${encodeURIComponent(containerId)}`
    return parseDeploymentContainer(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
  },
  async logs(
    id: string,
    params: Record<string, unknown>,
    signal?: AbortSignal
  ) {
    const endpoint = `${deploymentPath(id)}/logs`
    return requiredString(await api.get(endpoint, params, { signal }), endpoint)
  },
  create: (input: DeploymentCreateInput) =>
    api.post('/api/deployments/', input),
  update: (
    id: string,
    input: Partial<DeploymentConfig> & {
      args?: string[]
      registry_username?: string
      registry_secret?: string
      secret_env_variables?: Record<string, string>
    }
  ) => api.put(deploymentPath(id), input),
  rename: (id: string, name: string) =>
    api.put(`${deploymentPath(id)}/name`, { name }),
  extend: (id: string, duration_hours: number) =>
    api.post(`${deploymentPath(id)}/extend`, { duration_hours }),
  delete: (id: string) => api.delete(deploymentPath(id)),
}

export interface SystemInstance {
  node_name: string
  status: 'online' | 'stale'
  last_seen_at: number
  started_at: number
  version: string
  hostname: string
  master: boolean
  cpu: number | null
  memory: number | null
  storage: number | null
}
export interface SystemTask {
  task_id: string
  type: string
  status: string
  created_at: number
  updated_at: number
  error: string
  locked_by: string
  progress: number | null
  processed: number | null
  total: number | null
  deleted_count: number | null
}
export function parseSystemTask(
  value: unknown,
  endpoint = '/api/system-task/list'
): SystemTask {
  const row = record(value, endpoint)
  const state = row.state == null ? {} : record(row.state, endpoint)
  const result = row.result == null ? {} : record(row.result, endpoint)
  const status = requiredString(row.status, endpoint, false)
  if (!['pending', 'running', 'succeeded', 'failed'].includes(status))
    invalidResponse(endpoint)
  return {
    task_id: requiredString(row.task_id, endpoint, false),
    type: requiredString(row.type, endpoint, false),
    status,
    created_at: requiredStrictInteger(row.created_at, endpoint),
    updated_at: requiredStrictInteger(row.updated_at, endpoint),
    error: optionalText(row.error, endpoint),
    locked_by: optionalText(row.locked_by, endpoint),
    progress:
      state.progress == null
        ? null
        : requiredStrictNumber(state.progress, endpoint),
    processed:
      state.processed == null
        ? null
        : requiredStrictInteger(state.processed, endpoint),
    total:
      state.total == null ? null : requiredStrictInteger(state.total, endpoint),
    deleted_count:
      result.deleted_count == null
        ? null
        : requiredStrictInteger(result.deleted_count, endpoint),
  }
}
export const systemManagementApi = {
  async instances(signal?: AbortSignal): Promise<SystemInstance[]> {
    const endpoint = '/api/system-info/instances'
    return array(
      await api.get(endpoint, undefined, { signal }),
      endpoint,
      (item) => {
        const row = record(item, endpoint)
        if (row.status !== 'online' && row.status !== 'stale')
          invalidResponse(endpoint)
        const info = record(row.info ?? {}, endpoint)
        const runtime = record(info.runtime ?? {}, endpoint)
        const host = record(info.host ?? {}, endpoint)
        const role = record(info.role ?? {}, endpoint)
        const resources = record(info.resources ?? {}, endpoint)
        const cpu = record(resources.cpu ?? {}, endpoint)
        const memory = record(resources.memory ?? {}, endpoint)
        const storage = record(resources.storage ?? {}, endpoint)
        return {
          node_name: requiredString(row.node_name, endpoint, false),
          status: row.status,
          last_seen_at: requiredStrictInteger(row.last_seen_at, endpoint),
          started_at: requiredStrictInteger(row.started_at, endpoint),
          version: optionalText(runtime.version, endpoint),
          hostname: optionalText(host.hostname, endpoint),
          master:
            role.is_master === undefined
              ? false
              : requiredBoolean(role.is_master, endpoint),
          cpu:
            cpu.usage_percent == null
              ? null
              : requiredStrictNumber(cpu.usage_percent, endpoint),
          memory:
            memory.usage_percent == null
              ? null
              : requiredStrictNumber(memory.usage_percent, endpoint),
          storage:
            storage.used_percent == null
              ? null
              : requiredStrictNumber(storage.used_percent, endpoint),
        }
      }
    )
  },
  async tasks(limit: number, signal?: AbortSignal) {
    return array(
      (await api.get('/api/system-task/list', { limit }, { signal })) ?? [],
      '/api/system-task/list',
      parseSystemTask
    )
  },
  async task(taskId: string, signal?: AbortSignal) {
    return parseSystemTask(
      await api.get(
        `/api/system-task/${encodeURIComponent(taskId)}`,
        undefined,
        { signal }
      )
    )
  },
  deleteStale: (name?: string) =>
    api.delete(
      name
        ? `/api/system-info/instances/${encodeURIComponent(name)}`
        : '/api/system-info/stale-instances'
    ),
  cleanup: (target: number) =>
    api.post(`/api/system-task/log-cleanup?target_timestamp=${target}`),
}
