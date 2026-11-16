import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  putEnvelope: vi.fn(),
  delete: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  proof: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
vi.mock('@/composables/useFeatureAccess', async () => {
  const { ref } = await import('vue')
  return { useFeatureAccess: () => ({ readOnly: ref(false) }) }
})
import { useAdminUsers } from '../useAdminUsers'
import { useAdminRedemption } from '../useAdminRedemption'
import { ApiError } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import type { AdminUser } from '@/types/console'
import { configureQuotaDisplay } from '@/utils/format'
const member = { id: 2, role: 1, status: 1, username: 'member' } as AdminUser
// authz.Capabilities output: Casbin has no user or billing resource.
const capabilities = {
  audit: { read: true },
  channel: {
    read: true,
    operate: false,
    write: false,
    sensitive_write: false,
    secret_view: false,
  },
  routing: { read: true, write: true },
  task_plugin: { bind: false },
  ticket: { read: true, reply: true, manage: false },
}
function signIn(id: number, role: number) {
  useAuthStore().persist({
    id,
    username: `operator-${id}`,
    display_name: '',
    email: '',
    role,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: capabilities },
  })
}
beforeEach(() => {
  setActivePinia(createPinia())
  signIn(1, 100)
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ items: [], total: 0 })
  mocks.proof.mockResolvedValue('proof')
  mocks.post.mockResolvedValue(null)
  mocks.put.mockResolvedValue(null)
  mocks.putEnvelope.mockResolvedValue({ success: true })
})
function setup<T>(fn: () => T) {
  let state!: T
  const wrapper = mount(
    defineComponent({
      setup() {
        state = fn()
        return () => null
      },
    }),
    {
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            missingWarn: false,
            fallbackWarn: false,
          }),
        ],
      },
    }
  )
  return { wrapper, state }
}
it('lets a role-10 admin manage common users without a user permission entry', async () => {
  signIn(5, 10)
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  const peer = { ...member, id: 6, role: 10 } as AdminUser
  const self = { ...member, id: 5, role: 10 } as AdminUser
  expect(state.canMutate.value).toBe(true)
  expect(state.canManage(member)).toBe(true)
  expect(state.canManage(member, 'delete')).toBe(true)
  expect(state.canManage(peer)).toBe(false)
  expect(state.canManage(self)).toBe(false)
  expect(await state.toggleStatus(member)).toBe(true)
  expect(mocks.post).toHaveBeenCalledWith(
    '/api/user/manage',
    { id: 2, action: 'disable' },
    { headers: { 'X-Security-Proof': 'proof' } }
  )
  wrapper.unmount()
})
it('lets root manage admins and edit, but not disable or delete, another root', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  const admin = { ...member, id: 6, role: 10 } as AdminUser
  const otherRoot = { ...member, id: 7, role: 100 } as AdminUser
  expect(state.canManage(admin, 'delete')).toBe(true)
  expect(state.canManage(otherRoot)).toBe(true)
  expect(state.canManage(otherRoot, 'disable')).toBe(false)
  expect(state.canManage(otherRoot, 'delete')).toBe(false)
  expect(await state.toggleStatus(otherRoot)).toBe(false)
  expect(mocks.post).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('uses native integer quota adjustment and rejects nonintegral quota', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  // controller/user_quota.go answers the committed snapshot and its bonus.
  mocks.post.mockResolvedValueOnce({
    replayed: false,
    before: 1000000,
    after: 500000,
    bonus_quota: 0,
  })
  expect(
    await state.adjustQuota(member, '-500000', '', 'quota-operation')
  ).toBe(true)
  expect(mocks.post).toHaveBeenCalledWith('/api/user/manage', {
    id: 2,
    action: 'add_quota',
    operation_id: 'quota-operation',
    mode: 'subtract',
    value: 500000,
  })
  expect(await state.adjustQuota(member, '0.5', '', 'invalid-operation')).toBe(
    false
  )
  expect(mocks.post).toHaveBeenCalledTimes(1)
  wrapper.unmount()
})
it('reports the bonus of an addition and rejects a result without one', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  mocks.post.mockResolvedValueOnce({
    replayed: false,
    before: 0,
    after: 550000,
    bonus_quota: 50000,
  })
  expect(await state.adjustQuota(member, '500000', '', 'bonus-operation')).toBe(
    true
  )
  expect(mocks.post).toHaveBeenLastCalledWith('/api/user/manage', {
    id: 2,
    action: 'add_quota',
    operation_id: 'bonus-operation',
    mode: 'add',
    value: 500000,
  })
  mocks.post.mockResolvedValueOnce({ replayed: false, before: 0, after: 1 })
  expect(await state.adjustQuota(member, '1', '', 'malformed-operation')).toBe(
    false
  )
  wrapper.unmount()
})
it('converts redemption amounts using a fractional runtime quota unit', async () => {
  configureQuotaDisplay({ quota_per_unit: 1.25 })
  const { wrapper, state } = setup(useAdminRedemption)
  try {
    await flushPromises()
    mocks.post.mockResolvedValue(['redemption-code'])
    expect(
      await state.createCodes({
        name: 'Runtime unit',
        type: 'quota',
        amount: '4',
        count: 1,
        expired_time: -1,
      })
    ).toMatchObject({ codes: ['redemption-code'] })
    expect(mocks.post).toHaveBeenCalledWith('/api/redemption/', {
      name: 'Runtime unit',
      quota: 5,
      count: 1,
      expired_time: 0,
    })
  } finally {
    wrapper.unmount()
    configureQuotaDisplay({ quota_per_unit: 500_000 })
  }
})

