import { api } from './client'
import { parseModelEndpoints } from '@/utils/modelEndpoints'
import {
  invalidResponse,
  isRecord,
  parsePage,
  parseStringArray,
  requiredStrictInteger,
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
  matched_count: number
  quota_types: number[]
  created_time: number | null
  updated_time: number | null
}
export interface ModelVendor {
  id: number
  name: string
  description: string
  icon: string
  status: number
}
interface PrefillBase {
  id: number
  name: string
  description: string
}
export type PrefillGroup = PrefillBase &
  (
    | { type: 'model' | 'tag'; items: string[] }
    | { type: 'endpoint'; items: string }
  )
export type ModelInput = Omit<
  ModelMetadata,
  | 'id'
  | 'bound_channels'
  | 'enable_groups'
  | 'matched_models'
  | 'matched_count'
  | 'quota_types'
  | 'created_time'
  | 'updated_time'
> & { id?: number }
export type VendorInput = Omit<ModelVendor, 'id'> & { id?: number }
export type PrefillInput = Omit<PrefillBase, 'id'> & { id?: number } & (
    | { type: 'model' | 'tag'; items: string[] }
    | { type: 'endpoint'; items: string }
  )

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
    matched_count:
      row.matched_count == null
        ? 0
        : requiredStrictInteger(row.matched_count, endpoint),
    quota_types: array(row.quota_types ?? [], endpoint, (item) => {
      const quota = requiredStrictInteger(item, endpoint)
      if (quota !== 0 && quota !== 1) invalidResponse(endpoint)
      return quota
    }),
    created_time:
      row.created_time == null
        ? null
        : requiredStrictInteger(row.created_time, endpoint),
    updated_time:
      row.updated_time == null
        ? null
        : requiredStrictInteger(row.updated_time, endpoint),
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
  const base = {
    id: requiredStrictInteger(row.id, endpoint),
    name: requiredString(row.name, endpoint, false),
    description: optionalText(row.description, endpoint),
  }
  if (row.type === 'endpoint') {
    try {
      return {
        ...base,
        type: 'endpoint',
        items: JSON.stringify(parseModelEndpoints(items), null, 2),
      }
    } catch {
      invalidResponse(endpoint)
    }
  }
  return { ...base, type: row.type, items: parseStringArray(items, endpoint) }
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
    const data = record(await api.get(endpoint, params, { signal }), endpoint)
    const vendor_counts: Record<string, number> = {}
    for (const [id, count] of Object.entries(
      record(data.vendor_counts ?? {}, endpoint)
    ))
      vendor_counts[id] = requiredStrictInteger(count, endpoint)
    return { ...parsePage(data, endpoint, parseModelMetadata), vendor_counts }
  },
  async model(id: number, signal?: AbortSignal) {
    const endpoint = `/api/models/${id}`
    return parseModelMetadata(
      await api.get(endpoint, undefined, { signal }),
      endpoint
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
  async groups(signal?: AbortSignal, type?: PrefillGroup['type']) {
    const endpoint = '/api/prefill_group/'
    return array(
      (await api.get(endpoint, type ? { type } : undefined, { signal })) ?? [],
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
  async saveModel(input: ModelInput): Promise<ModelMetadata> {
    return parseModelMetadata(
      input.id
        ? await api.put('/api/models/', input)
        : await api.post('/api/models/', input)
    )
  },
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
