import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'

export interface ModelCapabilities {
  reasoning_efforts: string[] | null
  input_modalities: string[] | null
  output_modalities: string[] | null
  context_limit: number | null
  output_limit: number | null
  tool_call: boolean | null
  source: string | null
  updated_at: number
}

export interface ModelManifestEntry {
  id: string
  visible: boolean
  supported_endpoint_types: string[]
  capabilities: ModelCapabilities | null
  price_configured: boolean
  price_version: string
}

export interface ModelManifest {
  schema_version: number
  models: ModelManifestEntry[]
}

export interface ModelClientConfig {
  format: 'codex' | 'pi'
  filename: string
  media_type: string
  content: string
  models: string[]
  diagnostics: { model?: string; code: string }[]
}

export function parseModelCapabilities(
  value: unknown,
  endpoint: string
): ModelCapabilities | null {
  if (value == null) return null
  if (!isRecord(value)) invalidResponse(endpoint)
  const list = (key: string) =>
    value[key] == null ? null : parseStringArray(value[key], endpoint)
  const positive = (key: string) => {
    if (value[key] == null) return null
    const result = requiredStrictInteger(value[key], endpoint)
    if (result <= 0) invalidResponse(endpoint)
    return result
  }
  return {
    reasoning_efforts: list('reasoning_efforts'),
    input_modalities: list('input_modalities'),
    output_modalities: list('output_modalities'),
    context_limit: positive('context_limit'),
    output_limit: positive('output_limit'),
    tool_call:
      value.tool_call == null
        ? null
        : requiredBoolean(value.tool_call, endpoint),
    source:
      value.source == null ? null : requiredString(value.source, endpoint),
    updated_at:
      value.updated_at == null
        ? 0
        : requiredStrictInteger(value.updated_at, endpoint),
  }
}

export function parseModelManifest(value: unknown): ModelManifest {
  const endpoint = '/api/user/model-manifest'
  if (
    !isRecord(value) ||
    !Array.isArray(value.models) ||
    value.schema_version !== 1
  )
    invalidResponse(endpoint)
  return {
    schema_version: 1,
    models: value.models.map((entry) => {
      if (!isRecord(entry)) invalidResponse(endpoint)
      return {
        id: requiredString(entry.id, endpoint, false),
        visible: requiredBoolean(entry.visible, endpoint),
        supported_endpoint_types: parseStringArray(
          entry.supported_endpoint_types,
          endpoint
        ),
        capabilities: parseModelCapabilities(entry.capabilities, endpoint),
        price_configured: requiredBoolean(entry.price_configured, endpoint),
        price_version: requiredString(entry.price_version, endpoint),
      }
    }),
  }
}

export function parseModelClientConfig(value: unknown): ModelClientConfig {
  const endpoint = '/api/token/:id/client-config'
  if (
    !isRecord(value) ||
    (value.format !== 'codex' && value.format !== 'pi') ||
    !Array.isArray(value.diagnostics)
  )
    invalidResponse(endpoint)
  const filename = requiredString(value.filename, endpoint, false)
  if (!/^[a-zA-Z0-9._-]+$/.test(filename)) invalidResponse(endpoint)
  return {
    format: value.format,
    filename,
    media_type: requiredString(value.media_type, endpoint, false),
    content: requiredString(value.content, endpoint, false),
    models: parseStringArray(value.models, endpoint),
    diagnostics: value.diagnostics.map((entry) => {
      if (!isRecord(entry)) invalidResponse(endpoint)
      return {
        code: requiredString(entry.code, endpoint, false),
        ...(entry.model == null
          ? {}
          : { model: requiredString(entry.model, endpoint) }),
      }
    }),
  }
}

export async function getModelManifest(
  signal?: AbortSignal,
  tokenId?: number
): Promise<ModelManifest> {
  return parseModelManifest(
    await api.get<unknown>(
      '/api/user/model-manifest',
      tokenId == null ? undefined : { token_id: tokenId },
      { signal }
    )
  )
}

export async function getTokenClientConfig(
  tokenId: number,
  format: ModelClientConfig['format'],
  model?: string,
  signal?: AbortSignal
): Promise<ModelClientConfig> {
  return parseModelClientConfig(
    await api.get<unknown>(
      `/api/token/${tokenId}/client-config`,
      { format, ...(model ? { model } : {}) },
      { signal }
    )
  )
}
