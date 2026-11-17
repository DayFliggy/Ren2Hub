import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { AdminOrder } from '@/types/console'
import { nativeTopUpDetail } from '@/api/__tests__/topupFixtures'
import OrderDetailModal from '../OrderDetailModal.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  setActivePinia(createPinia())
  signIn(10)
})
afterEach(() => vi.restoreAllMocks())

// authz.Capabilities output: Casbin has no billing resource, and the native
// /api/user/topup/complete route only requires AdminAuth.
function signIn(role: number) {
  useAuthStore().persist({
    id: 3,
    username: 'operator',
    display_name: '',
    email: '',
    role,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: {
      admin_permissions: {
        audit: { read: true },
        routing: { read: true, write: false },
        ticket: { read: true, reply: true, manage: false },
      },
    },
  })
}

const order: AdminOrder = {
  id: 1,
  order_no: 'GPtest',
  user_id: 1,
  username: 'user',
  email: '',
  amount: 10,
  quota: '0',
  bonus_quota: '0',
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
        ConsoleModal: {
          template:
            '<div><slot /><footer><slot name="footer" /></footer></div>',
        },
      },
    },
  })
}

function completeButton(wrapper: ReturnType<typeof render>) {
  return wrapper
    .findAll('button')
    .find((button) => button.text() === 'Complete manually')
}

describe('manual completion', () => {
  it('spaces the available footer actions without sending completion before confirmation', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(nativeTopUpDetail)
    const post = vi.spyOn(api, 'post').mockResolvedValue(undefined)
    const wrapper = render()
    await flushPromises()
    const complete = completeButton(wrapper)!
    const close = wrapper
      .get('footer')
      .findAll('button')
      .find((button) => button.text() === 'Close')!
    expect(complete.element.parentElement).toBe(close.element.parentElement)
    expect(close.element.parentElement?.classList.contains('gap-3')).toBe(true)
    expect(
      close.element.parentElement?.classList.contains('sm:grid-cols-2')
    ).toBe(true)
    await complete.trigger('click')
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('offers manual completion of a pending order to a role-10 admin', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(nativeTopUpDetail)
    const post = vi.spyOn(api, 'post').mockResolvedValue(undefined)
    const wrapper = render()
    await flushPromises()
    await completeButton(wrapper)!.trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Confirm')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/user/topup/complete', {
      trade_no: 'GPtest',
    })
    wrapper.unmount()
  })

  it('hides manual completion from common users', async () => {
    signIn(1)
    vi.spyOn(api, 'get').mockResolvedValue(nativeTopUpDetail)
    const wrapper = render()
    await flushPromises()
    expect(completeButton(wrapper)).toBeUndefined()
    const footer = wrapper.get('footer')
    expect(footer.findAll('button')).toHaveLength(1)
    expect(
      footer
        .get('button')
        .element.parentElement?.classList.contains('sm:grid-cols-2')
    ).toBe(false)
    wrapper.unmount()
  })

  it('hides manual completion once the order is no longer pending', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUpDetail,
      status: 'success',
      credited_quota: 5000000,
      complete_time: 2,
    })
    const wrapper = render()
    await flushPromises()
    expect(completeButton(wrapper)).toBeUndefined()
    wrapper.unmount()
  })
})

describe('GMPay order details', () => {
  it('separates requested network and creation details from actual settlement evidence', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUpDetail,
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

  it('shows a settled switched-asset payment without a chain transaction id as not recorded', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...nativeTopUpDetail,
      status: 'success',
      credited_quota: 5000000,
      complete_time: 2,
      gmpay: {
        ...evidence,
        settled_receive_address: 'parent-address',
        settled_transaction_id: '',
      },
    })
    const wrapper = render()
    await flushPromises()
    const fields = Object.fromEntries(
      wrapper
        .findAll('dl > div')
        .map((field) => [field.get('dt').text(), field.get('dd').text()])
    )
    expect(fields['Gateway order status']).toBe('Settled')
    expect(fields['Callback receiving address']).toBe('parent-address')
    expect(fields['Settled transaction ID']).toBe('Not recorded')
    expect(wrapper.text()).toContain('Completed')
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
        ...nativeTopUpDetail,
        trade_no: 'GPnew',
        gmpay: { ...evidence, gateway_trade_no: 'new-gateway-id' },
      })
    const wrapper = render()
    await wrapper.setProps({ order: { ...order, order_no: 'GPnew' } })
    await flushPromises()
    expect(get.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    finish({
      ...nativeTopUpDetail,
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
      .mockResolvedValueOnce({ ...nativeTopUpDetail, gmpay: evidence })
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

  it('marks the owner as deleted only when the detail says so', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({ ...nativeTopUpDetail, gmpay: evidence })
      .mockResolvedValueOnce({
        ...nativeTopUpDetail,
        trade_no: 'GPgone',
        username: '',
        email: '',
        user_deleted: true,
        gmpay: evidence,
      })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).not.toContain('User deleted')
    await wrapper.setProps({
      order: { ...order, order_no: 'GPgone', username: '' },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('User deleted')
    expect(wrapper.text()).toContain('#7')
    wrapper.unmount()
  })
})
