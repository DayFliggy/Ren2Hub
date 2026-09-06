import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredString,
  requiredStrictNumber,
} from './contracts'

export const MODEL_PRICE_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
] as const
export type ModelPriceKey = (typeof MODEL_PRICE_KEYS)[number]
export type ModelPriceMaps = Partial<
  Record<ModelPriceKey, Record<string, number>>
>
export type ModelPriceFields = Record<ModelPriceKey, string>
export type ModelPriceMode = 'per-token' | 'per-request'

function priceForModel(
  map: Record<string, number> | undefined,
  name: string
): number | undefined {
  return map && Object.hasOwn(map, name) ? map[name] : undefined
}

export function parseModelPriceOptions(value: unknown): ModelPriceMaps {
  const endpoint = '/api/option/'
  if (!Array.isArray(value)) invalidResponse(endpoint)
  const maps: ModelPriceMaps = {}
  for (const option of value) {
    if (!isRecord(option)) invalidResponse(endpoint)
    const key = requiredString(option.key, endpoint)
    if (!MODEL_PRICE_KEYS.includes(key as ModelPriceKey)) continue
    if (Object.hasOwn(maps, key)) invalidResponse(endpoint)
    const raw = requiredString(option.value, endpoint)
    let parsed: unknown
    try {
      parsed = JSON.parse(raw)
    } catch {
      invalidResponse(endpoint)
    }
    if (!isRecord(parsed)) invalidResponse(endpoint)
    const values: Record<string, number> = {}
    for (const [name, value] of Object.entries(parsed)) {
      const price = requiredStrictNumber(value, endpoint)
      if (price < 0) invalidResponse(endpoint)
      Object.defineProperty(values, name, {
        value: price,
        enumerable: true,
        configurable: true,
        writable: true,
      })
    }
    maps[key as ModelPriceKey] = values
  }
  return maps
}

export function modelPriceFields(
  maps: ModelPriceMaps,
  name: string
): ModelPriceFields {
  return Object.fromEntries(
    MODEL_PRICE_KEYS.map((key) => [
      key,
      priceForModel(maps[key], name)?.toString() ?? '',
    ])
  ) as ModelPriceFields
}

export function validateModelPriceFields(fields: ModelPriceFields): void {
  for (const value of Object.values(fields)) {
    if (
      value.trim() &&
      (!/^(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(value.trim()) ||
        !Number.isFinite(Number(value)) ||
        Number(value) < 0)
    )
      throw new Error('priceInvalidNumber')
  }
}

export interface ModelPriceEdit {
  initial: ModelPriceMaps
  originalName: string
  name: string
  fields: ModelPriceFields
  mode: ModelPriceMode
  dirty: boolean
}

// Merge only this model into a fresh options snapshot. Concurrent target edits
// are rejected; unrelated model changes and options this form never loaded survive.
export function mergeModelPriceEdit(
  latest: ModelPriceMaps,
  edit: ModelPriceEdit
): Partial<Record<ModelPriceKey, string>> {
  validateModelPriceFields(edit.fields)
  const patch: Partial<Record<ModelPriceKey, string>> = {}
  const renamed = Boolean(edit.originalName && edit.originalName !== edit.name)
  if (!edit.dirty && !renamed) return patch
  for (const key of MODEL_PRICE_KEYS) {
    const initial = edit.initial[key]
    if (!initial) continue
    const current = latest[key]
    if (!current) throw new Error('priceUnavailable')
    const oldValue = edit.originalName
      ? priceForModel(initial, edit.originalName)
      : undefined
    if (
      edit.originalName &&
      priceForModel(current, edit.originalName) !== oldValue
    )
      throw new Error('priceConcurrentChange')
    const raw = edit.fields[key].trim()
    const active =
      edit.mode === 'per-request' ? key === 'ModelPrice' : key !== 'ModelPrice'
    const value = edit.dirty
      ? active && raw !== ''
        ? Number(raw)
        : undefined
      : oldValue
    if (edit.name !== edit.originalName && Object.hasOwn(current, edit.name)) {
      if (value !== undefined || renamed) throw new Error('priceTargetConflict')
      continue
    }
    const next = { ...current }
    if (renamed) delete next[edit.originalName]
    if (value === undefined) delete next[edit.name]
    else
      Object.defineProperty(next, edit.name, {
        value,
        enumerable: true,
        configurable: true,
        writable: true,
      })
    if (JSON.stringify(next) !== JSON.stringify(current))
      patch[key] = JSON.stringify(next)
  }
  return patch
}

export const modelPricingApi = {
  async load(signal?: AbortSignal): Promise<ModelPriceMaps> {
    return parseModelPriceOptions(
      await api.get('/api/option/', undefined, { signal })
    )
  },
  async save(edit: ModelPriceEdit): Promise<ModelPriceMaps> {
    const latest = await modelPricingApi.load()
    const options = mergeModelPriceEdit(latest, edit)
    if (Object.keys(options).length)
      await api.put('/api/option/bulk', { options })
    return {
      ...latest,
      ...Object.fromEntries(
        Object.entries(options).map(([key, value]) => [key, JSON.parse(value)])
      ),
    }
  },
}