it('binds sensitive user changes to native verification contexts and separates role management', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  await state.updateUserDetails(member, {
    username: 'member',
    display_name: 'Member',
    remark: 'Billing contact',
    password: 'new-password',
    role: 10,
    max_concurrency: 0,
  })
  expect(mocks.proof).toHaveBeenNthCalledWith(1, {
    scope: 'admin.user.update',
    context: { user_id: 2 },
  })
  expect(mocks.proof).toHaveBeenNthCalledWith(2, {
    scope: 'admin.user.manage',
    context: { user_id: 2, action: 'promote' },
  })
  expect(mocks.putEnvelope.mock.calls[0]![1]).toEqual({
    id: 2,
    username: 'member',
    display_name: 'Member',
    remark: 'Billing contact',
    password: 'new-password',
    max_concurrency: 0,
  })
  expect(mocks.post).toHaveBeenCalledWith(
    '/api/user/manage',
    { id: 2, action: 'promote' },
    { headers: { 'X-Security-Proof': 'proof' } }
  )
  wrapper.unmount()
})
it('preserves partially created redemption codes while showing the server failure', async () => {
  const { wrapper, state } = setup(useAdminRedemption)
  await flushPromises()
  mocks.post.mockRejectedValue(
    new ApiError('Only one code created', {
      business: true,
      data: ['created-code'],
    })
  )
  const result = await state.createCodes({
    name: 'credits',
    type: 'quota',
    amount: '1',
    count: 2,
    expired_time: -1,
  })
  expect(mocks.post).toHaveBeenCalledWith('/api/redemption/', {
    name: 'credits',
    quota: 500000,
    count: 2,
    expired_time: 0,
  })
  expect(result?.codes).toEqual(['created-code'])
  expect(mocks.error).toHaveBeenCalledWith('Only one code created')
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('sends native redemption sorting on both list and filtered requests and resets pagination', async () => {
  const { wrapper, state } = setup(useAdminRedemption)
  try {
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/redemption/',
      expect.objectContaining({ p: 1, sort_by: 'id', sort_order: 'desc' }),
      { signal: expect.any(AbortSignal) }
    )
    state.page.value = 3
    await flushPromises()
    expect(mocks.get.mock.lastCall![1].p).toBe(3)

    state.sortBy.value = 'created_time'
    state.sortOrder.value = 'asc'
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/redemption/',
      expect.objectContaining({
        p: 1,
        sort_by: 'created_time',
        sort_order: 'asc',
      }),
      { signal: expect.any(AbortSignal) }
    )

    state.statusFilter.value = 'used'
    state.sortBy.value = 'used_time'
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/redemption/search',
      expect.objectContaining({
        status: 3,
        p: 1,
        sort_by: 'used_time',
        sort_order: 'asc',
      }),
      { signal: expect.any(AbortSignal) }
    )
  } finally {
    wrapper.unmount()
  }
})
