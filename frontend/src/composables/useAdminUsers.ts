import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import { parseAdminUserPage } from '@/api/adminUserContracts'
import { proofOptions, type SecurityOperation } from '@/api/security'
import {
  adminOperator,
  adminUserStatusAction,
  canManageAdminUser,
  type AdminUserAction,
} from '@/constants/adminUsers'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from './useLatestRequest'
import { useToast } from './useToast'
import type {
  AdminUser,
  AdminUserCreateInput,
  AdminUserUpdateInput,
} from '@/types/console'
const BULK_DONE_KEYS = {
  enable: 'users.bulkEnabled',
  disable: 'users.bulkDisabled',
  delete: 'users.bulkDeleted',
} as const

export function useAdminUsers(
  requestProof: (operation: SecurityOperation) => Promise<string>
) {
  const auth = useAuthStore(),
    toast = useToast(),
    { t } = useI18n(),
    request = useLatestRequest()
  const rows = ref<AdminUser[]>([]),
    total = ref(0),
    page = ref(1),
    pageSize = ref(20),
    keyword = ref(''),
    role = ref(''),
    status = ref(''),
    sortBy = ref('id'),
    sortOrder = ref('desc'),
    loading = ref(false),
    initialError = ref(''),
    busy = ref(false)
  const operator = computed(() => adminOperator(auth))
  const isSelf = (user: AdminUser) => auth.user?.id === user.id
  const canManage = (user: AdminUser, action: AdminUserAction = 'edit') =>
    canManageAdminUser(user, operator.value, action)
  // User administration is AdminAuth-only upstream; Casbin has no user resource.
  const canMutate = computed(() => !busy.value && auth.isAdmin)
  const bulkAction = ref('')
  const bulkProgress = ref<{ done: number; total: number } | null>(null)
  const activeAction = ref('')
  const activeUser = ref<number | null>(null)
  const refreshing = ref(false)
  const isCrudBusy = computed(() => busy.value && !bulkAction.value)
  const isBulkBusy = computed(() => !!bulkAction.value)
  const isBusy = (_id?: number, _action?: string) => busy.value
  const isRowBusy = (id: number) =>
    busy.value && (activeUser.value === id || !!bulkAction.value)
  const isCrudActionBusy = (action: string, _id?: number) =>
    busy.value && activeAction.value === action
  const isBulkActionBusy = (action: string) => bulkAction.value === action
  async function authorize(scope: string, context: Record<string, unknown>) {
    return proofOptions(await requestProof({ scope, context }))
  }
  async function load(options: { background?: boolean } = {}) {
    refreshing.value = !!options.background && rows.value.length > 0
    loading.value = !refreshing.value
    initialError.value = ''
    const result = await request.run((signal) =>
      api
        .get<unknown>(
          keyword.value || role.value || status.value
            ? '/api/user/search'
            : '/api/user/',
          {
            p: page.value,
            page_size: pageSize.value,
            keyword: keyword.value,
            role: role.value || undefined,
            status: status.value || undefined,
            sort_by: sortBy.value,
            sort_order: sortOrder.value,
          },
          { signal }
        )
        .then(parseAdminUserPage)
    )
    if (result.stale) return
    loading.value = false
    refreshing.value = false
    if (result.ok) {
      rows.value = result.value.items
      total.value = result.value.total
    } else
      initialError.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
  }
  async function mutate(operation: () => Promise<unknown>) {
    if (busy.value) return false
    busy.value = true
    try {
      await operation()
      await load()
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      busy.value = false
    }
  }
  // Native proofs bind one user each, so every write below asks for its own.
  async function manageRequest(
    user: AdminUser,
    action: 'enable' | 'disable' | 'promote' | 'demote'
  ) {
    await api.post(
      '/api/user/manage',
      { id: user.id, action },
      await authorize('admin.user.manage', { user_id: user.id, action })
    )
  }
  async function deleteRequest(user: AdminUser) {
    await api.delete(
      `/api/user/${user.id}`,
      undefined,
      await authorize('admin.user.delete', { user_id: user.id })
    )
  }
  async function manage(user: AdminUser, action: 'enable' | 'disable') {
    if (!canManage(user, action)) return false
    activeUser.value = user.id
    activeAction.value = action
    return mutate(() => manageRequest(user, action))
  }
  const toggleStatus = (user: AdminUser) =>
    manage(user, adminUserStatusAction(user))
  async function createUser(input: AdminUserCreateInput) {
    return mutate(async () =>
      api.post(
        '/api/user/',
        input,
        input.role >= 10
          ? await authorize('admin.user.create', { role: input.role })
          : undefined
      )
    )
  }
  /**
   * Profile and role are two native writes; upstream keeps the role on its own
   * row action. Once the profile PUT succeeds the save is reported as done and
   * the list reloads either way. A role request error is reported without
   * claiming the outcome: the backend can commit the role and then fail.
   */
  async function updateUserDetails(
    user: AdminUser,
    input: AdminUserUpdateInput
  ) {
    if (busy.value) return false
    busy.value = true
    try {
      const { role, ...patch } = input
      try {
        await api.put(
          '/api/user/',
          { id: user.id, ...patch },
          patch.password || patch.admin_permissions
            ? await authorize('admin.user.update', { user_id: user.id })
            : undefined
        )
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
        return false
      }
      if (role !== user.role)
        await manageRequest(user, role >= 10 ? 'promote' : 'demote').catch(
          (e: unknown) =>
            toast.error(
              t('users.profileSavedRoleFailed', {
                message: e instanceof Error ? e.message : String(e),
              })
            )
        )
      await load()
      return true
    } finally {
      busy.value = false
    }
  }
  async function deleteUser(user: AdminUser) {
    return mutate(() => deleteRequest(user))
  }
  async function adjustQuota(
    user: AdminUser,
    delta: string,
    _reason: string,
    operationId: string
  ) {
    const value = Number(delta)
    if (!Number.isSafeInteger(value) || !value) return false
    return mutate(() =>
      api.post('/api/user/manage', {
        id: user.id,
        action: 'add_quota',
        operation_id: operationId,
        mode: value > 0 ? 'add' : 'subtract',
        value: Math.abs(value),
      })
    )
  }
  /**
   * Runs one verified request per user under a single busy lock. Users already
   * in the target state are skipped; the first failure or cancelled
   * verification stops the rest. The list refreshes once, and a refresh
   * failure surfaces as the page load error, not as a user's failure.
   * Resolves true when every target succeeded.
   */
  async function runBulk(
    action: 'enable' | 'disable' | 'delete',
    users: AdminUser[]
  ) {
    if (busy.value) return false
    const targets = users.filter(
      (user) =>
        canManage(user, action) &&
        (action === 'delete' || adminUserStatusAction(user) === action)
    )
    if (targets.length === 0) return true
    busy.value = true
    bulkAction.value = action
    const progress = { done: 0, total: targets.length }
    bulkProgress.value = { ...progress }
    let failure: { user: AdminUser; error: unknown } | null = null
    try {
      for (const user of targets) {
        try {
          if (action === 'delete') await deleteRequest(user)
          else await manageRequest(user, action)
        } catch (error) {
          failure = { user, error }
          break
        }
        progress.done += 1
        bulkProgress.value = { ...progress }
      }
      await load()
    } finally {
      busy.value = false
      bulkAction.value = ''
      bulkProgress.value = null
    }
    if (!failure) {
      toast.success(t(BULK_DONE_KEYS[action], { count: progress.done }))
      return true
    }
    toast.error(
      t('users.bulkStopped', {
        ...progress,
        name: failure.user.username,
        message:
          failure.error instanceof Error
            ? failure.error.message
            : String(failure.error),
      })
    )
    return false
  }
  watch([page, pageSize], () => void load())
  watch([keyword, role, status, sortBy, sortOrder], () => {
    if (page.value !== 1) page.value = 1
    else void load()
  })
  onMounted(() => void load())
  return {
    refreshing,
    isCrudBusy,
    isBulkBusy,
    canMutate,
    isSelf,
    isBusy,
    isRowBusy,
    isCrudActionBusy,
    isBulkActionBusy,
    runBulk,
    bulkProgress,
    rows,
    total,
    page,
    pageSize,
    keyword,
    role,
    status,
    sortBy,
    sortOrder,
    loading,
    initialError,
    busy,
    operator,
    canManage,
    load,
    toggleStatus,
    createUser,
    updateUserDetails,
    deleteUser,
    adjustQuota,
  }
}
