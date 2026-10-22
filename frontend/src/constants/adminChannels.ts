import type {
  AdminChannel,
  AdminChannelSortBy,
  AdminChannelStatus,
  AdminChannelUpdateInput,
} from '@/types/console'

export const ADMIN_CHANNEL_OPTIONAL_FIELDS = [
  'id',
  'type',
  'status',
  'priority',
  'weight',
  'capacity',
  'usage',
  'upstream',
  'response',
  'rowUpstreamAction',
  'rowResponseAction',
] as const

export type AdminChannelOptionalField =
  (typeof ADMIN_CHANNEL_OPTIONAL_FIELDS)[number]

export const ADMIN_CHANNEL_DEFAULT_VISIBLE_FIELDS: AdminChannelOptionalField[] =
  ADMIN_CHANNEL_OPTIONAL_FIELDS.filter(
    (field) =>
      field !== 'id' &&
      field !== 'priority' &&
      field !== 'weight' &&
      field !== 'rowUpstreamAction'
  )

// v2: defaults changed (priority/weight hidden, row test action shown), so a
// new key lets the new defaults take effect on browsers with stored settings.
export const ADMIN_CHANNEL_VISIBLE_FIELDS_STORAGE_KEY =
  'ren2hub_admin_channel_visible_fields_v2'

export function sanitizeAdminChannelVisibleFields(
  fields: readonly string[]
): AdminChannelOptionalField[] {
  const allowed = new Set<string>(ADMIN_CHANNEL_OPTIONAL_FIELDS)
  return ADMIN_CHANNEL_OPTIONAL_FIELDS.filter(
    (field) => allowed.has(field) && fields.includes(field)
  )
}

export interface AdminChannelTypeMeta {
  label: string
  supplier: string
  retired?: boolean
}

export const ADMIN_CHANNEL_TYPE_META: Record<number, AdminChannelTypeMeta> = {
  1: { label: 'OpenAI', supplier: 'OpenAI' },
  3: { label: 'Azure', supplier: 'OpenAI' },
  4: { label: 'Ollama', supplier: 'Ollama' },
  14: { label: 'Anthropic', supplier: 'Anthropic' },
  17: { label: 'Ali', supplier: '阿里通义' },
  20: { label: 'OpenRouter', supplier: 'OpenAI' },
  24: { label: 'Gemini', supplier: 'Google' },
  25: { label: 'Moonshot', supplier: 'Moonshot' },
  33: { label: 'AWS Bedrock', supplier: 'Anthropic' },
  40: { label: 'SiliconFlow', supplier: 'DeepSeek' },
  41: { label: 'Vertex AI', supplier: 'Google' },
  43: { label: 'DeepSeek', supplier: 'DeepSeek' },
  48: { label: 'xAI', supplier: 'xAI' },
  57: {
    label: 'ChatGPT Subscription (Codex)',
    supplier: 'OpenAI',
    retired: true,
  },
  58: { label: 'Advanced Custom', supplier: 'Custom' },
  59: { label: 'Sub2API', supplier: 'Sub2API' },
  60: { label: 'New API', supplier: 'New API' },
  61: { label: 'Task Plugin', supplier: 'Plugin' },
  62: { label: 'vLLM', supplier: 'vLLM' },
  63: { label: 'SGLang', supplier: 'SGLang' },
}

export const ADMIN_CHANNEL_SORT_FIELDS: AdminChannelSortBy[] = [
  'id',
  'name',
  'priority',
  'balance',
  'response_time',
]

export function adminChannelTypeMeta(type: number): AdminChannelTypeMeta {
  return (
    ADMIN_CHANNEL_TYPE_META[type] ?? {
      label: `Type ${type}`,
      supplier: `Type ${type}`,
    }
  )
}

export function isRetiredChannelType(type: number): boolean {
  return ADMIN_CHANNEL_TYPE_META[type]?.retired === true
}

/**
 * Every non-retired channel opens the edit form for a channel writer. The form
 * sends only the fields the caller may write, so a missing sensitive_write or
 * task_plugin.bind permission never hides the edit entry.
 */
