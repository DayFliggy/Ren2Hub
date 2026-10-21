import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { AdminOrder } from '@/types/console'
import { nativeTopUp } from '@/api/__tests__/topupFixtures'
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true, hasPermission: () => true }),
}))
import OrderDetailModal from '../OrderDetailModal.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())

const order: AdminOrder = {
  id: 1,
  order_no: 'GPtest',
  user_id: 1,
  username: 'user',
  email: '',
  amount: 10,
  quota: '0',
  type: 'topup',
  currency: 'USD',
  method: 'gmpay',
  payment_method: 'gmpay:usdt.tron',
  status: 'pending',
  created: 1,
  paid_at: 0,
}
const evidence = {
  status: 'settled',
  last_failure: '',
  token: 'usdt',
  network: 'tron',
  gateway_trade_no: 'gateway-id',
  gateway_crypto_amount: '10.1',
  receive_address: 'original-address',
  settled_crypto_amount: '9.9',
  settled_receive_address: 'final-address',
  settled_transaction_id: 'receipt-hash',
}
function render() {
  return mount(OrderDetailModal, {
    props: { order },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}

describe('GMPay order details', () => {
  it('separates requested network and creation details from actual settlement evidence', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUp,
      status: 'success',
      credited_quota: 5000000,
      complete_time: 2,
      gmpay: evidence,
    })
    const wrapper = render()
    await flushPromises()
    expect(
      wrapper.findAll('dd').some((field) => field.text() === 'Settled')
    ).toBe(true)
    expect(wrapper.text()).toContain('Requested network (not verified)')
    expect(wrapper.text()).toContain('original-address')
    expect(wrapper.text()).toContain('final-address')
    expect(wrapper.text()).toContain('receipt-hash')
    expect(wrapper.text()).toContain('Completed')
    expect(wrapper.text()).not.toContain('Not credited')
    wrapper.unmount()
  })

  it('ignores an older response after selecting another order', async () => {
    let finish!: (value: unknown) => void
    const get = vi
      .spyOn(api, 'get')
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve
          })
      )
      .mockResolvedValueOnce({
        ...nativeTopUp,
        trade_no: 'GPnew',
        gmpay: { ...evidence, gateway_trade_no: 'new-gateway-id' },
      })
    const wrapper = render()
    await wrapper.setProps({ order: { ...order, order_no: 'GPnew' } })
    await flushPromises()
    expect(get.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    finish({
      ...nativeTopUp,
      gmpay: { ...evidence, gateway_trade_no: 'stale-gateway-id' },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('new-gateway-id')
    expect(wrapper.text()).not.toContain('stale-gateway-id')
    wrapper.unmount()
  })

  it('reports detail failures and retries explicitly', async () => {
    vi.spyOn(api, 'get')
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ ...nativeTopUp, gmpay: evidence })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reload evidence')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('receipt-hash')
    wrapper.unmount()
  })
})
