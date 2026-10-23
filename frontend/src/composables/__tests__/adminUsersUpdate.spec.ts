import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  putEnvelope: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  proof: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))

import { useAdminUsers } from '../useAdminUsers'
import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { AdminUser, AdminUserUpdateInput } from '@/types/console'

const member = {
  id: 2,
  role: 1,
  status: 1,
  deleted: false,
  username: 'member',
} as AdminUser
const promotion: AdminUserUpdateInput = {
  username: 'member',
  display_name: 'Renamed',
  remark: '',
  role: 10,
  max_concurrency: 0,
}

// The role write can commit before a later step fails, so the message must not
// claim either outcome.
const roleRequestFailed = (message: string) =>
  `Profile saved. The role change request returned an error (${message}); check the role in the refreshed list.`

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().persist({
    id: 1,
    username: 'root',
    display_name: '',
    email: '',
    role: 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: {} },
  })
  for (const mock of Object.values(mocks)) mock.mockReset()
  mocks.get.mockResolvedValue({ items: [], total: 0 })
  mocks.proof.mockResolvedValue('proof')
  mocks.putEnvelope.mockResolvedValue({ success: true })
  mocks.post.mockResolvedValue(null)
})

async function setup() {
  let state!: ReturnType<typeof useAdminUsers>
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useAdminUsers(mocks.proof)
        return () => null
      },
    }),
    { global: { plugins: [i18n] } }
  )
  await flushPromises()
  mocks.get.mockClear()
  return { wrapper, state }
}

it('reports a saved profile whose role change failed and reloads the list', async () => {
  const { wrapper, state } = await setup()
  mocks.post.mockRejectedValue(
    new ApiError('Role change rejected', { status: 403 })
  )
  expect(await state.updateUserDetails(member, promotion)).toBe(true)
  expect(mocks.putEnvelope).toHaveBeenCalledOnce()
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith(
    roleRequestFailed('Role change rejected')
  )
  expect(mocks.get).toHaveBeenCalledOnce()
  expect(state.busy.value).toBe(false)
  wrapper.unmount()
})

it('keeps the saved profile when the role verification is cancelled', async () => {
  const { wrapper, state } = await setup()
  // Browsers reject with an Error named AbortError; jsdom's DOMException is not one.
  mocks.proof.mockRejectedValue(
    Object.assign(new Error('Cancelled'), { name: 'AbortError' })
  )
  expect(await state.updateUserDetails(member, promotion)).toBe(true)
  expect(mocks.post).not.toHaveBeenCalled()
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith(
    roleRequestFailed('Cancelled')
  )
  expect(mocks.get).toHaveBeenCalledOnce()
  wrapper.unmount()
})

it('reports a failed profile save as a whole failure without touching the role', async () => {
  const { wrapper, state } = await setup()
  mocks.putEnvelope.mockRejectedValue(
    new ApiError('Username taken', { status: 200 })
  )
  expect(await state.updateUserDetails(member, promotion)).toBe(false)
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith('Username taken')
  expect(mocks.proof).not.toHaveBeenCalled()
  expect(mocks.post).not.toHaveBeenCalled()
  expect(mocks.get).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('saves profile and role together with one reload and no error', async () => {
  const { wrapper, state } = await setup()
  expect(await state.updateUserDetails(member, promotion)).toBe(true)
  expect(mocks.post).toHaveBeenCalledWith(
    '/api/user/manage',
    { id: 2, action: 'promote' },
    { headers: { 'X-Security-Proof': 'proof' } }
  )
  expect(mocks.error).not.toHaveBeenCalled()
  expect(mocks.get).toHaveBeenCalledOnce()
  wrapper.unmount()
})

it('keeps a committed update successful while warning about pending runtime publication', async () => {
  const { wrapper, state } = await setup()
  mocks.putEnvelope.mockResolvedValue({
    success: true,
    publication_pending: true,
  })
  expect(
    await state.updateUserDetails(member, { ...promotion, role: member.role })
  ).toBe(true)
  expect(mocks.warning).toHaveBeenCalledExactlyOnceWith(
    i18n.global.t('common.publicationPending')
  )
  expect(mocks.error).not.toHaveBeenCalled()
  expect(mocks.get).toHaveBeenCalledOnce()
  wrapper.unmount()
})

it('stays neutral when the demotion committed but a later step rejected', async () => {
  const admin = { ...member, role: 10 } as AdminUser
  const { wrapper, state } = await setup()
  // controller/user.go commits the role, then ReloadPolicy, PublishUserAuthCache
  // or RevokeAllUserSessions can still fail the request.
  mocks.post.mockRejectedValue(
    new ApiError('Failed to revoke sessions', { status: 500 })
  )
  mocks.get.mockResolvedValue({
    items: [
      {
        ...member,
        display_name: 'Renamed',
        email: '',
        quota: 0,
        used_quota: 0,
        request_count: 0,
        max_concurrency: 0,
        aff_count: 0,
        aff_quota: 0,
        aff_history_quota: 0,
        aff_frozen_quota: 0,
        inviter_id: 0,
        created_at: 1,
        last_login_at: 0,
        DeletedAt: null,
      },
    ],
    total: 1,
  })
  expect(await state.updateUserDetails(admin, { ...promotion, role: 1 })).toBe(
    true
  )
  expect(mocks.post).toHaveBeenCalledWith(
    '/api/user/manage',
    { id: 2, action: 'demote' },
    { headers: { 'X-Security-Proof': 'proof' } }
  )
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith(
    roleRequestFailed('Failed to revoke sessions')
  )
  // The reload shows the committed role, which the message points to.
  expect(mocks.get).toHaveBeenCalledOnce()
  expect(state.rows.value.map((user) => user.role)).toEqual([1])
  wrapper.unmount()
})
