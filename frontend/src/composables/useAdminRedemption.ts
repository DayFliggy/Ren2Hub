import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { decimalToQuota, quotaToDecimal } from '@/utils/quota'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useFeatureAccess } from '@/composables/useFeatureAccess'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import type {
  AdminRedemptionCode,
  AdminRedemptionCreateInput,
  AdminRedemptionSortBy,
  AdminRedemptionSortOrder,
} from '@/types/console'

interface NativeRedemption {
  id: number
  name: string
  key: string
  status: number
  quota: number
  used_user_id: number
  created_time: number
  redeemed_time: number
  expired_time: number
}
export function projectRedemption(raw: NativeRedemption): AdminRedemptionCode {
  return {
    id: raw.id,
    name: raw.name,
    code: raw.key,
    type: 'quota',
    status:
      raw.status === 3
        ? 'used'
        : raw.status === 2
          ? 'disabled'
          : raw.expired_time > 0 && raw.expired_time < Date.now() / 1000
            ? 'expired'
            : 'unused',
    quota: String(raw.quota),
    amount: quotaToDecimal(raw.quota),
    redeemer_id: raw.used_user_id,
    created_time: raw.created_time,
    used_time: raw.redeemed_time,
    expired_time: raw.expired_time === 0 ? -1 : raw.expired_time,
  }
}

type RedemptionRowAction = 'status'
type RedemptionCrudAction = 'create' | 'delete'
type RedemptionBulkAction = 'delete'

