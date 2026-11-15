// Mirrors the dry-run and stage helpers of upstream NewAPI
// web/src/features/task-plugins/lib/request-plugins.ts. Ren2Hub difference:
// labels are i18n keys under pluginExtensions.rp, and no business group exists.
import type { TaskPluginOption } from '../types'

/** The stage hooks a request meets, in order. */
export const REQUEST_PLUGIN_STAGES = [
  'ingress',
  'request',
  'outbound',
  'egress',
  'reply',
  'report',
] as const

/** The i18n key of a stage hook, or null for a hook the console does not know. */
export function requestPluginStageKey(stage: string): string | null {
  return (REQUEST_PLUGIN_STAGES as readonly string[]).includes(stage)
    ? `pluginExtensions.rp.stages.${stage}`
    : null
}

export type RequestPluginFailure = 'skipped' | 'refuses' | 'stopsReply'

/**
 * What a failure of the plugin does. Only request stages can refuse a
 * request; a failed reply check stops the answer and a report holds nothing
 * up. A plugin without its stages listed is taken to change requests.
 */
export function requestPluginFailure(
  plugin: TaskPluginOption
): RequestPluginFailure | null {
  const hooks = plugin.transform_hooks ?? []
  const checksRequest =
    hooks.length === 0 ||
    hooks.some((hook) => hook !== 'reply' && hook !== 'report')
  const checksReply = hooks.includes('reply')
  if (!checksRequest && !checksReply) return null
  if (plugin.optional) return 'skipped'
  return checksRequest ? 'refuses' : 'stopsReply'
}

/** Formats a request can be tried in, with the default model of each. */
export const REQUEST_PLUGIN_DRY_RUN_FORMATS = [
  { value: 'openai', label: 'OpenAI Chat Completions', model: 'gpt-4.1-mini' },
  {
    value: 'openai_responses',
    label: 'OpenAI Responses',
    model: 'gpt-4.1-mini',
  },
  { value: 'claude', label: 'Claude Messages', model: 'claude-sonnet-4-5' },
  { value: 'gemini', label: 'Gemini', model: 'gemini-2.5-flash' },
] as const

export function requestPluginDryRunPath(format: string, model: string) {
  if (format === 'openai_responses') return '/v1/responses'
  if (format === 'claude') return '/v1/messages'
  if (format === 'gemini') return `/v1beta/models/${model}:generateContent`
  return '/v1/chat/completions'
}

const EXAMPLE_PROMPT =
  'My email is alice@example.com. Summarize the attached sales notes in three bullet points.'

/** A realistic request in each format, for the dry run body. */
export function requestPluginDryRunExample(format: string, model: string) {
  if (format === 'openai_responses') {
    return {
      model,
      instructions: 'You are a concise assistant.',
      input: [{ role: 'user', content: EXAMPLE_PROMPT }],
      reasoning: { effort: 'low' },
    }
  }
  if (format === 'claude') {
    return {
      model,
      max_tokens: 1024,
      system: 'You are a concise assistant.',
      messages: [{ role: 'user', content: EXAMPLE_PROMPT }],
    }
  }
  if (format === 'gemini') {
    return {
      systemInstruction: { parts: [{ text: 'You are a concise assistant.' }] },
      contents: [{ role: 'user', parts: [{ text: EXAMPLE_PROMPT }] }],
      generationConfig: { maxOutputTokens: 1024 },
    }
  }
  return {
    model,
    messages: [
      { role: 'system', content: 'You are a concise assistant.' },
      { role: 'user', content: EXAMPLE_PROMPT },
    ],
    stream: false,
  }
}

/** The request body as a JSON object, or null when it is anything else. */
export function parseRequestPluginBody(
  text: string
): Record<string, unknown> | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  return isObject(parsed) ? parsed : null
}

/** A channel ID typed by the admin: undefined when empty, null when invalid. */
export function parseRequestPluginChannel(
  text: string
): number | undefined | null {
  if (!text.trim()) return undefined
  const value = Number(text)
  return Number.isSafeInteger(value) && value > 0 ? value : null
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export type RequestPluginChangedText = {
  path: string
  before: string
  after: string
}

/**
 * Up to three texts a run rewrote, in document order: strings at the same
 * place in both bodies that differ. Data URLs and base64 images are left out.
 */
export function requestPluginChangedTexts(
  before: unknown,
  after: unknown,
  path = '',
  found: RequestPluginChangedText[] = []
): RequestPluginChangedText[] {
  if (found.length === 3) return found
  if (typeof before === 'string' && typeof after === 'string') {
    if (
      before !== after &&
      !before.startsWith('data:') &&
      !after.startsWith('data:')
    )
      found.push({ path, before, after })
  } else if (Array.isArray(before) && Array.isArray(after)) {
    after.forEach((item, index) =>
      requestPluginChangedTexts(before[index], item, `${path}/${index}`, found)
    )
  } else if (isObject(before) && isObject(after) && !imageData(after)) {
    for (const [key, value] of Object.entries(after))
      requestPluginChangedTexts(before[key], value, `${path}/${key}`, found)
  }
  return found
}

/** Whether an object holds an image as base64 data next to its MIME type. */
function imageData(object: Record<string, unknown>) {
  const mime = object.media_type ?? object.mimeType ?? object.mime_type
  return (
    typeof mime === 'string' &&
    mime.startsWith('image/') &&
    typeof object.data === 'string'
  )
}
