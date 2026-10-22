import { computed, ref } from 'vue'
import { getModelCatalog, type CatalogChannel } from '@/api/modelCatalog'
import { labDisplayName } from '@/constants/adminChannels'
import { useLatestRequest } from './useLatestRequest'
export interface RouteGroupList {
  groupSlug: string
  groupName: string
  channels: CatalogChannel[]
  activeCount: number
}
export function buildRouteGroupList(
  channels: CatalogChannel[]
): RouteGroupList[] {
  const groups = new Map<string, RouteGroupList>()
  for (const channel of channels) {
    const group = groups.get(channel.lab.id) ?? {
      groupSlug: channel.lab.id,
      groupName: labDisplayName(channel.lab),
      channels: [],
      activeCount: 0,
    }
    group.channels.push(channel)
    if (channel.eligible) group.activeCount++
    groups.set(channel.lab.id, group)
  }
  return [...groups.values()].sort((a, b) =>
    a.groupName.localeCompare(b.groupName)
  )
}
export function useAutoRoute() {
  const loading = ref(false),
    error = ref(''),
    lastUpdated = ref<Date | null>(null),
    raw = ref<CatalogChannel[]>([]),
    request = useLatestRequest()
  const vendorList = computed(() => buildRouteGroupList(raw.value))
  async function load() {
    loading.value = true
    error.value = ''
    const result = await request.run((signal) =>
      getModelCatalog(undefined, signal)
    )
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      error.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
      return
    }
    raw.value = result.value.channels
    lastUpdated.value = new Date(result.value.generated_at * 1000)
  }
  return { loading, error, lastUpdated, vendorList, load }
}
