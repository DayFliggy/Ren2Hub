import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

import {
  TOP_UP_RANKING_LIMIT,
  getTopUps,
  parseTopUpRanking,
  parseTopUpStatistics,
} from '@/api/adminOrders'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { ADMIN_ORDER_DEFAULT_RANGE } from '@/constants/adminOrders'
import type {
  AdminOrder,
  AdminOrderRange,
  AdminOrderSpender,
  AdminOrderStats,
} from '@/types/console'

const EXPORT_PAGE_SIZE = 100

export const ORDER_EXPORT_MAX_ROWS = 10_000

/** Read-only recharge ledger backed by native TopUp endpoints. */
export function useAdminOrders() {
  const toast = useToast()

  const rows = ref<AdminOrder[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const keyword = ref('')
  const status = ref('')
  const method = ref('')
  const loading = ref(true)
  const refreshing = ref(false)
  const initialError = ref('')

  const stats = ref<AdminOrderStats | null>(null)
  const range = ref<AdminOrderRange>(ADMIN_ORDER_DEFAULT_RANGE)
  const statsLoading = ref(true)
  const statsRefreshing = ref(false)
  const statsError = ref('')
  const statsStale = ref(false)
  // The ranking is a separate admin read; its failure must not hide the stats.
  const ranking = ref<AdminOrderSpender[] | null>(null)
  const rankingError = ref('')

  let listController: AbortController | null = null
  let statsController: AbortController | null = null
  let listSequence = 0
  let statsSequence = 0
  let statsRange: AdminOrderRange | null = null
  let searchTimer = 0

  function filterParams(): Record<string, string | undefined> {
    const trimmed = keyword.value.trim()
    return {
      status:
        status.value === 'completed' ? 'success' : status.value || undefined,
      method: method.value || undefined,
      keyword: trimmed || undefined,
    }
  }

  async function load(options: { background?: boolean } = {}) {
    listController?.abort()
    const controller = new AbortController()
    listController = controller
    const sequence = ++listSequence
    const background = options.background === true && rows.value.length > 0

    if (background) refreshing.value = true
    else loading.value = true
    if (rows.value.length === 0) initialError.value = ''

    try {
      const data = await getTopUps(
        { ...filterParams(), p: page.value, page_size: pageSize.value },
        controller.signal
      )
      if (sequence !== listSequence) return
      rows.value = data.items
      total.value = data.total
      initialError.value = ''
    } catch (error) {
      if (controller.signal.aborted || sequence !== listSequence) return
      const message = error instanceof ApiError ? error.message : String(error)
      if (rows.value.length === 0) initialError.value = message
      else toast.error(message)
    } finally {
      if (sequence === listSequence) {
        loading.value = false
        refreshing.value = false
      }
    }
  }

  async function loadStats(options: { background?: boolean } = {}) {
    statsController?.abort()
    const controller = new AbortController()
    statsController = controller
    const sequence = ++statsSequence
    const requestedRange = range.value
    if (statsRange !== requestedRange) {
      stats.value = null
      ranking.value = null
      rankingError.value = ''
      statsRange = null
      statsStale.value = false
      statsError.value = ''
    }
    const background = options.background === true && stats.value !== null

    statsRefreshing.value = background
    statsLoading.value = !background
    if (!stats.value) statsError.value = ''

    const endTime = Math.floor(Date.now() / 1000)
    const window = {
      start_time: endTime - requestedRange * 86400,
      end_time: endTime,
    }
    const rankingRequest = api
      .get<unknown>(
        '/api/user/topup/ranking',
        { ...window, limit: TOP_UP_RANKING_LIMIT },
        { signal: controller.signal }
      )
      .then((data) => ({ items: parseTopUpRanking(data), error: '' }))
      .catch((error: unknown) => ({
        items: null,
        error: error instanceof ApiError ? error.message : String(error),
      }))
    try {
      const data = await api.get<unknown>('/api/user/topup/stats', window, {
        signal: controller.signal,
      })
      if (controller.signal.aborted || sequence !== statsSequence) return
      stats.value = parseTopUpStatistics(data)
      const rankingResult = await rankingRequest
      if (controller.signal.aborted || sequence !== statsSequence) return
      ranking.value = rankingResult.items
      rankingError.value = rankingResult.error
      statsRange = requestedRange
      statsError.value = ''
      statsStale.value = false
    } catch (error) {
      if (controller.signal.aborted || sequence !== statsSequence) return
      if (
        error instanceof ApiError &&
        (error.status === 401 || error.status === 403)
      ) {
        stats.value = null
        ranking.value = null
        statsRange = null
      }
      statsError.value =
        error instanceof ApiError ? error.message : String(error)
      statsStale.value = stats.value !== null
    } finally {
      if (sequence === statsSequence) {
        statsLoading.value = false
        statsRefreshing.value = false
      }
    }
  }

  function reloadFromFirstPage() {
    window.clearTimeout(searchTimer)
    if (page.value === 1) void load()
    else page.value = 1
  }

  watch(keyword, () => {
    window.clearTimeout(searchTimer)
    searchTimer = window.setTimeout(reloadFromFirstPage, 300)
  })
  watch([status, method], reloadFromFirstPage)
  watch(pageSize, reloadFromFirstPage)
  watch(page, () => void load())
  watch(range, () => void loadStats(), { flush: 'sync' })

  async function fetchAllForExport(
    signal: AbortSignal
  ): Promise<{ items: AdminOrder[]; truncated: boolean }> {
    const filters = filterParams()
    const first = await getTopUps(
      { ...filters, p: 1, page_size: EXPORT_PAGE_SIZE },
      signal
    )

    const items = [...first.items]
    const reachable = Math.min(first.total, ORDER_EXPORT_MAX_ROWS)
    const pages = Math.ceil(reachable / EXPORT_PAGE_SIZE)

    for (let p = 2; p <= pages; p++) {
      const next = await getTopUps(
        { ...filters, p, page_size: EXPORT_PAGE_SIZE },
        signal
      )
      items.push(...next.items)
    }

    return {
      items: items.slice(0, ORDER_EXPORT_MAX_ROWS),
      truncated: first.total > ORDER_EXPORT_MAX_ROWS,
    }
  }

  function refreshAll() {
    void load({ background: true })
    void loadStats({ background: true })
  }

  onMounted(() => {
    void load()
    void loadStats()
  })

  onBeforeUnmount(() => {
    listController?.abort()
    statsController?.abort()
    window.clearTimeout(searchTimer)
  })

  return {
    rows,
    total,
    page,
    pageSize,
    keyword,
    status,
    method,
    loading,
    refreshing,
    initialError,
    stats,
    range,
    statsLoading,
    statsRefreshing,
    statsError,
    statsStale,
    ranking,
    rankingError,
    load,
    loadStats,
    fetchAllForExport,
    refreshAll,
  }
}
