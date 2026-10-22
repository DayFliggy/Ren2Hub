import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import InviteAffiliateCard from '@/components/console/invite/InviteAffiliateCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('InviteAffiliateCard', () => {
  it('shows the effective rebate rate without a fake QR code', () => {
    const wrapper = mount(InviteAffiliateCard, {
      props: {
        code: 'INVITE01',
        inviteLink: 'https://example.test/auth/sign-up?aff=INVITE01',
        ratePercent: 10,
        shareable: true,
        notice: '',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.text()).toContain('10%')
    expect(wrapper.find('[role="img"]').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(
      wrapper
        .findAll('button')
        .every((b) => b.attributes('disabled') === undefined)
    ).toBe(true)
  })

  it('explains a blocked invitation and disables every copy and share control', () => {
    const wrapper = mount(InviteAffiliateCard, {
      props: {
        code: 'INVITE01',
        inviteLink: '',
        ratePercent: 10,
        shareable: false,
        notice: 'Your invite code has been disabled. Contact an administrator.',
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('[role="status"]').text()).toBe(
      'Your invite code has been disabled. Contact an administrator.'
    )
    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(5)
    for (const button of buttons)
      expect(button.attributes('disabled')).toBeDefined()
  })
})
