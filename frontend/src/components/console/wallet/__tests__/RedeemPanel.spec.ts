import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import RedeemPanel from '@/components/console/wallet/RedeemPanel.vue'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { formatQuota } from '@/utils/format'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().toasts.splice(0)
})

describe('RedeemPanel', () => {
  it('renders the real redemption form without a fake history request', async () => {
    const get = vi.spyOn(api, 'get')
    const wrapper = mount(RedeemPanel, {
      global: { plugins: [createPinia(), i18n] },
    })

    await flushPromises()

    expect(get).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Redeem code')
    expect(wrapper.text()).not.toContain('No redeem records yet')
    expect(wrapper.find('input').attributes('placeholder')).toBe(
      'Enter redeem code'
    )

    wrapper.unmount()
  })

  it('confirms the credited quota rather than repeating the button label', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    vi.spyOn(useAuthStore(), 'fetchSelf').mockResolvedValue(true)
    // The bonus sits beside data in the real envelope.
    vi.spyOn(api, 'postEnvelope').mockResolvedValue({
      success: true,
      message: '',
      data: 5000000,
      bonus_quota: 0,
    } as never)
    // Attached, so clicking the submit button submits the form as in a browser.
    const wrapper = mount(RedeemPanel, {
      attachTo: document.body,
      global: { plugins: [pinia, i18n] },
    })

    await wrapper.get('input').setValue('CODE-1')
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(useToast().toasts.map((toast) => toast.message)).toEqual([
      `Redeemed: ${formatQuota(5000000)} credited`,
    ])
    expect(wrapper.emitted('done')).toHaveLength(1)
    wrapper.unmount()
  })
})
