import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
  request: vi.fn(),
}))
vi.mock('../client', () => ({ api: mocks }))
vi.mock('../httpTransport', () => ({
  httpTransport: { request: mocks.request },
}))
import { tokenRoutingApi } from '../tokenRouting'
import {
  paymentRequest,
  parseTopupInfo,
  parseCreemProducts,
  checkoutTarget,
  enabledPayMethods,
  paymentProvider,
  topupApi,
  type TopupInfo,
} from '../topup'
import { aggregateUsage } from '@/composables/useDashboardStats'
const route = {
  token_id: 7,
  mode: 'manual',
  version: 3,
  entries: [
    { channel_id: 8, enabled: true, position: 0 },
    { channel_id: 2, enabled: false, position: 1 },
  ],
}
beforeEach(() => vi.clearAllMocks())
describe('native core contracts', () => {
  it('saves ordered private entries with optimistic version and clears them for automatic mode', async () => {
    mocks.put.mockResolvedValue(route)
    await tokenRoutingApi.save(
      7,
      route as Parameters<typeof tokenRoutingApi.save>[1]
    )
    expect(mocks.put).toHaveBeenLastCalledWith('/api/token/7/routing', {
      mode: 'manual',
      version: 3,
      entries: [
        { channel_id: 8, enabled: true },
        { channel_id: 2, enabled: false },
      ],
    })
    await tokenRoutingApi.save(7, { ...route, mode: 'auto' })
    expect(mocks.put).toHaveBeenLastCalledWith('/api/token/7/routing', {
      mode: 'auto',
      version: 3,
      entries: [],
    })
  })
  it('preserves a route version conflict for the editor to require reload', async () => {
    const error = Object.assign(new Error('conflict'), {
      status: 409,
      code: 'ROUTING_VERSION_CONFLICT',
    })
    mocks.put.mockRejectedValue(error)
    await expect(
      tokenRoutingApi.save(7, { ...route, mode: 'manual' })
    ).rejects.toBe(error)
  })
  it('previews saved eligibility through a read-only request with cancellation', async () => {
    const signal = new AbortController().signal
    mocks.get.mockResolvedValue({
      mode: 'auto',
      version: 3,
      model: 'gpt',
      path: '/v1/responses',
      groups: ['default'],
      entries: [
        { channel_id: 8, eligible: false, reason: 'protocol_unavailable' },
      ],
    })
    expect(
      (await tokenRoutingApi.preview(7, 'gpt', '/v1/responses', signal))
        .entries[0]?.eligible
    ).toBe(false)
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/token/7/routing/preview',
      { model: 'gpt', path: '/v1/responses' },
      { signal }
    )
    expect(mocks.post).not.toHaveBeenCalled()
  })
  it.each(['alipay', 'stripe', 'waffo', 'waffo_pancake', 'gmpay:usdt.tron'])(
    'uses the native amount endpoint for %s',
    async (method) => {
      mocks.request.mockResolvedValue({ message: 'success', data: '73.00' })
      const signal = new AbortController().signal
      expect(await topupApi.quote(method, 10, signal)).toBe('73.00')
      const provider = paymentProvider(method)
      const prefix =
        provider === 'gmpay' || provider === 'epay'
          ? ''
          : `${provider.replaceAll('_', '-')}/`
      expect(mocks.request).toHaveBeenCalledWith(
        'POST',
        `/api/user/${prefix}amount`,
        { data: { amount: 10, payment_method: method }, signal }
      )
    }
  )
  it('keeps native payment errors visible and forwards only the selected payment payload', async () => {
    mocks.request.mockResolvedValue({ message: 'payment unavailable' })
    await expect(
      topupApi.pay('gmpay:usdt.tron', { amount: 10 })
    ).rejects.toThrow('payment unavailable')
    expect(mocks.request).toHaveBeenCalledWith('POST', '/api/user/gmpay/pay', {
      data: { amount: 10, payment_method: 'gmpay:usdt.tron' },
      signal: undefined,
    })
  })
  it('offers only enabled gateways and adds the native Creem product flow', () => {
    const info = {
      pay_methods: [
        { type: 'alipay', name: 'Alipay' },
        { type: 'gmpay:usdt.tron', name: 'USDT' },
        { type: 'stripe', name: 'Stripe' },
      ],
      enable_online_topup: false,
      enable_gmpay_topup: true,
      enable_stripe_topup: false,
      enable_creem_topup: true,
    } as TopupInfo
    expect(enabledPayMethods(info).map((m) => m.type)).toEqual([
      'gmpay:usdt.tron',
      'creem',
    ])
  })
  it('keeps EPay form submission distinct from hosted checkout and rejects unsafe destinations', () => {
    expect(
      checkoutTarget('alipay', {
        url: 'https://pay.example/submit',
        data: { trade_no: 'order-1' },
      })
    ).toEqual({
      url: 'https://pay.example/submit',
      form: { trade_no: 'order-1' },
    })
    expect(
      checkoutTarget('stripe', {
        data: { pay_link: 'https://checkout.example/session' },
      }).form
    ).toBeNull()
    expect(() =>
      checkoutTarget('stripe', { data: { pay_link: 'javascript:alert(1)' } })
    ).toThrow()
    expect(() =>
      checkoutTarget('alipay', {
        url: 'https://pay.example',
        data: { invalid: {} },
      })
    ).toThrow()
  })
  it('aggregates native usage without inventing latency or success metrics', () => {
    const time = new Date(2026, 9, 17, 9).getTime() / 1000
    const stats = aggregateUsage(
      [
        {
          model_name: 'a',
          created_at: time,
          count: 2,
          quota: '500000',
          token_used: 300,
        },
        {
          model_name: 'a',
          created_at: time,
          count: 1,
          quota: '250000',
          token_used: 10,
        },
        {
          model_name: 'b',
          created_at: time + 86400,
          count: 1,
          quota: '250000',
          token_used: 90,
        },
      ],
      [
        {
          model_name: 'a',
          created_at: time - 86400,
          count: 2,
          quota: '500000',
          token_used: 100,
        },
      ]
    )
    expect(stats.kpi).toEqual({
      totalQuota: '1000000',
      totalRequests: 4,
      totalTokens: 400,
    })
    expect(stats.models.find((m) => m.model === 'a')).toMatchObject({
      quota: '750000',
      requests: 3,
      share: 0.75,
    })
    expect(stats.flow).toHaveLength(2)
    expect(stats.comparison).toEqual({ quotaDelta: 100, requestsDelta: 100 })
    expect(stats.kpi).not.toHaveProperty('successRate')
    expect(aggregateUsage([]).comparison).toEqual({
      quotaDelta: null,
      requestsDelta: null,
    })
  })
})

const nativeInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  enable_creem_topup: false,
  enable_waffo_topup: false,
  enable_waffo_pancake_topup: false,
  enable_gmpay_topup: false,
  enable_redemption: false,
  pay_methods: [],
  amount_options: [1, 10],
  min_topup: 1,
  stripe_min_topup: 1,
  waffo_min_topup: 1,
  waffo_pancake_min_topup: 1,
  waffo_pay_methods: null,
  creem_products: '',
  topup_link: '',
  discount: { '10': 0.95 },
}
it('keeps disabled native gateways disabled and validates public recharge configuration', () => {
  expect(parseTopupInfo(nativeInfo)).toEqual(nativeInfo)
  for (const bad of [
    { ...nativeInfo, enable_gmpay_topup: 'true' },
    { ...nativeInfo, discount: { '10': -1 } },
    { ...nativeInfo, amount_options: [1.1] },
  ])
    expect(() => parseTopupInfo(bad)).toThrow()
  expect(() =>
    parseCreemProducts(
      '[{"productId":"a","name":"Credits","price":10,"currency":"USD","quota":"500000"}]'
    )
  ).toThrow()
  expect(
    parseCreemProducts(
      '[{"productId":"a","name":"Credits","price":10,"currency":"USD","quota":500000}]'
    )[0]?.quota
  ).toBe(500000)
})

it('reads native Waffo catalog success and preserves partial pair failure details', async () => {
  mocks.request.mockResolvedValueOnce({
    message: 'success',
    data: { stores: [] },
  })
  expect(
    (
      await paymentRequest(
        '/api/option/waffo-pancake/catalog',
        undefined,
        undefined,
        'GET'
      )
    ).data
  ).toEqual({ stores: [] })
  expect(mocks.request).toHaveBeenLastCalledWith(
    'GET',
    '/api/option/waffo-pancake/catalog',
    { data: undefined, signal: undefined }
  )
  mocks.request.mockResolvedValueOnce({
    message: 'error',
    data: { orphan_store: true, store_id: 'orphan' },
  })
  await expect(
    paymentRequest('/api/option/waffo-pancake/pair', {})
  ).rejects.toMatchObject({
    business: true,
    data: { orphan_store: true, store_id: 'orphan' },
  })
})
