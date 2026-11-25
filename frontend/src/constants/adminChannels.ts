import i18n from '@/i18n'
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

export const ADMIN_CHANNEL_GROUP_BY_LAB_STORAGE_KEY =
  'ren2hub_admin_channel_group_by_lab'

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
  /** Vendor logo key; only the types the create form offers carry one. */
  supplier?: string
  /** The create form offers this type and its type-specific settings. */
  creatable?: boolean
  retired?: boolean
}

/**
 * Every channel type the backend knows (constant/channel.go). Names follow
 * upstream web CHANNEL_TYPES, including the ids it leaves commented out.
 * Types without `creatable` are shown by name and allow common-field edits
 * only; the create list stays the set Ren2Hub configures end to end.
 */
export const ADMIN_CHANNEL_TYPE_META: Record<number, AdminChannelTypeMeta> = {
  1: { label: 'OpenAI', supplier: 'OpenAI', creatable: true },
  2: { label: 'MjProxy' },
  3: { label: 'Azure', supplier: 'OpenAI', creatable: true },
  4: { label: 'Ollama', supplier: 'Ollama', creatable: true },
  5: { label: 'MjProxyPlus' },
  6: { label: 'OpenAIMax' },
  7: { label: 'OhMyGPT' },
  8: { label: 'Custom' },
  9: { label: 'AILS' },
  10: { label: 'AI Proxy' },
  11: { label: 'PaLM' },
  12: { label: 'API2GPT' },
  13: { label: 'AIGC2D' },
  14: { label: 'Anthropic', supplier: 'Anthropic', creatable: true },
  15: { label: 'Baidu' },
  16: { label: 'Zhipu' },
  17: { label: 'Ali', supplier: '阿里通义', creatable: true },
  18: { label: 'Xunfei' },
  19: { label: '360' },
  20: { label: 'OpenRouter', supplier: 'OpenAI', creatable: true },
  21: { label: 'AI Proxy Library' },
  22: { label: 'FastGPT' },
  23: { label: 'Tencent' },
  24: { label: 'Gemini', supplier: 'Google', creatable: true },
  25: { label: 'Moonshot', supplier: 'Moonshot', creatable: true },
  26: { label: 'Zhipu GLM' },
  27: { label: 'Perplexity' },
  31: { label: 'LingYiWanWu' },
  33: { label: 'AWS Bedrock', supplier: 'Anthropic', creatable: true },
  34: { label: 'Cohere' },
  35: { label: 'MiniMax' },
  36: { label: 'SunoAPI' },
  37: { label: 'Dify' },
  38: { label: 'Jina' },
  39: { label: 'Cloudflare' },
  40: { label: 'SiliconFlow', supplier: 'DeepSeek', creatable: true },
  41: { label: 'Vertex AI', supplier: 'Google', creatable: true },
  42: { label: 'Mistral' },
  43: { label: 'DeepSeek', supplier: 'DeepSeek', creatable: true },
  44: { label: 'MokaAI' },
  45: { label: 'VolcEngine' },
  46: { label: 'Baidu V2' },
  47: { label: 'Xinference' },
  48: { label: 'xAI', supplier: 'xAI', creatable: true },
  49: { label: 'Coze' },
  50: { label: 'Kling' },
  51: { label: 'Jimeng' },
  52: { label: 'Vidu' },
  53: { label: 'Submodel' },
  54: { label: 'Doubao' },
  55: { label: 'Sora' },
  56: { label: 'Replicate' },
  57: {
    label: 'ChatGPT Subscription (Codex)',
    supplier: 'OpenAI',
    retired: true,
  },
  58: { label: 'Advanced Custom', supplier: 'Custom', creatable: true },
  59: { label: 'Sub2API', supplier: 'Sub2API', creatable: true },
  60: { label: 'New API', supplier: 'New API', creatable: true },
  61: { label: 'Task Plugin', supplier: 'Plugin', creatable: true },
  62: { label: 'vLLM', supplier: 'vLLM', creatable: true },
  63: { label: 'SGLang', supplier: 'SGLang', creatable: true },
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
      label: i18n.global.t('channels.unknownType', { type }),
    }
  )
}

