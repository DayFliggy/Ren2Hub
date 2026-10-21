import { api } from './client'
import { parseToken } from './liveContracts'
import { getModelCatalog } from './modelCatalog'
import {
  ClientConfigError,
  type ClientConfigCatalog,
  type ClientConfigFormat,
  type PiModelSettings,
} from '@/types/clientConfig'
import { buildClientConfig, clientConfigBaseURL } from '@/utils/clientConfig'

const endpointTypes = { codex: 'openai-response', pi: 'openai' } as const

async function checkToken(id: number, signal?: AbortSignal) {
  const path = `/api/token/${id}`
  const token = parseToken(await api.get(path, undefined, { signal }), path)
  if (
    token.status !== 1 ||
    (!token.unlimited && token.remain_quota <= 0) ||
    (token.expired_time !== -1 && token.expired_time <= Date.now() / 1000)
  )
    throw new ClientConfigError('token_unavailable')
}

export async function loadClientConfigCatalog(
  tokenId: number,
  signal?: AbortSignal
): Promise<ClientConfigCatalog> {
  const [, catalog] = await Promise.all([
    checkToken(tokenId, signal),
    getModelCatalog(tokenId, signal),
  ])
  const models = catalog.models.map((model) => model.model_name)
  if (!models.length) throw new ClientConfigError('no_models')
  return {
    baseURL: catalog.server_address,
    models,
    protocolModels: {
      codex: catalog.models
        .filter((model) =>
          model.supported_endpoint_types.includes(endpointTypes.codex)
        )
        .map((model) => model.model_name),
      pi: catalog.models
        .filter((model) =>
          model.supported_endpoint_types.includes(endpointTypes.pi)
        )
        .map((model) => model.model_name),
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
  const [, catalog] = await Promise.all([
    checkToken(tokenId, signal),
    getModelCatalog(tokenId, signal),
  ])
  const entry = catalog.models.find((item) => item.model_name === model)
  if (
    !entry?.supported_endpoint_types.includes(endpointTypes[format]) ||
    !catalog.offers.some(
      (offer) =>
        offer.model_id === model &&
        offer.supported_endpoint_types.includes(endpointTypes[format])
    )
  )
    throw new ClientConfigError('model_unavailable')
  return buildClientConfig(format, base, model, pi)
}
