import { computed, onMounted, ref, watch } from 'vue'
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
export function useAdminUsers(
  requestProof: (operation: SecurityOperation) => Promise<string>
) {
  const auth = useAuthStore(),
    toast = useToast(),
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
  async function manage(user: AdminUser, action: 'enable' | 'disable') {
    if (!canManage(user, action)) return false
    activeUser.value = user.id
    activeAction.value = action
    return mutate(async () =>
      api.post(
        '/api/user/manage',
        { id: user.id, action },
        await authorize('admin.user.manage', { user_id: user.id, action })
      )
    )
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
  async function updateUserDetails(
    user: AdminUser,
    input: AdminUserUpdateInput
  ) {
    return mutate(async () => {
      const { role, ...patch } = input
      await api.put(
        '/api/user/',
        { id: user.id, ...patch },
        patch.password || patch.admin_permissions
          ? await authorize('admin.user.update', { user_id: user.id })
          : undefined
      )
      if (role !== user.role) {
        const action = role >= 10 ? 'promote' : 'demote'
        await api.post(
          '/api/user/manage',
          { id: user.id, action },
          await authorize('admin.user.manage', { user_id: user.id, action })
        )
      }
    })
  }
  async function deleteUser(user: AdminUser) {
    return mutate(async () =>
      api.delete(
        `/api/user/${user.id}`,
        undefined,
        await authorize('admin.user.delete', { user_id: user.id })
      )
    )
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
  async function updateUsersStatus(
    action: 'enable' | 'disable',
    users: AdminUser[]
  ) {
    if (busy.value || users.some((user) => !canManage(user, action)))
      return false
    bulkAction.value = action
    try {
      for (const user of users) {
        if (!(await manage(user, action))) return false
      }
      return true
    } finally {
      bulkAction.value = ''
    }
  }
  async function deleteSelectedUsers(users: AdminUser[]) {
    if (busy.value || users.some((user) => !canManage(user, 'delete')))
      return false
    bulkAction.value = 'delete'
    try {
      for (const user of users) {
        if (!(await deleteUser(user))) return false
      }
      return true
    } finally {
      bulkAction.value = ''
    }
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
    updateUsersStatus,
    deleteSelectedUsers,
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
