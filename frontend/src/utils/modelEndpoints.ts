import { isRecord } from '@/api/contracts'

export const MODEL_ENDPOINT_TEMPLATES = {
  openai: { path: '/v1/chat/completions', method: 'POST' },
  'openai-response': { path: '/v1/responses', method: 'POST' },
  'openai-response-compact': { path: '/v1/responses/compact', method: 'POST' },
  'openai-alpha-search': { path: '/v1/alpha/search', method: 'POST' },
  'openai-video': { path: '/v1/videos', method: 'POST' },
  anthropic: { path: '/v1/messages', method: 'POST' },
  gemini: { path: '/v1beta/models/{model}:generateContent', method: 'POST' },
  'jina-rerank': { path: '/v1/rerank', method: 'POST' },
  'image-generation': { path: '/v1/images/generations', method: 'POST' },
  embeddings: { path: '/v1/embeddings', method: 'POST' },
} satisfies Record<string, { path: string; method: string }>

export type ModelEndpoints =
  string[] | Record<string, { path: string; method: string }>

export function parseModelEndpoints(value: unknown): ModelEndpoints {
  const parsed: unknown = typeof value === 'string' ? JSON.parse(value) : value
  if (Array.isArray(parsed)) {
    if (
      parsed.some((item) => typeof item !== 'string' || !item.trim()) ||
      new Set(parsed).size !== parsed.length
    )
      throw new Error('invalidEndpoint')
    return parsed as string[]
  }
  if (!isRecord(parsed)) throw new Error('invalidEndpoint')
  const result: Record<string, { path: string; method: string }> = {}
  const requests = new Set<string>()
  for (const [key, raw] of Object.entries(parsed)) {
    const item = typeof raw === 'string' ? { path: raw } : raw
    if (!isRecord(item)) throw new Error('invalidEndpoint')
    // Match the backend's historical string shorthand and default POST method.
    const method = Object.hasOwn(item, 'method')
      ? typeof item.method === 'string'
        ? item.method.toUpperCase()
        : item.method
      : 'POST'
    if (
      !key.trim() ||
      typeof item.path !== 'string' ||
      !/^\/(?!\/)[^\s?#]*$/.test(item.path) ||
      typeof method !== 'string' ||
      !['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].includes(
        method
      ) ||
      Object.keys(item).some((field) => field !== 'path' && field !== 'method')
    )
      throw new Error('invalidEndpoint')
    const request = `${method} ${item.path}`
    if (requests.has(request)) throw new Error('duplicateEndpoint')
    requests.add(request)
    Object.defineProperty(result, key, {
      value: { path: item.path, method },
      enumerable: true,
      writable: true,
      configurable: true,
    })
  }
  return result
}

export function appendModelNames(
  existing: string[],
  incoming: string[]
): string[] {
  return [
    ...new Set(
      [...existing, ...incoming].map((name) => name.trim()).filter(Boolean)
    ),
  ]
}
