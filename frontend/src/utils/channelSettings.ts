export interface AdvancedChannelRoute extends Record<string, unknown> {
  incoming_path: string
  upstream_path: string
  converter: string
  models?: string[]
  pass_through_body_enabled?: boolean
  auth?: { type: string; name?: string; value?: string }
}

export interface AdvancedChannelConfig extends Record<string, unknown> {
  advanced_routes: AdvancedChannelRoute[]
}

const openAIFieldTypes = [1, 58, 59, 60, 62, 63]
const claudeFieldTypes = [14, 58, 59, 60, 62, 63]
const sharedFieldTypes = [1, 14, 58, 59, 60, 62, 63]

export const CHANNEL_SETTING_FIELDS = [
  { key: 'allow_service_tier', types: sharedFieldTypes },
  { key: 'disable_store', types: openAIFieldTypes },
  { key: 'allow_safety_identifier', types: openAIFieldTypes },
  { key: 'allow_include_obfuscation', types: openAIFieldTypes },
  { key: 'allow_inference_geo', types: sharedFieldTypes },
  { key: 'allow_speed', types: claudeFieldTypes },
  { key: 'claude_beta_query', types: [14] },
  { key: 'ollama_openai_chat', types: [4] },
] as const

export type ChannelSettingField = (typeof CHANNEL_SETTING_FIELDS)[number]['key']

export function parseChannelObject(value: unknown): Record<string, unknown> {
  if (value === '' || value === undefined || value === null) return {}
  const parsed: unknown = typeof value === 'string' ? JSON.parse(value) : value
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
    throw new Error('Expected a JSON object')
  return structuredClone(parsed as Record<string, unknown>)
}

function storedPluginKeys(setting: Record<string, unknown>) {
  const single =
    typeof setting.task_plugin_key === 'string' ? setting.task_plugin_key : ''
  const list = Array.isArray(setting.task_extend_plugin_keys)
    ? setting.task_extend_plugin_keys.filter(
        (key): key is string => typeof key === 'string'
      )
    : []
  return { single, list }
}

/**
 * Mirrors ChannelSettings.TaskPluginBindings in relaykit/dto/channel_settings.go:
 * the sorted, de-duplicated set bound through either field. UpdateChannel
 * compares this set to decide whether a save rebinds plugins.
 */
function taskPluginBindings(setting: Record<string, unknown>): string[] {
  const { single, list } = storedPluginKeys(setting)
  return [...new Set([single, ...list].filter(Boolean))].sort()
}

/**
 * Plugin keys the channel form edits, read as upstream readTaskExtendPluginKeys
 * does: type 61 binds task_plugin_key alone, type 60 binds the union of both
 * fields, other types bind nothing.
 */
export function formTaskPluginKeys(
  type: number,
  setting: Record<string, unknown>
): string[] {
  const { single, list } = storedPluginKeys(setting)
  const keys = [single, ...(type === 60 ? list : [])]
    .map((key) => key.trim())
    .filter(Boolean)
  if (type === 61) return keys.slice(0, 1)
  return type === 60 ? [...new Set(keys)] : []
}

/**
 * Writes the form's plugin selection the way upstream buildSettingJSON does:
 * type 61 keeps task_plugin_key only, type 60 keeps task_extend_plugin_keys
 * only, other types keep neither. An unchanged binding set returns the setting
 * as is, so a save without a rebinding never touches the binding fields.
 */
export function withTaskPluginKeys(
  setting: Record<string, unknown>,
  type: number,
  keys: string[]
): Record<string, unknown> {
  const next = type === 61 ? keys.slice(0, 1) : type === 60 ? keys : []
  const nextBindings = taskPluginBindings({ task_extend_plugin_keys: next })
  if (
    JSON.stringify(nextBindings) === JSON.stringify(taskPluginBindings(setting))
  )
    return setting
  const result = { ...setting }
  delete result.task_plugin_key
  delete result.task_extend_plugin_keys
  if (type === 61 && next[0]) result.task_plugin_key = next[0]
  if (type === 60 && next.length) result.task_extend_plugin_keys = next
  return result
}

export function advancedChannelPreset(type: number): AdvancedChannelConfig {
  const paths = [
    '/v1/chat/completions',
    '/v1/completions',
    '/v1/responses',
    '/v1/embeddings',
    '/v1/messages',
    '/v1/models',
  ]
  const advanced_routes: AdvancedChannelRoute[] = paths.map((path) => ({
    incoming_path: path,
    upstream_path: path,
    converter: 'none',
    auth: { type: 'header', name: 'Authorization', value: 'Bearer {api_key}' },
  }))
  if (type === 63)
    advanced_routes.push(
      ...['/v1/rerank', '/rerank'].map((path) => ({
        incoming_path: path,
        upstream_path: '/v1/rerank',
        converter: 'jina_rerank_to_sglang',
      }))
    )
  return { advanced_routes }
}

export function parseAdvancedChannelConfig(raw: string): AdvancedChannelConfig {
  const parsed = parseChannelObject(raw)
  if (!Array.isArray(parsed.advanced_routes) || !parsed.advanced_routes.length)
    throw new Error('advanced_routes must be a non-empty array')
  for (const item of parsed.advanced_routes) {
    const row = parseChannelObject(item)
    if (
      typeof row.incoming_path !== 'string' ||
      !row.incoming_path.startsWith('/') ||
      typeof row.upstream_path !== 'string' ||
      !row.upstream_path.trim() ||
      (row.converter !== undefined && typeof row.converter !== 'string') ||
      (row.models !== undefined &&
        (!Array.isArray(row.models) ||
          row.models.some((x: unknown) => typeof x !== 'string')))
    )
      throw new Error('Invalid advanced route')
    if (
      row.pass_through_body_enabled &&
      !['', 'none', 'jina_rerank_to_sglang'].includes(
        String(row.converter ?? '')
      )
    )
      throw new Error('Pass-through requires a native route')
  }
  return parsed as AdvancedChannelConfig
}
