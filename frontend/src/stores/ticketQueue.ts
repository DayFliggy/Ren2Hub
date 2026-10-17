import { onScopeDispose, ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api/console'
import type { TicketQueueSummary } from '@/types/console'

import { useAuthStore } from './auth'

const REFRESH_INTERVAL_MS = 60_000

export const useTicketQueueStore = defineStore('ticket-queue', () => {
  const auth = useAuthStore()
  const summary = ref<TicketQueueSummary>({
    pending: 0,
    unassigned: 0,
    mine: 0,
  })
  let timer = 0
  let consumers = 0
  let refreshing: Promise<void> | null = null
  let controller: AbortController | null = null

  function cancelRefresh(): void {
    controller?.abort()
    controller = null
    refreshing = null
  }

  async function refresh(): Promise<void> {
    if (!auth.hasPermission('ticket', 'read')) {
      cancelRefresh()
      summary.value = { pending: 0, unassigned: 0, mine: 0 }
      return
    }
    if (refreshing) return refreshing
    const request = new AbortController()
    const userID = auth.user?.id
    controller = request
    refreshing = api
      .get<TicketQueueSummary>('/api/admin/tickets/summary', undefined, {
        signal: request.signal,
      })
      .then((value) => {
        if (
          request.signal.aborted ||
          userID !== auth.user?.id ||
          !auth.hasPermission('ticket', 'read')
        )
          return
        summary.value = value
      })
      .catch((error: unknown) => {
        if (request.signal.aborted) return
        console.error('Failed to refresh ticket queue summary', error)
      })
      .finally(() => {
        if (controller === request) {
          controller = null
          refreshing = null
        }
      })
    return refreshing
  }

  function onFocus(): void {
    void refresh()
  }

  function start(): void {
    consumers += 1
    if (consumers > 1) return
    void refresh()
    timer = window.setInterval(() => void refresh(), REFRESH_INTERVAL_MS)
    window.addEventListener('focus', onFocus)
  }

  function stop(): void {
    consumers = Math.max(0, consumers - 1)
    if (consumers > 0) return
    window.clearInterval(timer)
    timer = 0
    window.removeEventListener('focus', onFocus)
    cancelRefresh()
  }

  watch(
    [() => auth.user?.id, () => auth.hasPermission('ticket', 'read')],
    () => {
      cancelRefresh()
      summary.value = { pending: 0, unassigned: 0, mine: 0 }
      if (consumers > 0) void refresh()
    },
    { flush: 'sync' }
  )
  onScopeDispose(() => {
    consumers = 0
    stop()
  })

  return { summary, refresh, start, stop }
})
