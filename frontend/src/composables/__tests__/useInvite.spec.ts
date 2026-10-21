import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/client'
import { useInvite } from '@/composables/useInvite'
import { useToast } from '@/composables/useToast'
import InviteRewardCard from '@/components/console/invite/InviteRewardCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const auth = vi.hoisted(() => ({
  user: { quota: Number.MAX_SAFE_INTEGER },
  fetchSelf: vi.fn(),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: { value: false } }),
}))
const info = {
  code: 'INVITE',
  code_enabled: true,
  invited: 1,
  effective_rate_bps: 2000,
  effective_rate_percent: 20,
  transferable: 3,
  frozen_reward: 0,
  reward_total: 3,
  monthly_series: [
    { month: '2026-10', new_count: 1, cumulative: 1, reward: 3 },
  ],
  policy: { enabled: true, freeze_hours: 0 },
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  auth.user.quota = Number.MAX_SAFE_INTEGER
  auth.fetchSelf.mockReset()
})
afterEach(() => vi.restoreAllMocks())

const InviteHost = defineComponent({
  emits: ['ready'],
  setup(_, { emit }) {
    emit('ready', useInvite())
    return () => h('div')
  },
})
function mountInvite() {
  let state!: ReturnType<typeof useInvite>
  const wrapper = mount(InviteHost, {
    props: {
      onReady: (value: ReturnType<typeof useInvite>) => (state = value),
    },
    global: { plugins: [i18n] },
  })
  return { state, wrapper }
}

describe('native full referral transfer', () => {
  it('blocks the full transfer when the native wallet would exceed the safe quota limit', async () => {
    const card = mount(InviteRewardCard, {
      props: { transferable: '3', frozen: '0', rewardTotal: '3', invited: 1 },
      global: { plugins: [i18n] },
    })
    expect(card.text()).toContain('—')
    expect(card.get('button').attributes('disabled')).toBeDefined()
    vi.spyOn(api, 'get').mockResolvedValue(info)
    const post = vi.spyOn(api, 'post')
    const { state, wrapper } = mountInvite()
    await state.load()
    expect(state.canTransfer.value).toBe(false)
    await state.transfer()
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
    card.unmount()
  })
  it('transfers any positive balance in full and keeps the operation ID after a lost response', async () => {
    auth.user.quota = 0
    vi.spyOn(api, 'get').mockResolvedValue(info)
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValueOnce(new Error('response lost'))
      .mockResolvedValueOnce({
        replayed: true,
        transferred_quota: 3,
        wallet_quota: 3,
      })
    const { state, wrapper } = mountInvite()
    await state.load()
    expect(state.canTransfer.value).toBe(true)
    await state.transfer()
    await state.transfer()
    const first = post.mock.calls[0]!
    expect(first[0]).toBe('/api/user/affiliate/transfer')
    expect(first[1]).toEqual({ operation_id: expect.any(String) })
    expect(post.mock.calls[1]![1]).toEqual(first[1])
    expect(auth.fetchSelf).toHaveBeenCalledOnce()
    expect(useToast().toasts.map((item) => item.message)).toContain(
      'Transferred to your account balance'
    )
    wrapper.unmount()
  })
  it('does not replace the current invitee ordering with a stale response', async () => {
    let resolveOld!: (value: unknown) => void
    const get = vi
      .spyOn(api, 'get')
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveOld = resolve
          })
      )
      .mockResolvedValueOnce({
        items: [{ masked_name: 'n***w', invited_at: 1, reward_quota: 0 }],
        total: 1,
        page: 1,
        page_size: 20,
      })
    const { state, wrapper } = mountInvite()
    const first = state.loadInvitees(2)
    state.inviteesOrder.value = 'asc'
    await state.loadInvitees(1)
    resolveOld({
      items: [{ masked_name: 'o***d', invited_at: 2, reward_quota: 0 }],
      total: 21,
      page: 2,
      page_size: 20,
    })
    await first
    expect(state.invitees.value.map((row) => row.masked_name)).toEqual([
      'n***w',
    ])
    expect(state.inviteesPage.value).toBe(1)
    expect(get.mock.calls[0]![2]?.signal?.aborted).toBe(true)
    wrapper.unmount()
  })
})