export function canEditChannel(
  channel: Pick<AdminChannel, 'type'>,
  canWrite: boolean
): boolean {
  return canWrite && !isRetiredChannelType(channel.type)
}

// Types with a real branch in controller/channel-billing.go: Advanced Custom in
// updateChannelBalance, the rest in updateStandardChannelBalance. Every other
// type answers "Not implemented yet".
const BALANCE_QUERY_TYPES: ReadonlySet<number> = new Set([
  1, 8, 10, 12, 13, 20, 25, 40, 43, 58, 59, 60,
])

/** UpdateChannelBalance also rejects every multi-key channel. */
export function supportsBalanceQuery(
  channel: Pick<AdminChannel, 'type' | 'channel_info'>
): boolean {
  return (
    BALANCE_QUERY_TYPES.has(channel.type) &&
    channel.channel_info?.is_multi_key !== true
  )
}

/**
 * Mirrors channelSensitiveFields in controller/channel_authz.go: the upstream
 * edit-form list plus channel_ratio and key_mode. Without ChannelSensitiveWrite
 * the edit form sends none of them, so UpdateChannel keeps the stored values
 * and skips the type-specific validation they would trigger.
 */
export const CHANNEL_SENSITIVE_UPDATE_FIELDS: readonly string[] = [
  'type',
  'key',
  'base_url',
  'openai_organization',
  'param_override',
  'header_override',
  'setting',
  'settings',
  'other',
  'channel_ratio',
  'key_mode',
]

export function withoutSensitiveChannelFields(
  input: AdminChannelUpdateInput
): AdminChannelUpdateInput {
  // Every listed field is optional in AdminChannelUpdateInput.
  return Object.fromEntries(
    Object.entries(input).filter(
      ([field]) => !CHANNEL_SENSITIVE_UPDATE_FIELDS.includes(field)
    )
  ) as AdminChannelUpdateInput
}

export function adminChannelStatusTone(
  status: AdminChannelStatus
): 'success' | 'danger' | 'warning' {
  if (status === 1) return 'success'
  if (status === 2) return 'danger'
  return 'warning'
}

export function adminChannelStatusLabelKey(
  status: AdminChannelStatus
):
  | 'channels.statusEnabled'
  | 'channels.statusDisabled'
  | 'channels.statusAutoDisabled' {
  if (status === 1) return 'channels.statusEnabled'
  if (status === 2) return 'channels.statusDisabled'
  return 'channels.statusAutoDisabled'
}

export function adminChannelResponseTone(
  responseTime: number
): 'neutral' | 'success' | 'warning' | 'danger' {
  if (responseTime <= 0) return 'neutral'
  if (responseTime <= 1_000) return 'success'
  if (responseTime <= 2_000) return 'warning'
  return 'danger'
}

export function adminChannelResponseText(
  responseTime: number,
  untestedLabel: string
): string {
  if (responseTime <= 0) return untestedLabel
  if (responseTime < 1_000) return `${responseTime}ms`
  return `${(responseTime / 1_000).toFixed(2)}s`
}

/**
 * Endpoint overrides accepted by GET /api/channel/test/:id via
 * ?endpoint_type=. Values mirror relaykit/types/endpoint_type.go; an empty
 * value means auto-detection (no query parameter).
 */
export const CHANNEL_TEST_ENDPOINT_OPTIONS: ReadonlyArray<{
  value: string
  label: string
}> = [
  { value: 'openai', label: 'OpenAI (/v1/chat/completions)' },
  { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
  {
    value: 'openai-response-compact',
    label: 'OpenAI Response Compaction (/v1/responses/compact)',
  },
  { value: 'anthropic', label: 'Anthropic (/v1/messages)' },
  {
    value: 'gemini',
    label: 'Gemini (/v1beta/models/{model}:generateContent)',
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
  {
    value: 'image-generation',
    label: 'Image Generation (/v1/images/generations)',
  },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
]
