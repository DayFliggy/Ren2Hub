import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

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
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))

import { useAdminUsers } from '../useAdminUsers'
import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { AdminUser } from '@/types/console'

function user(id: number, status: 1 | 2 = 1) {
  return {
    id,
    role: 1,
    status,
    deleted: false,
    username: `member-${id}`,
  } as AdminUser
}
const targets = [user(2), user(3), user(4)]
const page = { items: [], total: 0 }

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
  mocks.get.mockResolvedValue(page)
  mocks.proof.mockImplementation(
    async ({ context }: { context: { user_id: number } }) =>
      `proof-${context.user_id}`
  )
  mocks.post.mockResolvedValue(null)
  mocks.delete.mockResolvedValue(null)
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

it('verifies each user separately and refreshes the list once', async () => {
  const { wrapper, state } = await setup()
  expect(await state.runBulk('delete', targets)).toBe(true)
  expect(mocks.proof.mock.calls.map(([operation]) => operation)).toEqual(
    targets.map((target) => ({
      scope: 'admin.user.delete',
      context: { user_id: target.id },
    }))
  )
  expect(mocks.delete.mock.calls).toEqual(
    targets.map((target) => [
      `/api/user/${target.id}`,
      undefined,
      { headers: { 'X-Security-Proof': `proof-${target.id}` } },
    ])
  )
  expect(mocks.get).toHaveBeenCalledOnce()
  expect(mocks.success).toHaveBeenCalledExactlyOnceWith('Deleted 3 users')
  expect(state.busy.value).toBe(false)
  wrapper.unmount()
})

it('stops at the first failure without touching later users', async () => {
  const { wrapper, state } = await setup()
  mocks.post
    .mockResolvedValueOnce(null)
    .mockRejectedValueOnce(new ApiError('Target is busy', { status: 409 }))
  expect(await state.runBulk('disable', targets)).toBe(false)
  expect(mocks.proof).toHaveBeenCalledTimes(2)
  expect(mocks.post).toHaveBeenCalledTimes(2)
  expect(mocks.proof).not.toHaveBeenCalledWith(
    expect.objectContaining({ context: { user_id: 4, action: 'disable' } })
  )
  expect(mocks.get).toHaveBeenCalledOnce()
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith(
    'Processed 1/3 users; member-3 failed: Target is busy'
  )
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('treats a cancelled verification as a stop', async () => {
  const { wrapper, state } = await setup()
  // SecurityProofDialog rejects with an AbortError DOMException, which is an
  // Error in browsers; jsdom's DOMException is not, so build the browser shape.
  const cancelled = Object.assign(new Error('Cancelled'), {
    name: 'AbortError',
  })
  mocks.proof.mockResolvedValueOnce('proof-2').mockRejectedValueOnce(cancelled)
  expect(await state.runBulk('delete', targets)).toBe(false)
  expect(mocks.delete).toHaveBeenCalledOnce()
  expect(mocks.proof).toHaveBeenCalledTimes(2)
  expect(mocks.error).toHaveBeenCalledExactlyOnceWith(
    'Processed 1/3 users; member-3 failed: Cancelled'
  )
  wrapper.unmount()
})

it('skips users already in the target state', async () => {
  const { wrapper, state } = await setup()
  expect(
    await state.runBulk('enable', [user(2, 2), user(3, 1), user(4, 2)])
  ).toBe(true)
  expect(mocks.post.mock.calls.map(([, body]) => body)).toEqual([
    { id: 2, action: 'enable' },
    { id: 4, action: 'enable' },
  ])
  expect(mocks.success).toHaveBeenCalledExactlyOnceWith('Enabled 2 users')
  wrapper.unmount()
})

it('does nothing when no selected user needs the change', async () => {
  const { wrapper, state } = await setup()
  expect(await state.runBulk('enable', [user(2), user(3)])).toBe(true)
  expect(mocks.proof).not.toHaveBeenCalled()
  expect(mocks.get).not.toHaveBeenCalled()
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('reports a failed refresh as a load error, not as a user failure', async () => {
  const { wrapper, state } = await setup()
  mocks.get.mockRejectedValueOnce(new Error('List unavailable'))
  expect(await state.runBulk('delete', targets.slice(0, 2))).toBe(true)
  expect(mocks.success).toHaveBeenCalledExactlyOnceWith('Deleted 2 users')
  expect(mocks.error).not.toHaveBeenCalled()
  expect(state.initialError.value).toBe('List unavailable')
  wrapper.unmount()
})

it('holds one busy lock for the whole batch and exposes progress', async () => {
  const { wrapper, state } = await setup()
  let release!: () => void
  mocks.delete
    .mockResolvedValueOnce(null)
    .mockImplementationOnce(() => new Promise<void>((r) => (release = r)))
  const run = state.runBulk('delete', targets.slice(0, 2))
  await flushPromises()
  expect(state.busy.value).toBe(true)
  expect(state.bulkProgress.value).toEqual({ done: 1, total: 2 })
  expect(await state.toggleStatus(user(9))).toBe(false)
  expect(await state.runBulk('delete', [user(9)])).toBe(false)
  release()
  expect(await run).toBe(true)
  expect(state.bulkProgress.value).toBeNull()
  expect(mocks.proof).toHaveBeenCalledTimes(2)
  wrapper.unmount()
})
