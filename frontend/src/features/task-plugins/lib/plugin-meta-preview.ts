// Follows upstream lib/plugin-meta-preview.ts, but Ren2Hub reads metadata on
// the server (POST /api/plugin/task/preview) instead of parsing JavaScript in
// the browser. The server returns literal properties and names the ones it
// could not resolve; this module maps that onto the same field states.
import type {
  MarketplaceIndexVersion,
  MarketplacePlugin,
  PluginMetaPreview,
  PluginPreviewField,
  PluginPreviewValues,
  TaskPluginPreview,
  TaskPluginProtocolClaim,
  TaskPluginRoute,
} from '../types'

const NO_VALUE = Symbol('invalid preview value')
type Parsed<T> = T | typeof NO_VALUE

const isText = (value: unknown): value is string =>
  typeof value === 'string' && value.length > 0

function textArray(value: unknown): Parsed<string[]> {
  return Array.isArray(value) && value.every(isText) ? value : NO_VALUE
}

const MODES = ['stream', 'sync', 'background'] as const

/** Shared with the marketplace index parser. */
export function parseProtocolClaims(
  value: unknown
): Parsed<TaskPluginProtocolClaim[]> {
  if (!Array.isArray(value)) return NO_VALUE
  const claims: TaskPluginProtocolClaim[] = []
  for (const item of value) {
    if (isText(item)) {
      claims.push(item)
      continue
    }
    if (!item || typeof item !== 'object' || !isText(item.name)) return NO_VALUE
    const models =
      item.models === undefined ? undefined : textArray(item.models)
    const supports = item.supports
    if (models === NO_VALUE) return NO_VALUE
    if (
      supports !== undefined &&
      (!Array.isArray(supports) ||
        !supports.every((mode: unknown) =>
          MODES.includes(mode as (typeof MODES)[number])
        ))
    )
      return NO_VALUE
    claims.push({ name: item.name, models, supports })
  }
  return claims
}

function parseRoutes(value: unknown): Parsed<TaskPluginRoute[]> {
  if (!Array.isArray(value)) return NO_VALUE
  const routes: TaskPluginRoute[] = []
  for (const item of value) {
    if (
      !item ||
      typeof item !== 'object' ||
      !isText(item.method) ||
      !isText(item.path) ||
      !['submit', 'query', 'dynamic'].includes(item.type)
    )
      return NO_VALUE
    const models =
      item.models === undefined ? undefined : textArray(item.models)
    if (models === NO_VALUE) return NO_VALUE
    routes.push({
      method: item.method,
      path: item.path,
      type: item.type,
      models,
    })
  }
  return routes
}

function parseChannelTypes(value: unknown): Parsed<number[]> {
  return Array.isArray(value) &&
    value.every((item) => Number.isInteger(item) && item >= 0)
    ? value
    : NO_VALUE
}

function parseAuth(value: unknown): Parsed<string> {
  if (typeof value === 'string') return value
  if (value && typeof value === 'object' && 'type' in value)
    return typeof value.type === 'string' ? value.type : NO_VALUE
  return NO_VALUE
}

const parsers: {
  [Key in keyof PluginPreviewValues]: (
    value: unknown
  ) => Parsed<PluginPreviewValues[Key]>
} = {
  models: textArray,
  protocols: parseProtocolClaims,
  routes: parseRoutes,
  channelTypes: parseChannelTypes,
  baseUrl: (value) => (typeof value === 'string' ? value : NO_VALUE),
  allowedHosts: textArray,
  auth: parseAuth,
}

const fieldKeys = Object.keys(parsers) as (keyof PluginPreviewValues)[]

function unknownFields(): PluginMetaPreview['fields'] {
  return Object.fromEntries(
    fieldKeys.map((key) => [key, { state: 'unknown' }])
  ) as PluginMetaPreview['fields']
}

/** Used when the source cannot be read at all (syntax error, no literal meta). */
export function unavailablePreview(): PluginMetaPreview {
  return { status: 'unavailable', fields: unknownFields() }
}

/** Maps the server preview onto value / missing / unknown per field. */
export function previewFromServer(
  preview: TaskPluginPreview
): PluginMetaPreview {
  // "meta" means the whole object is unsafe to read (spread, escape, mutation).
  if (preview.unresolved.includes('meta')) return unavailablePreview()
  const fields = unknownFields()
  let status: PluginMetaPreview['status'] = 'parsed'
  for (const key of fieldKeys) {
    if (preview.unresolved.includes(key)) {
      status = 'partial'
      continue
    }
    if (!Object.hasOwn(preview.meta, key)) {
      fields[key] = { state: 'missing', origin: 'source' }
      continue
    }
    const value = parsers[key](preview.meta[key])
    if (value === NO_VALUE) {
      status = 'partial'
      continue
    }
    Object.assign(fields, {
      [key]: { state: 'value', value, origin: 'source' },
    })
  }
  return { status, fields }
}

/** Source absence is a fact; only unresolved source fields fall back to index hints. */
export function resolvePluginMetaPreview(
  plugin: MarketplacePlugin,
  version: MarketplaceIndexVersion | undefined,
  source: PluginMetaPreview | undefined
): PluginMetaPreview['fields'] {
  const latest = version?.version === plugin.latest
  const index: Record<keyof PluginPreviewValues, unknown> = {
    models: latest ? plugin.models : undefined,
    protocols: latest ? plugin.protocols : undefined,
    channelTypes: latest ? plugin.channelTypes : undefined,
    routes: undefined,
    baseUrl: version?.baseUrl,
    allowedHosts: version?.allowedHosts,
    auth: version?.auth,
  }
  const fields = unknownFields()
  for (const key of fieldKeys) {
    const fromSource = source?.fields[key]
    let field: PluginPreviewField<unknown> = fromSource ?? { state: 'unknown' }
    if (!fromSource || fromSource.state === 'unknown') {
      const value = parsers[key](index[key])
      if (value !== NO_VALUE) field = { state: 'value', value, origin: 'index' }
    }
    Object.assign(fields, { [key]: field })
  }
  return fields
}
