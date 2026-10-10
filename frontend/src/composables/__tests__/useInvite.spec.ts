import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import { useInvite } from '@/composables/useInvite'
import { useToast } from '@/composables/useToast'
import InviteRewardCard from '@/components/console/invite/InviteRewardCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const auth = vi.hoisted(() => ({
  user: { available_quota: '9223372036854775807' },
  fetchSelf: vi.fn(),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: { value: false } }),
}))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
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

describe('invite transfer balance boundary', () => {
  it('keeps the reward card usable when transferring all rewards would overflow', () => {
    const wrapper = mount(InviteRewardCard, {
      props: {
        transferable: '500000',
        frozen: '0',
        rewardTotal: '500000',
        invited: 1,
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('—')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('does not submit a transfer whose wallet projection exceeds int64', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      transferable: '500000',
      code: 'test',
    })
    const post = vi.spyOn(api, 'post')
    const { state, wrapper } = mountInvite()
    await state.load()
    state.transferDollars.value = '1'
    expect(state.canTransfer.value).toBe(false)
    await state.transfer()
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('confirms a transfer in the console language', async () => {
    const quota = auth.user.available_quota
    auth.user.available_quota = '0'
    vi.spyOn(api, 'get').mockResolvedValue({
      transferable: '500000',
      code: 'test',
    })
    // The backend still answers with its own Chinese text.
    vi.spyOn(api, 'post').mockResolvedValue({
      message: '已转入账户余额',
      transferred: '500000',
    })
    const toast = useToast()
    const { state, wrapper } = mountInvite()
    try {
      await state.load()
      state.transferDollars.value = '1'
      await state.transfer()
      expect(toast.toasts.map((item) => item.message)).toContain(
        'Transferred to your account balance'
      )
    } finally {
      auth.user.available_quota = quota
      wrapper.unmount()
    }
  })
})
