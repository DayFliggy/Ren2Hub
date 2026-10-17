import { api } from './client'
import { parseToken } from './liveContracts'
import { publicApi } from './public'
import { tokenRoutingApi } from './tokenRouting'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredString,
} from './contracts'
import {
  ClientConfigError,
  type ClientConfigCatalog,
  type ClientConfigFormat,
  type PiModelSettings,
} from '@/types/clientConfig'
import { buildClientConfig, clientConfigBaseURL } from '@/utils/clientConfig'

const endpointTypes = { codex: 'openai-response', pi: 'openai' } as const

async function loadProtocolFacts(signal?: AbortSignal) {
  const path = '/api/pricing'
  const value = await api.get<unknown>(path, undefined, { signal })
  if (!Array.isArray(value)) invalidResponse(path)
  // Read only published protocol facts. Pricing ratios cannot establish
  // context size, reasoning, input modalities, or client cost estimates.
  return new Map(
    value.map((item) => {
      if (!isRecord(item)) invalidResponse(path)
      return [
        requiredString(item.model_name, path, false),
        parseStringArray(item.supported_endpoint_types, path),
      ]
    })
  )
}

async function checkToken(id: number, signal?: AbortSignal) {
  const path = `/api/token/${id}`
  const token = parseToken(await api.get(path, undefined, { signal }), path)
  if (
    token.status !== 1 ||
    (token.expired_time !== -1 && token.expired_time <= Date.now() / 1000)
  )
    throw new ClientConfigError('token_unavailable')
}

export async function loadClientConfigCatalog(
  tokenId: number,
  signal?: AbortSignal
): Promise<ClientConfigCatalog> {
  const [, status, channels, protocols] = await Promise.all([
    checkToken(tokenId, signal),
    publicApi.status(signal),
    tokenRoutingApi.channels(tokenId, signal),
    loadProtocolFacts(signal),
  ])
  // Combine token-authorized candidates with explicitly published protocols.
  // Generation rechecks those facts and current routing eligibility.
  const models = [...new Set(channels.flatMap((channel) => channel.models))]
    .filter((model) => Boolean(model.trim()))
    .sort()
  if (!models.length) throw new ClientConfigError('no_models')
  return {
    baseURL: status.server_address ?? '',
    models,
    protocolModels: {
      codex: models.filter((model) =>
        protocols.get(model)?.includes(endpointTypes.codex)
      ),
      pi: models.filter((model) =>
        protocols.get(model)?.includes(endpointTypes.pi)
      ),
    },
  }
}

export async function getTokenClientConfig(
  tokenId: number,
  format: ClientConfigFormat,
  model: string,
  baseURL: string,
  pi?: PiModelSettings,
  signal?: AbortSignal
) {
  const base = clientConfigBaseURL(baseURL)
  const [, protocols] = await Promise.all([
    checkToken(tokenId, signal),
    loadProtocolFacts(signal),
  ])
  if (!protocols.get(model)?.includes(endpointTypes[format]))
    throw new ClientConfigError('model_unavailable')
  const path = format === 'codex' ? '/v1/responses' : '/v1/chat/completions'
  const preview = await tokenRoutingApi.preview(tokenId, model, path, signal)
  if (
    preview.model !== model ||
    preview.path !== path ||
    !preview.entries.some((entry) => entry.eligible)
  )
    throw new ClientConfigError('model_unavailable')
  return buildClientConfig(format, base, model, pi)
}
