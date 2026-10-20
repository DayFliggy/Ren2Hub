import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import { useActivity } from '@/composables/useActivity'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const auth = vi.hoisted(() => ({ fetchSelf: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('activity claim', () => {
  it('confirms a claim in the console language', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      activities: [],
      summary: { claimable: 0, reward_earned: '0', ongoing: 0 },
    })
    // The backend still answers with its own Chinese text.
    vi.spyOn(api, 'post').mockResolvedValue({
      message: '领取成功',
      reward: '1000',
    })
    let state!: ReturnType<typeof useActivity>
    const wrapper = mount(
      defineComponent({
        setup() {
          state = useActivity()
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } }
    )

    await state.claim(1)

    expect(useToast().toasts.map((item) => item.message)).toContain(
      'Claimed successfully'
    )
    wrapper.unmount()
  })
})
