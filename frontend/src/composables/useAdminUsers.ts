import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/console'
import { proofOptions, type SecurityOperation } from '@/api/security'
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
    group = ref(''),
    role = ref(''),
    status = ref(''),
    sortBy = ref('id'),
    sortOrder = ref('desc'),
    loading = ref(false),
    initialError = ref(''),
    busy = ref(false)
  const operatorLevel = computed(() => auth.user?.role ?? 0)
  const canManage = (user: AdminUser) =>
    auth.isRoot || operatorLevel.value > user.role
  async function authorize(scope: string, context: Record<string, unknown>) {
    return proofOptions(await requestProof({ scope, context }))
  }
  async function load() {
    loading.value = true
    initialError.value = ''
    const result = await request.run((signal) =>
      api.get<{ items: AdminUser[]; total: number }>(
        keyword.value || group.value || role.value || status.value
          ? '/api/user/search'
          : '/api/user/',
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: keyword.value,
          group: group.value,
          role: role.value || undefined,
          status: status.value || undefined,
          sort_by: sortBy.value,
          sort_order: sortOrder.value,
        },
        { signal }
      )
    )
    if (result.stale) return
    loading.value = false
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
  async function manage(user: AdminUser, action: string) {
    return mutate(async () =>
      api.post(
        '/api/user/manage',
        { id: user.id, action },
        await authorize('admin.user.manage', { user_id: user.id, action })
      )
    )
  }
  const toggleStatus = (user: AdminUser) =>
    manage(user, user.status === 1 ? 'disable' : 'enable')
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
  async function adjustQuota(user: AdminUser, delta: string) {
    const value = Number(delta)
    if (!Number.isSafeInteger(value) || !value) return false
    return mutate(() =>
      api.post('/api/user/manage', {
        id: user.id,
        action: 'add_quota',
        mode: value > 0 ? 'add' : 'subtract',
        value: Math.abs(value),
      })
    )
  }
  watch([page, pageSize], load)
  watch([keyword, group, role, status, sortBy, sortOrder], () => {
    if (page.value !== 1) page.value = 1
    else void load()
  })
  onMounted(load)
  return {
    rows,
    total,
    page,
    pageSize,
    keyword,
    group,
    role,
    status,
    sortBy,
    sortOrder,
    loading,
    initialError,
    busy,
    operatorLevel,
    canManage,
    load,
    toggleStatus,
    createUser,
    updateUserDetails,
    deleteUser,
    adjustQuota,
  }
}
