import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import { useInvite } from '@/composables/useInvite'
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
    let state!: ReturnType<typeof useInvite>
    const wrapper = mount(
      defineComponent({
        setup() {
          state = useInvite()
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } }
    )
    await state.load()
    state.transferDollars.value = '1'
    expect(state.canTransfer.value).toBe(false)
    await state.transfer()
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
