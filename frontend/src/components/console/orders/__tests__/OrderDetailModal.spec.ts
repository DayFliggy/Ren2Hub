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

describe('referral reward reversal', () => {
  const paid = {
    ...nativeTopUpDetail,
    status: 'success',
    credited_quota: 5000000,
    complete_time: 2,
  }
  const reward = { inviter_id: 12, reward_quota: 1000000, reversed: false }
  function section(wrapper: ReturnType<typeof render>) {
    return wrapper.find('[data-order-affiliate]')
  }
  function reverseButton(wrapper: ReturnType<typeof render>) {
    return wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reverse reward')
  }
  // Both confirmation dialogs are in the stubbed tree; pick this one by name.
  async function confirmReversal(wrapper: ReturnType<typeof render>) {
    await wrapper
      .get('[aria-label="Reverse reward"]')
      .findAll('button')
      .find((button) => button.text() === 'Confirm')!
      .trigger('click')
    await flushPromises()
  }

  it('shows the reward only for an order that paid one', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ ...paid, affiliate: null })
    const wrapper = render()
    await flushPromises()
    expect(section(wrapper).exists()).toBe(false)
    expect(reverseButton(wrapper)).toBeUndefined()
    wrapper.unmount()
  })

  it('reverses after confirmation and reports where the reward went', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ ...paid, affiliate: reward })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      record: {},
      replayed: false,
      reversed_quota: 1000000,
      revoked_quota: 750000,
      debt_added_quota: 250000,
    })
    const wrapper = render()
    await flushPromises()
    expect(section(wrapper).text()).toContain('Inviter #12 received $2.00')
    await reverseButton(wrapper)!.trigger('click')
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    await confirmReversal(wrapper)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(
      '/api/user/topup/admin/affiliate-reversal',
      { trade_no: 'GPtest' }
    )
    expect(reverseButton(wrapper)).toBeUndefined()
    expect(section(wrapper).text()).toContain('Reversed')
    expect(wrapper.get('[data-order-affiliate-result]').text()).toBe(
      'Revoked $1.50 from untransferred rewards; added $0.50 to the pending offset'
    )
    wrapper.unmount()
  })

  it('keeps the action and shows the server message when the reversal fails', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ ...paid, affiliate: reward })
    vi.spyOn(api, 'post').mockRejectedValue(new Error('permission denied'))
    const wrapper = render()
    await flushPromises()
    await reverseButton(wrapper)!.trigger('click')
    await confirmReversal(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toBe('permission denied')
    expect(reverseButton(wrapper)).toBeDefined()
    expect(wrapper.find('[data-order-affiliate-result]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('sends one reversal for a repeated confirmation', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ ...paid, affiliate: reward })
    let finish!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const wrapper = render()
    await flushPromises()
    await reverseButton(wrapper)!.trigger('click')
    await confirmReversal(wrapper)
    await confirmReversal(wrapper)
    expect(post).toHaveBeenCalledTimes(1)
    finish({
      record: {},
      replayed: false,
      reversed_quota: 1000000,
      revoked_quota: 1000000,
      debt_added_quota: 0,
    })
    await flushPromises()
    expect(section(wrapper).text()).toContain('Reversed')
    wrapper.unmount()
  })

  it('does not mark another order when a reversal finishes after switching', async () => {
    const other = { inviter_id: 13, reward_quota: 250000, reversed: false }
    vi.spyOn(api, 'get').mockImplementation((url: string) =>
      Promise.resolve(
        url.endsWith('/GPother')
          ? { ...paid, trade_no: 'GPother', affiliate: other }
          : { ...paid, affiliate: reward }
      )
    )
    let finish!: (value: unknown) => void
    vi.spyOn(api, 'post').mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const wrapper = render()
    await flushPromises()
    await reverseButton(wrapper)!.trigger('click')
    await confirmReversal(wrapper)
    await wrapper.setProps({ order: { ...order, order_no: 'GPother' } })
    await flushPromises()
    finish({
      record: {},
      replayed: false,
      reversed_quota: 1000000,
      revoked_quota: 1000000,
      debt_added_quota: 0,
    })
    await flushPromises()
    expect(section(wrapper).text()).toContain('Inviter #13 received $0.50')
    expect(section(wrapper).text()).not.toContain('Reversed')
    expect(reverseButton(wrapper)).toBeDefined()
    expect(wrapper.find('[data-order-affiliate-result]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('marks a reversed reward and hides the action from common users', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      ...paid,
      affiliate: { ...reward, reversed: true },
    })
    const reversed = render()
    await flushPromises()
    expect(section(reversed).text()).toContain('Reversed')
    expect(reverseButton(reversed)).toBeUndefined()
    reversed.unmount()

    signIn(1)
    vi.spyOn(api, 'get').mockResolvedValue({ ...paid, affiliate: reward })
    const common = render()
    await flushPromises()
    expect(section(common).exists()).toBe(true)
    expect(reverseButton(common)).toBeUndefined()
    common.unmount()
  })
})
