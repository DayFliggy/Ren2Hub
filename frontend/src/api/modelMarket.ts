import { api } from './client'
import { invalidResponse, isRecord, requiredString } from './contracts'
import { parseUserModels } from './liveContracts'
import { publicCatalogApi } from './publicCatalog'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import type { MarketGroup, ModelMarketCatalog } from '@/types/modelMarket'

export function parseMarketGroups(value: unknown): MarketGroup[] {
  const endpoint = '/api/user/self/groups'
  if (!isRecord(value)) invalidResponse(endpoint)
  return Object.entries(value).map(([name, item]) => {
    if (!name || !isRecord(item)) invalidResponse(endpoint)
    const ratio = item.ratio
    if (
      name === 'auto'
        ? ratio !== 'auto'
        : !(typeof ratio === 'number' && Number.isFinite(ratio) && ratio >= 0)
    )
      invalidResponse(endpoint)
    return {
      name,
      description: requiredString(item.desc, endpoint),
      ratio: ratio as number | 'auto',
      models: [],
    }
  })
}

/** Every column is a user-selectable group, never a synthetic channel offer. */
export async function getModelMarket(
  signal?: AbortSignal
): Promise<ModelMarketCatalog> {
  const [pricing, rawGroups] = await Promise.all([
    publicCatalogApi.pricing(signal),
    api.get<unknown>('/api/user/self/groups', undefined, { signal }),
  ])
  const groups = parseMarketGroups(rawGroups)
  await Promise.all(
    groups.map(async (group) => {
      group.models = parseUserModels(
        await api.get<unknown>(
          '/api/user/models',
          { group: group.name },
          { signal }
        )
      )
    })
  )
  return buildModelMarketCatalog(pricing, groups)
}
