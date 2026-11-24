import { ref, watch, type Ref } from 'vue'

import { upstreamsApi, type UpstreamBinding } from '@/api/upstreams'
import { useLatestRequest } from '@/composables/useLatestRequest'
import type { AdminChannel } from '@/types/console'

// The bindings endpoint accepts at most 500 channel IDs per request.
const BATCH = 500

/**
 * Loads upstream bindings for the visible channels when the operator may read
 * upstreams. Failures leave the channel table usable and clear the badges, so
 * a stale binding never keeps the ratio field locked.
 */
export function useChannelUpstreamBindings(
  rows: Ref<AdminChannel[]>,
  canRead: Ref<boolean>
) {
  const bindings = ref<Record<number, UpstreamBinding>>({})
  const error = ref<unknown>(null)
  const request = useLatestRequest()
  let loadSequence = 0

  async function reload() {
    const sequence = ++loadSequence
    const ids = [...new Set(rows.value.map((channel) => channel.id))]
    if (!canRead.value || !ids.length) {
      request.cancel()
      bindings.value = {}
      error.value = null
      return
    }
    const result = await request.run(async (signal) => {
      const batches: Promise<UpstreamBinding[]>[] = []
      for (let start = 0; start < ids.length; start += BATCH)
        batches.push(
          upstreamsApi.bindingsForChannels(
            ids.slice(start, start + BATCH),
            signal
          )
        )
      return (await Promise.all(batches)).flat()
    })
    if (result.stale || sequence !== loadSequence) return
    if (!result.ok) {
      bindings.value = {}
      error.value = result.error
      return
    }
    error.value = null
    bindings.value = Object.fromEntries(
      result.value.map((binding) => [binding.channel_id, binding])
    )
  }

  watch([rows, canRead], () => void reload(), { immediate: true })

  return { bindings, error, reload }
}
