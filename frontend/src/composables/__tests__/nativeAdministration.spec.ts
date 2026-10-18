import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  proof: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isRoot: true, isAdmin: true, user: { role: 100 } }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
vi.mock('@/composables/useFeatureAccess', async () => {
  const { ref } = await import('vue')
  return { useFeatureAccess: () => ({ readOnly: ref(false) }) }
})
import { useAdminUsers } from '../useAdminUsers'
import { useAdminRedemption } from '../useAdminRedemption'
import { ApiError } from '@/api/types'
import type { AdminUser } from '@/types/console'
const member = { id: 2, role: 1, status: 1, username: 'member' } as AdminUser
beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ items: [], total: 0 })
  mocks.proof.mockResolvedValue('proof')
  mocks.post.mockResolvedValue(null)
  mocks.put.mockResolvedValue(null)
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
it('uses native integer quota adjustment and rejects nonintegral quota', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
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
it('binds sensitive user changes to native verification contexts and separates role management', async () => {
  const { wrapper, state } = setup(() => useAdminUsers(mocks.proof))
  await flushPromises()
  await state.updateUserDetails(member, {
    username: 'member',
    display_name: 'Member',
    email: '',
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
  expect(mocks.put.mock.calls[0]![1]).not.toHaveProperty('role')
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
