import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

import { announcementsApi, type UserAnnouncement } from '@/api/announcements'
import { ApiError } from '@/api/types'

import { useAuthStore } from './auth'

const REFRESH_THROTTLE_MS = 5 * 60 * 1000
const POLL_INTERVAL_MS = 10 * 60 * 1000

export const useAnnouncementStore = defineStore('announcements', () => {
  const auth = useAuthStore()

  const items = ref<UserAnnouncement[]>([])
  const serverUnreadCount = ref(0)
  const loading = ref(false)
  const error = ref('')
  const popupQueue = ref<UserAnnouncement[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)

  let lastFetchedAt = 0
  let consumers = 0
  let timer = 0
  let fetchGeneration = 0
  let activeRequest: AbortController | null = null
  let shownPopupIds = new Set<number>()
  let dismissingPopup = false

  const unreadCount = computed(() => {
    const localUnread = items.value.filter(
      (item) => item.read_at === null
    ).length
    return Math.max(localUnread, serverUnreadCount.value)
  })

  function reset() {
    fetchGeneration += 1
    activeRequest?.abort()
    activeRequest = null
    dismissingPopup = false
    items.value = []
    serverUnreadCount.value = 0
    loading.value = false
    error.value = ''
    popupQueue.value = []
    currentPopup.value = null
    lastFetchedAt = 0
    shownPopupIds = new Set()
  }

  watch(
    () => auth.user?.id,
    (userId) => {
      reset()
      if (userId && consumers > 0) {
        void fetchAnnouncements(true)
      }
    }
  )

  function enqueuePopups() {
    const candidates = items.value.filter(
      (item) =>
        item.notify_mode === 'popup' &&
        item.read_at === null &&
        !shownPopupIds.has(item.id)
    )
    if (!candidates.length) return
    for (const candidate of candidates) {
      if (!popupQueue.value.some((queued) => queued.id === candidate.id)) {
        popupQueue.value.push(candidate)
      }
    }
    if (!currentPopup.value) showNextPopup()
  }

  function showNextPopup() {
    const next = popupQueue.value.shift() ?? null
    currentPopup.value = next
    if (next) shownPopupIds.add(next.id)
  }

  async function fetchAnnouncements(force = false): Promise<void> {
    if (!auth.isAuthenticated) {
      reset()
      return
    }
    const now = Date.now()
    if (
      !force &&
      lastFetchedAt > 0 &&
      now - lastFetchedAt < REFRESH_THROTTLE_MS
    ) {
      return
    }
    lastFetchedAt = now
    const generation = ++fetchGeneration
    activeRequest?.abort()
    const controller = new AbortController()
    activeRequest = controller
    loading.value = true
    error.value = ''

    try {
      const response = await announcementsApi.listForUser(
        false,
        controller.signal
      )
      if (generation !== fetchGeneration || controller.signal.aborted) return
      items.value = response.items
      serverUnreadCount.value = response.unread_count
      enqueuePopups()
    } catch (cause) {
      if (generation !== fetchGeneration || controller.signal.aborted) return
      lastFetchedAt = 0
      error.value =
        cause instanceof ApiError
          ? cause.message
          : cause instanceof Error
            ? cause.message
            : String(cause)
    } finally {
      if (generation === fetchGeneration) {
        loading.value = false
        if (activeRequest === controller) activeRequest = null
      }
    }
  }

  async function markAsRead(id: number): Promise<boolean> {
    const generation = fetchGeneration
    const target = items.value.find((item) => item.id === id)
    if (target && target.read_at !== null) {
      if (currentPopup.value?.id === id) {
        currentPopup.value = null
        showNextPopup()
      }
      popupQueue.value = popupQueue.value.filter((item) => item.id !== id)
      return true
    }
    try {
      const result = await announcementsApi.markRead(id)
      if (generation !== fetchGeneration) return false
      const current = items.value.find((item) => item.id === id)
      if (current && current.read_at === null) {
        current.read_at = result.read_at
        serverUnreadCount.value = Math.max(0, serverUnreadCount.value - 1)
      }
      if (currentPopup.value?.id === id) {
        currentPopup.value = null
        showNextPopup()
      }
      popupQueue.value = popupQueue.value.filter((item) => item.id !== id)
      return true
    } catch {
      return false
    }
  }

  async function markAllAsRead(): Promise<boolean> {
    const generation = fetchGeneration
    if (unreadCount.value === 0) return true
    try {
      await announcementsApi.markAllRead()
      if (generation !== fetchGeneration) return false
      const nowSec = Math.floor(Date.now() / 1000)
      for (const item of items.value) {
        if (item.read_at === null) item.read_at = nowSec
      }
      serverUnreadCount.value = 0
      popupQueue.value = []
      currentPopup.value = null
      return true
    } catch {
      return false
    }
  }

  async function dismissPopup(): Promise<boolean> {
    const active = currentPopup.value
    if (!active || dismissingPopup) return false
    const generation = fetchGeneration
    dismissingPopup = true
    try {
      const ok = await markAsRead(active.id)
      if (generation !== fetchGeneration) return false
      if (!ok) return false
      if (currentPopup.value?.id === active.id) {
        currentPopup.value = null
        showNextPopup()
      }
      return true
    } finally {
      if (generation === fetchGeneration) {
        dismissingPopup = false
      }
    }
  }

  function onFocus() {
    void fetchAnnouncements(false)
  }

  function start() {
    consumers += 1
    if (consumers > 1) return
    if (auth.isAuthenticated) {
      void fetchAnnouncements(true)
    }
    timer = window.setInterval(
      () => void fetchAnnouncements(false),
      POLL_INTERVAL_MS
    )
    window.addEventListener('focus', onFocus)
  }

  function stop() {
    consumers = Math.max(0, consumers - 1)
    if (consumers > 0) return
    window.clearInterval(timer)
    timer = 0
    window.removeEventListener('focus', onFocus)
    activeRequest?.abort()
    activeRequest = null
  }

  return {
    items,
    unreadCount,
    loading,
    error,
    currentPopup,
    fetchAnnouncements,
    markAsRead,
    markAllAsRead,
    dismissPopup,
    reset,
    start,
    stop,
  }
})
