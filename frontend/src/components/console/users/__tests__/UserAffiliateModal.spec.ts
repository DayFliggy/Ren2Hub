import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { affiliateApi, type AffiliateUserPolicy } from '@/api/affiliate'
import UserAffiliateModal from '../UserAffiliateModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import type { AdminUser } from '@/types/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const target = {
  id: 8,
  username: 'operator-target',
  aff_count: 1,
  aff_quota: 7,
  aff_frozen_quota: 2,
} as AdminUser
const policy: AffiliateUserPolicy = {
  user_id: 8,
  code: 'OLD-CODE',
  code_custom: true,
  enabled: false,
  rate_bps: 1234,
  effective_rate_bps: 1234,
  inviter_id: 0,
  invited_at: 0,
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())
function mountModal() {
  return mount(UserAffiliateModal, {
    props: { open: true, target },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}

describe('affiliate administrator policy', () => {
  it('resets the code while retaining the saved rate and disabled state', async () => {
    vi.spyOn(affiliateApi, 'userPolicy').mockResolvedValue(policy)
    const update = vi
      .spyOn(affiliateApi, 'updateUserPolicy')
      .mockResolvedValue({ ...policy, code: 'NEW-CODE', code_custom: false })
    const wrapper = mountModal()
    await flushPromises()
    const reset = wrapper
      .findAll('button')
      .find((button) =>
        button.text().includes(i18n.global.t('users.affiliateResetCode'))
      )!
    await reset.trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(8, { rate_bps: 1234, reset_code: true })
    expect(
      wrapper.findAllComponents(ConsoleToggle)[0]!.props('modelValue')
    ).toBe(false)
    expect(
      (
        wrapper.get('input[name="admin-user-affiliate-code"]')
          .element as HTMLInputElement
      ).value
    ).toBe('NEW-CODE')
    wrapper.unmount()
  })
  it('clears the rate override with null and does not send retired limit fields', async () => {
    vi.spyOn(affiliateApi, 'userPolicy').mockResolvedValue(policy)
    const update = vi
      .spyOn(affiliateApi, 'updateUserPolicy')
      .mockResolvedValue({
        ...policy,
        rate_bps: null,
        effective_rate_bps: 2000,
      })
    const wrapper = mountModal()
    await flushPromises()
    wrapper
      .findAllComponents(ConsoleToggle)[1]!
      .vm.$emit('update:modelValue', false)
    await flushPromises()
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === i18n.global.t('users.affiliateSave'))!
    await save.trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(8, { enabled: false, rate_bps: null })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('ignores a late policy response after switching the target user', async () => {
    let resolveOld!: (value: AffiliateUserPolicy) => void
    vi.spyOn(affiliateApi, 'userPolicy')
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveOld = resolve
          })
      )
      .mockResolvedValueOnce({ ...policy, user_id: 9, code: 'CURRENT-CODE' })
    const wrapper = mountModal()
    await wrapper.setProps({ target: { ...target, id: 9 } })
    await flushPromises()
    resolveOld(policy)
    await flushPromises()
    expect(
      (
        wrapper.get('input[name="admin-user-affiliate-code"]')
          .element as HTMLInputElement
      ).value
    ).toBe('CURRENT-CODE')
    wrapper.unmount()
  })
})
