import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  fetchSelf: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { quota: 0 }, fetchSelf: mocks.fetchSelf }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: ref(false) }),
}))
import { useInvite } from '../useInvite'
beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({
    code: 'FIXTURE',
    transferable: 1,
    policy: { enabled: true },
  })
  mocks.fetchSelf.mockResolvedValue(undefined)
})
function mountInvite() {
  let state!: ReturnType<typeof useInvite>
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useInvite()
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
  return { state, wrapper }
}
it('transfers even one quota unit in full and reuses the operation after a lost response', async () => {
  const { state, wrapper } = mountInvite()
  await state.load()
  expect(state.canTransfer.value).toBe(true)
  mocks.post
    .mockRejectedValueOnce(new Error('Lost response'))
    .mockResolvedValueOnce({ replayed: true })
  await state.transfer()
  await state.transfer()
  await flushPromises()
  const first = mocks.post.mock.calls[0]!,
    second = mocks.post.mock.calls[1]!
  expect(first[0]).toBe('/api/user/affiliate/transfer')
  expect(first[1]).toEqual({ operation_id: expect.any(String) })
  expect(second[1]).toEqual(first[1])
  expect(mocks.fetchSelf).toHaveBeenCalledTimes(1)
  await state.transfer()
  expect(mocks.post.mock.calls[2]![1].operation_id).not.toBe(
    first[1].operation_id
  )
  wrapper.unmount()
})

it('uses the server total and resets server-side ordering to page one', async () => {
  const { state, wrapper } = mountInvite()
  mocks.get.mockResolvedValueOnce({
    page: 2,
    page_size: 20,
    total: 41,
    items: [{ masked_name: 'a***z', invited_at: 1700000000, reward_quota: 23 }],
  })
  await state.loadInvitees(2)
  expect(mocks.get).toHaveBeenLastCalledWith(
    '/api/user/affiliate/invitees?p=2&page_size=20&order=desc',
    undefined,
    { signal: expect.any(AbortSignal) }
  )
  expect(state.inviteesPageCount.value).toBe(3)
  expect(state.inviteesPage.value).toBe(2)
  expect(state.invitees.value[0]).toEqual({
    masked_name: 'a***z',
    invited_at: 1700000000,
    reward_quota: 23,
  })
  mocks.get.mockResolvedValueOnce({
    page: 1,
    page_size: 20,
    total: 41,
    items: [],
  })
  await state.sortInvitees('asc')
  expect(mocks.get).toHaveBeenLastCalledWith(
    '/api/user/affiliate/invitees?p=1&page_size=20&order=asc',
    undefined,
    { signal: expect.any(AbortSignal) }
  )
  expect(state.inviteesPage.value).toBe(1)
  wrapper.unmount()
})
it('ignores a late previous page after changing the referral order', async () => {
  const { state, wrapper } = mountInvite()
  let resolvePrevious!: (value: unknown) => void
  mocks.get.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolvePrevious = resolve
      })
  )
  const previous = state.loadInvitees(2)
  mocks.get.mockResolvedValueOnce({
    page: 1,
    page_size: 20,
    total: 1,
    items: [{ masked_name: 'n***w', invited_at: 1700000001, reward_quota: 0 }],
  })
  await state.sortInvitees('asc')
  resolvePrevious({
    page: 2,
    page_size: 20,
    total: 41,
    items: [{ masked_name: 'o***d', invited_at: 1700000000, reward_quota: 7 }],
  })
  await previous
  expect(state.inviteesPage.value).toBe(1)
  expect(state.inviteesTotal.value).toBe(1)
  expect(state.invitees.value[0]?.masked_name).toBe('n***w')
  wrapper.unmount()
})
it('reports a malformed referral page instead of inventing a successful empty list', async () => {
  const { state, wrapper } = mountInvite()
  mocks.get.mockResolvedValueOnce({
    page: 1,
    page_size: 20,
    total: 2,
    items: [
      { masked_name: 'a***z', invited_at: 1700000000, reward_quota: '23' },
    ],
  })
  await state.loadInvitees()
  expect(state.inviteesError.value).toContain('Invalid API response')
  expect(state.invitees.value).toEqual([])
  expect(state.inviteesLoading.value).toBe(false)
  wrapper.unmount()
})