export function useAdminRedemption() {
  const { t } = useI18n()
  const toast = useToast()
  const auth = useAuthStore()
  const { readOnly } = useFeatureAccess('admin', 'disabled')

  const rows = ref<AdminRedemptionCode[]>([])
  const total = ref(0)
  const statusCounts = ref<Record<string, number>>({})
  const page = ref(1)
  const pageSize = ref(20)
  const keyword = ref('')
  const statusFilter = ref('')
  const sortBy = ref<AdminRedemptionSortBy>('id')
  const sortOrder = ref<AdminRedemptionSortOrder>('desc')
  const loading = ref(true)
  const refreshing = ref(false)
  const initialError = ref('')
  const busy = ref<Map<number, RedemptionRowAction>>(new Map())
  const crudAction = ref<{
    action: RedemptionCrudAction
    id: number | null
  } | null>(null)
  const bulkAction = ref<RedemptionBulkAction | null>(null)
  const listRequest = useLatestRequest()
  let searchTimer = 0

  const isCrudBusy = computed(() => crudAction.value !== null)
  const isBulkBusy = computed(() => bulkAction.value !== null)
  const canMutate = computed(
    () =>
      !readOnly.value &&
      !loading.value &&
      !refreshing.value &&
      !isCrudBusy.value &&
      !isBulkBusy.value &&
      busy.value.size === 0
  )

  /**
   * Admins own all redemption codes; no per-row authority check needed.
   * Root-only codes would need a separate guard, but the current schema has none.
   */
  const canManage = computed(() => !readOnly.value && auth.isAdmin)

  function isBusy(id: number, action: RedemptionRowAction): boolean {
    return busy.value.get(id) === action
  }

  function isRowBusy(id: number): boolean {
    return busy.value.has(id)
  }

  function isCrudActionBusy(
    action: RedemptionCrudAction,
    id: number | null = null
  ): boolean {
    if (!crudAction.value) return false
    return (
      crudAction.value.action === action &&
      (id === null || crudAction.value.id === id)
    )
  }

  function isBulkActionBusy(action: RedemptionBulkAction): boolean {
    return bulkAction.value === action
  }

  async function load(opts: { background?: boolean } = {}): Promise<void> {
    const background = opts.background === true && rows.value.length > 0
    if (background) refreshing.value = true
    else loading.value = true
    if (rows.value.length === 0) initialError.value = ''

    const trimmed = keyword.value.trim()
    const url =
      trimmed || statusFilter.value
        ? '/api/redemption/search'
        : '/api/redemption/'
    const result = await listRequest.run((signal) =>
      api.get<{ items: NativeRedemption[]; total: number }>(
        url,
        {
          keyword: trimmed || undefined,
          status: (
            { unused: 1, used: 3, disabled: 2, expired: 'expired' } as Record<
              string,
              string | number
            >
          )[statusFilter.value],
          p: page.value,
          page_size: pageSize.value,
        },
        { signal }
      )
    )
    if (result.stale) return

    loading.value = false
    refreshing.value = false
    if (!result.ok) {
      const message =
        result.error instanceof ApiError
          ? result.error.message
          : t('redemption.loadFailed')
      if (rows.value.length === 0) initialError.value = message
      else toast.error(message)
      return
    }

    rows.value = result.value.items.map(projectRedemption)
    total.value = result.value.total

    initialError.value = ''
  }

  function reloadFromFirstPage(): void {
    window.clearTimeout(searchTimer)
    if (page.value === 1) void load()
    else page.value = 1
  }

  watch(keyword, () => {
    window.clearTimeout(searchTimer)
    searchTimer = window.setTimeout(reloadFromFirstPage, 300)
  })
  watch([statusFilter, sortBy, sortOrder], reloadFromFirstPage)
  watch(pageSize, reloadFromFirstPage)
  watch(page, () => void load())

  onMounted(() => void load())
  onBeforeUnmount(() => window.clearTimeout(searchTimer))

  async function toggleStatus(code: AdminRedemptionCode): Promise<void> {
    if (busy.value.has(code.id)) return
    const next = new Map(busy.value)
    next.set(code.id, 'status')
    busy.value = next
    try {
      const updated = projectRedemption(
        await api.put<NativeRedemption>('/api/redemption/?status_only=true', {
          id: code.id,
          status: code.status === 'disabled' ? 1 : 2,
        })
      )
      const index = rows.value.findIndex((c) => c.id === code.id)
      if (index >= 0) rows.value[index] = updated
      toast.success(
        updated.status === 'disabled'
          ? t('redemption.disabled')
          : t('redemption.enabled')
      )
      await load({ background: true })
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err))
    } finally {
      const next2 = new Map(busy.value)
      next2.delete(code.id)
      busy.value = next2
    }
  }

  async function createCodes(
    input: AdminRedemptionCreateInput
  ): Promise<{ codes: string[]; items: AdminRedemptionCode[] } | null> {
    crudAction.value = { action: 'create', id: null }
    try {
      const quota = Number(decimalToQuota(input.amount))
      if (!Number.isSafeInteger(quota)) throw new Error('Invalid quota')
      const codes = await api.post<string[]>('/api/redemption/', {
        name: input.name,
        quota,
        count: input.count,
        expired_time: input.expired_time === -1 ? 0 : input.expired_time,
      })
      const result = { codes, items: [] as AdminRedemptionCode[] }
      toast.success(t('redemption.created'))
      await load({ background: true })
      return result
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err))
      if (
        err instanceof ApiError &&
        Array.isArray(err.data) &&
        err.data.length > 0 &&
        err.data.every((code) => typeof code === 'string')
      ) {
        await load({ background: true })
        return { codes: err.data as string[], items: [] }
      }
      return null
    } finally {
      crudAction.value = null
    }
  }

  async function deleteCode(code: AdminRedemptionCode): Promise<boolean> {
    crudAction.value = { action: 'delete', id: code.id }
    try {
      await api.delete(`/api/redemption/${code.id}`)
      toast.success(t('redemption.deleted'))
      if (rows.value.length === 1 && page.value > 1) page.value -= 1
      else await load({ background: true })
      return true
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err))
      return false
    } finally {
      crudAction.value = null
    }
  }

  async function deleteSelected(
    codes: AdminRedemptionCode[]
  ): Promise<boolean> {
    if (codes.length === 0) return false
    bulkAction.value = 'delete'
    try {
      const deleted = await api.post<number>('/api/redemption/batch', {
        ids: codes.map((c) => c.id),
      })
      toast.success(
        t('redemption.bulkDeleted', { count: deleted ?? codes.length })
      )
      const deletedIds = new Set(codes.map((code) => code.id))
      const clearsPage = rows.value.every((code) => deletedIds.has(code.id))
      if (clearsPage && page.value > 1) page.value -= 1
      else await load({ background: true })
      return true
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err))
      return false
    } finally {
      bulkAction.value = null
    }
  }

  return {
    rows,
    total,
    statusCounts,
    page,
    pageSize,
    keyword,
    statusFilter,
    sortBy,
    sortOrder,
    loading,
    refreshing,
    initialError,
    isCrudBusy,
    isBulkBusy,
    canMutate,
    canManage,
    load,
    isBusy,
    isRowBusy,
    isCrudActionBusy,
    isBulkActionBusy,
    toggleStatus,
    createCodes,
    deleteCode,
    deleteSelected,
  }
}
