import type { AdminChannelSortBy, AdminChannelStatus } from '@/types/console'

export const ADMIN_CHANNEL_OPTIONAL_FIELDS = [
  'id',
  'type',
  'status',
  'priority',
  'weight',
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

// The native core no longer exposes capacity fields.
export const ADMIN_CHANNEL_VISIBLE_FIELDS_STORAGE_KEY =
  'ren2hub_admin_channel_visible_fields_v3'

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
  2: { label: 'Midjourney', supplier: 'Midjourney' },
  5: { label: 'Midjourney Plus', supplier: 'Midjourney Plus' },
  6: { label: 'OpenAI Max', supplier: 'OpenAI Max' },
  7: { label: 'OhMyGPT', supplier: 'OhMyGPT' },
  8: { label: 'Custom', supplier: 'Custom' },
  9: { label: 'AILS', supplier: 'AILS' },
  10: { label: 'AIProxy', supplier: 'AIProxy' },
  11: { label: 'PaLM', supplier: 'PaLM' },
  12: { label: 'API2GPT', supplier: 'API2GPT' },
  13: { label: 'AIGC2D', supplier: 'AIGC2D' },
  15: { label: 'Baidu', supplier: 'Baidu' },
  16: { label: 'Zhipu', supplier: 'Zhipu' },
  18: { label: 'Xunfei', supplier: 'Xunfei' },
  19: { label: '360', supplier: '360' },
  21: { label: 'AIProxy Library', supplier: 'AIProxy Library' },
  22: { label: 'FastGPT', supplier: 'FastGPT' },
  23: { label: 'Tencent', supplier: 'Tencent' },
  26: { label: 'Zhipu V4', supplier: 'Zhipu V4' },
  27: { label: 'Perplexity', supplier: 'Perplexity' },
  31: { label: 'LingYiWanWu', supplier: 'LingYiWanWu' },
  34: { label: 'Cohere', supplier: 'Cohere' },
  35: { label: 'MiniMax', supplier: 'MiniMax' },
  36: { label: 'Suno', supplier: 'Suno' },
  37: { label: 'Dify', supplier: 'Dify' },
  38: { label: 'Jina', supplier: 'Jina' },
  39: { label: 'Cloudflare', supplier: 'Cloudflare' },
  42: { label: 'Mistral', supplier: 'Mistral' },
  44: { label: 'MokaAI', supplier: 'MokaAI' },
  45: { label: 'VolcEngine', supplier: 'VolcEngine' },
  46: { label: 'Baidu V2', supplier: 'Baidu V2' },
  47: { label: 'Xinference', supplier: 'Xinference' },
  49: { label: 'Coze', supplier: 'Coze' },
  50: { label: 'Kling', supplier: 'Kling' },
  51: { label: 'Jimeng', supplier: 'Jimeng' },
  52: { label: 'Vidu', supplier: 'Vidu' },
  53: { label: 'Submodel', supplier: 'Submodel' },
  54: { label: 'Doubao Video', supplier: 'Doubao Video' },
  55: { label: 'Sora', supplier: 'Sora' },
  56: { label: 'Replicate', supplier: 'Replicate' },
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
