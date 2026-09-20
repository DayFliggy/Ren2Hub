import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import TopupRecords from '@/components/console/wallet/TopupRecords.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const order = {
  id: 1,
  order_no: 'USR1NOtest',
  user_id: 1,
  username: '',
  email: '',
  amount: 73,
  quota: '0',
  type: 'topup',
  currency: 'CNY',
  method: 'epay',
  payment_method: 'alipay',
  status: 'pending',
  created: 1785114000,
  paid_at: 0,
}

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
    pageSize: 6,
  })
  const wrapper = mount(TopupRecords, { global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}

describe('TopupRecords payment verification', () => {
  it('keeps reads side-effect free and explicitly checks payment before refreshing the balance', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ ...order, status: 'completed', quota: '5000000' })
    const wrapper = await render()
    expect(post).not.toHaveBeenCalled()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Check payment result')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/next/wallet/topups/USR1NOtest/verify',
      undefined,
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.text()).toContain('Payment confirmed and credited')
    expect(wrapper.emitted('done')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('Check payment result')
    wrapper.unmount()
  })

  it('does not report success or change the order after a failed check', async () => {
    vi.spyOn(api, 'post').mockRejectedValue(new ApiError('Gateway timed out'))
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Check payment result')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Gateway timed out')
    expect(wrapper.text()).toContain('Pending')
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })
})
