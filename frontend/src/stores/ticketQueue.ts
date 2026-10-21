import { ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { ticketsApi } from '@/api/tickets'
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
  let request: AbortController | null = null

  watch(
    () => [auth.user?.id, auth.hasPermission('ticket', 'read')],
    () => {
      request?.abort()
      request = null
      refreshing = null
      summary.value = { pending: 0, unassigned: 0, mine: 0 }
      if (consumers > 0) void refresh()
    }
  )

  async function refresh(): Promise<void> {
    if (!auth.hasPermission('ticket', 'read')) {
      summary.value = { pending: 0, unassigned: 0, mine: 0 }
      return
    }
    if (refreshing) return refreshing
    const current = new AbortController()
    request = current
    refreshing = ticketsApi
      .summary(current.signal)
      .then((value) => {
        if (!current.signal.aborted && auth.hasPermission('ticket', 'read'))
          summary.value = value
      })
      .catch((error: unknown) => {
        if (!current.signal.aborted)
          console.error('Failed to refresh ticket queue summary', error)
      })
      .finally(() => {
        if (request === current) {
          request = null
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
  }

  return { summary, refresh, start, stop }
})
