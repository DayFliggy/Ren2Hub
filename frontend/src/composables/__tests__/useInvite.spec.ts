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
  it('disables reward transfer when payment compliance is not confirmed', async () => {
    auth.user.quota = 0
    vi.spyOn(api, 'get').mockResolvedValue({
      ...info,
      transfer_enabled: false,
      outstanding_offset: 500000,
    })
    const post = vi.spyOn(api, 'post')
    const { state, wrapper } = mountInvite()
    await state.load()
    expect(state.canTransfer.value).toBe(false)
    await state.transfer()
    expect(post).not.toHaveBeenCalled()
    const card = mount(InviteRewardCard, {
      props: {
        transferable: '3',
        frozen: '0',
        rewardTotal: '3',
        invited: 1,
        outstandingOffset: '500000',
        transferEnabled: false,
      },
      global: { plugins: [i18n] },
    })
    expect(card.get('button').attributes('disabled')).toBeDefined()
    expect(card.text()).toContain('Payment compliance has not been confirmed')
    expect(card.text()).toContain('Pending reward offset')
    expect(card.text()).toContain('$1.00')
    card.unmount()
    wrapper.unmount()
  })
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

describe('invite sharing follows the native registration switches', () => {
  const originalClipboard = Object.getOwnPropertyDescriptor(
    navigator,
    'clipboard'
  )
  let writeText: ReturnType<typeof vi.fn>
  let location: { href: string; origin: string }
  beforeEach(() => {
    writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    location = { href: '', origin: 'https://invite.test' }
    vi.stubGlobal('location', location)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    if (originalClipboard)
      Object.defineProperty(navigator, 'clipboard', originalClipboard)
    else Reflect.deleteProperty(navigator, 'clipboard')
  })

  async function shareAll(state: ReturnType<typeof useInvite>) {
    await state.copyCode()
    await state.copyLink()
    state.shareX()
    state.shareTelegram()
    state.shareEmail()
  }

  it.each([
    {
      name: 'program paused',
      data: { ...info, policy: { enabled: false, freeze_hours: 0 } },
      notice:
        'The referral program is paused, so new users cannot register with an invite right now.',
    },
    {
      name: 'code disabled',
      data: { ...info, code_enabled: false },
      notice: 'Your invite code has been disabled. Contact an administrator.',
    },
  ])('$name: no link and no copy or share action fires', async (row) => {
    auth.user.quota = 0
    vi.spyOn(api, 'get').mockResolvedValue(row.data)
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const { state, wrapper } = mountInvite()
    await state.load()

    expect(state.shareable.value).toBe(false)
    expect(state.inviteLink.value).toBe('')
    expect(state.shareNotice.value).toBe(row.notice)
    await shareAll(state)
    expect(writeText).not.toHaveBeenCalled()
    expect(open).not.toHaveBeenCalled()
    expect(location.href).toBe('')
    // Existing rewards stay transferable while invitations are blocked.
    expect(state.canTransfer.value).toBe(true)
    wrapper.unmount()
  })

  it('copies and shares the sign-up link when both switches are on', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(info)
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const { state, wrapper } = mountInvite()
    await state.load()

    const link = 'https://invite.test/auth/sign-up?aff=INVITE'
    expect(state.shareable.value).toBe(true)
    expect(state.shareNotice.value).toBe('')
    expect(state.inviteLink.value).toBe(link)
    await shareAll(state)
    expect(writeText.mock.calls).toEqual([['INVITE'], [link]])
    expect(open).toHaveBeenCalledTimes(2)
    expect(location.href).toContain(`mailto:?subject=`)
    expect(location.href).toContain(encodeURIComponent(link))
    wrapper.unmount()
  })
})

describe('invitee request failures', () => {
  const page = (name: string, current = 1, total = 1) => ({
    items: [{ masked_name: name, invited_at: 1, reward_quota: 0 }],
    total,
    page: current,
    page_size: 20,
  })

  it('clears a failed sort and restores the requested ordering on retry', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(page('first'))
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(page('retry'))
    const { state, wrapper } = mountInvite()
    state.inviteesOrder.value = 'asc'
    await state.loadInvitees()
    state.inviteesOrder.value = 'desc'
    await state.loadInvitees(1)
    expect(state.invitees.value).toEqual([])
    expect(state.inviteesPage.value).toBe(1)
    expect(state.inviteesTotal.value).toBe(0)
    expect(state.inviteesPageCount.value).toBe(1)
    expect(state.inviteesError.value).not.toBe('')
    await state.loadInvitees()
    expect(state.invitees.value[0]?.masked_name).toBe('retry')
    expect(state.inviteesError.value).toBe('')
    expect(get.mock.calls[2]?.[1]).toMatchObject({ order: 'desc', p: 1 })
    wrapper.unmount()
  })

  it('clears failed pagination but retries the failed requested page', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(page('first', 1, 41))
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(page('third', 3, 41))
    const { state, wrapper } = mountInvite()
    await state.loadInvitees()
    await state.loadInvitees(3)
    expect(state.invitees.value).toEqual([])
    expect(state.inviteesPage.value).toBe(1)
    expect(state.inviteesTotal.value).toBe(0)
    await state.loadInvitees()
    expect(get.mock.calls[2]?.[1]).toMatchObject({ p: 3 })
    expect(state.inviteesPage.value).toBe(3)
    expect(state.invitees.value[0]?.masked_name).toBe('third')
    wrapper.unmount()
  })

  it('does not clear the latest success when an earlier request fails late', async () => {
    let rejectOld!: (error: Error) => void
    vi.spyOn(api, 'get')
      .mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            rejectOld = reject
          })
      )
      .mockResolvedValueOnce(page('latest'))
    const { state, wrapper } = mountInvite()
    const old = state.loadInvitees(2)
    state.inviteesOrder.value = 'asc'
    await state.loadInvitees(1)
    rejectOld(new Error('stale failure'))
    await old
    expect(state.invitees.value[0]?.masked_name).toBe('latest')
    expect(state.inviteesError.value).toBe('')
    expect(state.inviteesLoading.value).toBe(false)
    wrapper.unmount()
  })
})
