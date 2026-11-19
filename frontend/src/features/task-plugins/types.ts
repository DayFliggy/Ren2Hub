// Mirrors upstream NewAPI web/src/features/task-plugins/types.ts. Ren2Hub
// differences: usage channels carry `type` (the cascade explains a New API
// unbind), version rows carry `factory`, and the server-side preview replaces
// the client-side metadata parser.
import type {
  BillingUsageExample,
  BillingUsageSchema,
} from '@/features/pricing/types'
import type { ModelPricingIssue } from '@/api/pricingConfig'

export type LocalizedTextValue = string | Record<string, string>
export type TaskPluginParts = {
  task?: boolean
  provider?: boolean
  transform?: boolean
  transform_hooks?: string[]
  login?: boolean
  refresh?: boolean
  balance?: boolean
}

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
  name_i18n?: LocalizedTextValue
  relay?: Array<{
    incoming_path: string
    upstream_path: string
    converter?: string
    models?: string[]
  }>
  configSchema?: RequestPluginConfigSchema
  /** The models and channel types a request processing plugin is made for. */
  scope?: { models?: string[] | null; channelTypes?: number[] | null } | null
  samples?: RequestPluginSample[]
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

export type TaskPluginListItem = TaskPluginParts & {
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
  skipped_routes?: string[]
}

export type TaskPluginUsage = {
  channels: Array<{ id: number; name: string; type: number }>
  in_flight_count: number
}

export type TaskPluginDetail = TaskPluginParts & {
  plugin?: TaskPluginRecord
  meta: TaskPluginMeta
  source: string
  layer: 'factory' | 'override'
  has_icon?: boolean
  skipped_routes?: string[]
  pricing_issues?: ModelPricingIssue[] | null
}

export type TaskPluginChangeNotice = {
  pricing_issues?: ModelPricingIssue[] | null
}

export type TaskPluginStatusResult = TaskPluginChangeNotice & {
  plugin_enabled: boolean
  disabled_channels: number
  unbound_channels: number
}

/** Bind options from GET /api/task_plugin_options (admin with task_plugin.bind). */
export type TaskPluginOption = TaskPluginParts & {
  channelConfig?: RequestPluginConfigSchema
  keyHint?: LocalizedTextValue
  relayPaths?: string[]
  responsesWebsocket?: boolean
  checkKey?: boolean
  /** A request processing plugin whose failure is skipped instead of refusing. */
  optional?: boolean
  /** The models and channel types the plugin is made for, if it names them. */
  scope?: { models?: string[] | null; channelTypes?: number[] | null } | null

  configSchema?: RequestPluginConfigSchema
  name_i18n?: LocalizedTextValue
  samples?: RequestPluginSample[]
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

/** Local literal metadata preview, never evaluated. */
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
export type MarketplaceDocument = {
  path: string
  sha256?: string
  sourceUrl?: string
  translations?: Record<
    string,
    { path: string; sha256?: string; sourceUrl?: string }
  >
}

export type MarketplaceIndexVersion = {
  readme?: MarketplaceDocument
  changelog?: MarketplaceDocument
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
  code?: string
  recommended?: boolean
  installs30d?: number
  author?: { login: string; url?: string }
  repo?: string
  name_i18n?: LocalizedTextValue
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
  revoked?: Array<{ sha256: string; key?: string; reason?: LocalizedTextValue }>
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

export type RequestPluginConfigField = {
  type: 'string' | 'number' | 'integer' | 'boolean' | 'array' | 'object'
  title: LocalizedTextValue
  description?: LocalizedTextValue
  default?: unknown
  examples?: unknown[]
  enum?: (string | number)[]
  enumTitles?: LocalizedTextValue[]
  minimum?: number
  maximum?: number
  /** Display unit of a number; byte values are entered in B, KB or MB. */
  unit?: 'byte' | 'pixel' | 'character' | 'second'
  /**
   * The field shows only while every listed sibling holds one of its
   * values. A hidden field keeps its value.
   */
  visibleWhen?: Record<string, (string | number | boolean)[]>
  minLength?: number
  maxLength?: number
  pattern?: string
  /** keywords is a keyword list, one per line, edited like textarea. */
  format?: 'textarea' | 'keywords' | 'regex' | 'json' | 'url' | 'password'
  items?: RequestPluginConfigField
  minItems?: number
  maxItems?: number
  uniqueItems?: boolean
  properties?: Record<string, RequestPluginConfigField>
  required?: string[]
  /**
   * A value per model: an object without `properties` whose keys are model
   * names and whose values all follow this scalar field.
   */
  additionalProperties?: RequestPluginConfigField
}

export type RequestPluginConfigSchema = {
  type: 'object'
  properties: Record<string, RequestPluginConfigField>
  required?: string[]
}

/** A plugin's scope as an administrator confirmed it. */
export type RequestPluginScope = {
  models: string[]
  channel_types: number[]
}

/**
 * One configured use of a request processing plugin. `global` is its own scope; channels bind instances by id. `models` limits it to the
 * models clients ask for; empty means every model. An instance with
 * `plugin_scope` runs where its plugin's scope says instead.
 */
export type RequestPluginInstance = {
  id: string
  name: string
  plugin: string
  enabled: boolean
  global: boolean
  models: string[]
  config: Record<string, unknown>
  /** Set when the instance runs where its plugin's scope says. */
  plugin_scope?: RequestPluginScope | null
}

/**
 * A request a plugin offers for trying it out. The body is text only: the
 * string "[sample-image]" as an item of a content or parts list stands for
 * the console's own sample image.
 */
export type RequestPluginSample = {
  name: LocalizedTextValue
  /** The base language of its text, such as "en" or "zh"; absent fits any. */
  lang?: string
  format: 'openai' | 'openai_responses' | 'claude' | 'gemini'
  body: Record<string, unknown>
  /** An answer from the model, for plugins that check replies. */
  reply?: string
}

export type RequestPluginDryRunRequest = {
  format: string
  path?: string
  model: string
  channel_id?: number
  body: unknown
  /** An unsaved instance to run alone instead of the saved ones. */
  instance?: RequestPluginInstance
  /** An answer from the model for the reply checks to read. */
  reply?: string
}

export type RequestPluginDryRunStep = {
  instance: string
  name: string
  plugin: string
  stage: string
  action: string
  effects: number
  findings?: Array<{ rule: string; category?: string; count: number }> | null
  duration_ms: number
  error?: string
}

export type RequestPluginDryRunResult = {
  rejected: { code: string; message: string; instance: string } | null
  failed: { instance: string; error: string } | null
  steps: RequestPluginDryRunStep[] | null
  body: unknown
  headers?: { set?: Record<string, string> | null; remove?: string[] | null }
  reasoning_effort?: string
  /** The effort the request had before any plugin ran; empty when none. */
  reasoning_effort_before?: string
  /** The reply checks' result, when the run had a reply; `text` is masked. */
  reply?: {
    text: string
    stopped: { instance: string; code: string; message: string } | null
  } | null
  /** What the log owner would see about this request in their logs. */
  record?: Record<string, unknown>[] | null
}
