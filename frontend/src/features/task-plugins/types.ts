// Mirrors upstream NewAPI web/src/features/task-plugins/types.ts. Ren2Hub
// differences: usage channels carry `type` (the cascade explains a New API
// unbind), version rows carry `factory`, and the server-side preview replaces
// the client-side metadata parser.
import type {
  BillingUsageExample,
  BillingUsageSchema,
} from '@/features/pricing/types'

export type TaskPluginProtocolClaim =
  | string
  | {
      name: string
      models?: string[]
      supports?: ('stream' | 'sync' | 'background')[]
    }

/** One native route declared by `meta.routes`; hook bindings stay internal. */
export type TaskPluginRoute = {
  method: string
  path: string
  type: 'submit' | 'query' | 'dynamic'
  models?: string[]
}

export type TaskPluginMeta = {
  sortPriority?: number
  website?: string
  apiVersion: number
  key: string
  name: string
  icon?: string
  description?: Record<string, string>
  version: string
  author?: { name: string; url?: string }
  baseUrl?: string
  channelTypes?: number[] | null
  models: string[] | null
  fetchMode?: string
  allowedHosts?: string[] | null
  upstreams?: string[]
  routes?: TaskPluginRoute[] | null
  protocols?: TaskPluginProtocolClaim[] | null
  usageSchema?: BillingUsageSchema
  usageProfiles?: {
    models: string[]
    schema: BillingUsageSchema
    examples?: BillingUsageExample[]
  }[]
}

export type TaskPluginRecord = {
  id: number
  key: string
  api_version: number
  version: string
  source: string
  source_hash: string
  enabled: boolean
  active: boolean
  factory: boolean
  created_at: number
  remark: string
}

export type TaskPluginRuntimeStatus =
  | 'registered'
  | 'compile_failed'
  | 'disabled'
  | 'disabled_fallback'
  | 'not_registered'

export type TaskPluginListItem = {
  meta: TaskPluginMeta
  source: 'factory' | 'override' | 'override_over_factory'
  enabled: boolean
  active: boolean
  source_hash: string
  has_icon?: boolean
  remark: string
  runtime_status: TaskPluginRuntimeStatus
  runtime_error?: string
  factory_meta?: TaskPluginMeta
  channel_count: number
  in_flight_count: number
}

export type TaskPluginUsage = {
  channels: Array<{ id: number; name: string; type: number }>
  in_flight_count: number
}

export type TaskPluginDetail = {
  plugin?: TaskPluginRecord
  meta: TaskPluginMeta
  source: string
  layer: 'factory' | 'override'
  has_icon?: boolean
}

/** Bind options from GET /api/task_plugin_options (admin with task_plugin.bind). */
export type TaskPluginOption = {
  key: string
  name: string
  description?: Record<string, string>
  icon?: string
  hasIcon?: boolean
  baseUrl?: string
  sortPriority?: number
  website?: string
  models: string[] | null
  channelTypes?: number[] | null
  upstreams?: string[] | null
}

/** POST /api/plugin/task/preview: literal metadata, never evaluated. */
export type TaskPluginPreview = {
  meta: Record<string, unknown>
  unresolved: string[]
  source_hash: string
}

export type TaskPluginDryRunRequest = {
  hook: string
  member?: string
  args: unknown[]
}

export type MarketplaceSource = {
  name: string
  index_url: string
}

/** One installable version from a marketplace index. */
export type MarketplaceIndexVersion = {
  version: string
  path: string
  sha256?: string
  minApiVersion?: number
  kind?: string
  allowedHosts?: string[]
  baseUrl?: string
  auth?: string
}

export type MarketplacePluginIcon = {
  /** Index-relative path of the sidecar icon.svg / icon.png. */
  path: string
  sha256?: string
}

export type MarketplacePlugin = {
  protocols?: TaskPluginProtocolClaim[]
  sortPriority?: number
  website?: string
  key: string
  name: string
  icon?: string
  iconFile?: MarketplacePluginIcon
  description?: string | Record<string, string>
  channelTypes?: number[]
  models?: string[]
  latest: string
  versions: MarketplaceIndexVersion[]
}

export type MarketplaceIndex = {
  indexVersion: number
  name: string
  plugins: MarketplacePlugin[]
}

/** A missing source property is distinct from an empty value or an unreadable one. */
export type PluginPreviewField<T> =
  | { state: 'value'; value: T; origin: 'source' | 'index' }
  | { state: 'missing'; origin: 'source' }
  | { state: 'unknown' }

export type PluginPreviewValues = {
  models: string[]
  protocols: TaskPluginProtocolClaim[]
  routes: TaskPluginRoute[]
  channelTypes: number[]
  baseUrl: string
  allowedHosts: string[]
  auth: string
}

export type PluginMetaPreview = {
  status: 'parsed' | 'partial' | 'unavailable'
  fields: {
    [Key in keyof PluginPreviewValues]: PluginPreviewField<
      PluginPreviewValues[Key]
    >
  }
}
