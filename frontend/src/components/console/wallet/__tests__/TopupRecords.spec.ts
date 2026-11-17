import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import TopupRecords from '@/components/console/wallet/TopupRecords.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

import { nativeTopUp as order } from '@/api/__tests__/topupFixtures'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())

async function render() {
  vi.spyOn(api, 'get').mockResolvedValue({
    items: [order],
    total: 1,
    page: 1,
    page_size: 6,
  })
  const wrapper = mount(TopupRecords, {
    props: { refreshKey: 0 },
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}

describe('TopupRecords', () => {
  it('only reads order status and shows externally completed payments after refresh', async () => {
    const post = vi.spyOn(api, 'post')
    const wrapper = await render()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/topup/self',
      { p: 1, page_size: 6 },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.text()).toContain('Pending')
    // The status already says the order is unpaid; no second "not credited".
    expect(wrapper.text()).not.toContain('Not credited')
    expect(wrapper.text()).not.toContain('Check payment result')
    expect(post).not.toHaveBeenCalled()

    vi.mocked(api.get).mockResolvedValueOnce({
      items: [{ ...order, status: 'success', credited_quota: 5000000 }],
      total: 1,
      page: 1,
      page_size: 6,
    })
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Completed')
    expect(wrapper.text()).toContain('$10.00')
    expect(wrapper.text()).not.toContain('Pending')
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows a retryable error state instead of an empty state on load failure', async () => {
    vi.spyOn(api, 'get').mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(TopupRecords, {
      props: { refreshKey: 0 },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('offline')
    expect(wrapper.text()).not.toContain('No data')

    vi.mocked(api.get).mockResolvedValueOnce({
      items: [order],
      total: 1,
      page: 1,
      page_size: 6,
    })
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(order.trade_no)
    wrapper.unmount()
  })
})