/** Logo key for a type; types without a vendor reuse their name. */
export function adminChannelTypeVendor(type: number): string {
  const meta = adminChannelTypeMeta(type)
  return meta.supplier ?? meta.label
}

export function isKnownChannelType(type: number): boolean {
  return Object.hasOwn(ADMIN_CHANNEL_TYPE_META, type)
}

export function isCreatableChannelType(type: number): boolean {
  return ADMIN_CHANNEL_TYPE_META[type]?.creatable === true
}

type ChannelLab = AdminChannel['lab']

/**
 * Display name for a backend lab (model/channel_lab.go). The ids unknown and
 * mixed are classifications whose names are lowercase identifiers.
 */
export function labDisplayName(lab: Pick<ChannelLab, 'id' | 'name'>): string {
  if (lab.id === 'unknown') return i18n.global.t('channels.labUnknown')
  if (lab.id === 'mixed') return i18n.global.t('channels.labMixed')
  return lab.name
}

/** The backend could not read or follow this channel's model mapping. */
export function hasInvalidModelMapping(lab: ChannelLab): boolean {
  return lab.source === 'invalid_mapping'
}

export interface ChannelLabGroup {
  groupSlug: string
  supplier: string
  channels: AdminChannel[]
}

/** Groups channels by backend lab id, keeping first-seen order. */
export function groupChannelsByLab(
  channels: readonly AdminChannel[]
): ChannelLabGroup[] {
  const groups = new Map<string, ChannelLabGroup>()
  for (const channel of channels) {
    const group = groups.get(channel.lab.id)
    if (group) {
      group.channels.push(channel)
      continue
    }
    groups.set(channel.lab.id, {
      groupSlug: channel.lab.id,
      supplier: labDisplayName(channel.lab),
      channels: [channel],
    })
  }
  return [...groups.values()]
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

export function supportsBalanceQuery(
  channel: Pick<AdminChannel, 'type'>
): boolean {
  return channel.type === 61 || BALANCE_QUERY_TYPES.has(channel.type)
}

/**
 * Mirrors channelSensitiveFields in controller/channel_authz.go: the upstream
 * edit-form list plus channel_ratio. Without ChannelSensitiveWrite
 * the edit form sends none of them, so UpdateChannel keeps the stored values
 * and skips the type-specific validation they would trigger.
 */
/**
 * Value an update sends to clear a field the reader cannot see; omitting the
 * field keeps the stored value.
 */
export const HIDDEN_FIELD_CLEAR_VALUE = ''

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
  path: string
}> = [
  {
    value: 'openai',
    path: '/v1/chat/completions',
    label: 'OpenAI (/v1/chat/completions)',
  },
  {
    value: 'openai-response',
    path: '/v1/responses',
    label: 'OpenAI Responses (/v1/responses)',
  },
  {
    value: 'openai-response-compact',
    path: '/v1/responses/compact',
    label: 'OpenAI Response Compaction (/v1/responses/compact)',
  },
  {
    value: 'anthropic',
    path: '/v1/messages',
    label: 'Anthropic (/v1/messages)',
  },
  {
    value: 'gemini',
    path: '/v1beta/models/{model}:generateContent',
    label: 'Gemini (/v1beta/models/{model}:generateContent)',
  },
  {
    value: 'jina-rerank',
    path: '/v1/rerank',
    label: 'Jina Rerank (/v1/rerank)',
  },
  {
    value: 'image-generation',
    path: '/v1/images/generations',
    label: 'Image Generation (/v1/images/generations)',
  },
  {
    value: 'embeddings',
    path: '/v1/embeddings',
    label: 'Embeddings (/v1/embeddings)',
  },
]
