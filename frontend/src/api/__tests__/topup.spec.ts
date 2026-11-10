import { afterEach, describe, expect, it, vi } from 'vitest'
import quoteResponse from '../../../tests/fixtures/native/topup-quote.post.json'
import quoteChangedResponse from '../../../tests/fixtures/native/topup-quote-changed.post.json'
import { httpTransport } from '../httpTransport'
import {
  checkoutTarget,
  parseCreationFailure,
  parseGmpayConfig,
  parseTopupInfo,
  parseTopupQuote,
  parseWaffoPancakePairFailure,
  paymentRequest,
  quoteChangedFrom,
  topupApi,
} from '../topup'
import { topupInputAmounts, validTopupAmount } from '@/utils/topupAmount'
import { nativeTopupInfo } from './topupFixtures'

afterEach(() => vi.restoreAllMocks())

describe('native payment contracts', () => {
  it('reads a native payment catalog with GET and no credentials in the query', async () => {
    const request = vi.spyOn(httpTransport, 'request').mockResolvedValue({
      message: 'success',
      data: { stores: [] },
    } as never)
    await expect(
      paymentRequest(
        '/api/option/waffo-pancake/catalog',
        undefined,
        undefined,
        'GET'
      )
    ).resolves.toEqual({ data: { stores: [] } })
    expect(request).toHaveBeenCalledWith(
      'GET',
      '/api/option/waffo-pancake/catalog',
      {
        data: undefined,
        signal: undefined,
      }
    )
  })
  it('uses the amount endpoint and keeps the complete asset and network method', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue(quoteResponse as never)
    expect(await topupApi.quote('gmpay:usdt.tron', 10)).toEqual({
      pay_amount: '693.50',
      quote: { ...quoteResponse.quote, expected_quota: '50000000' },
    })
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/amount', {
      data: { amount: 10, payment_method: 'gmpay:usdt.tron' },
      signal: undefined,
    })
    request.mockResolvedValue({
      message: 'success',
      url: 'https://cashier.example/pay',
      data: { trade_no: 'GPtest', currency: 'CNY' },
    } as never)
    await topupApi.pay('gmpay:usdt.tron', { amount: 10 })
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/gmpay/pay', {
      data: { amount: 10, payment_method: 'gmpay:usdt.tron' },
      signal: undefined,
    })
    expect(request).toHaveBeenCalledTimes(2)
  })
  it('requires a server quote matching data from discounted providers only', async () => {
    const request = vi.spyOn(httpTransport, 'request')
    for (const [method, path] of [
      ['alipay', '/api/user/amount'],
      ['waffo', '/api/user/waffo/amount'],
      ['waffo_pancake', '/api/user/waffo-pancake/amount'],
    ] as const) {
      request.mockResolvedValue({ message: 'success', data: '73.00' } as never)
      await expect(topupApi.quote(method, 10)).rejects.toMatchObject({
        code: 'INVALID_RESPONSE',
      })
      request.mockResolvedValue({ ...quoteResponse, data: '730.00' } as never)
      await expect(topupApi.quote(method, 10)).rejects.toMatchObject({
        code: 'INVALID_RESPONSE',
      })
      expect(request).toHaveBeenLastCalledWith('POST', path, expect.anything())
    }
    // Stripe charges its remote price and is never discounted.
    request.mockResolvedValue({ message: 'success', data: '10.00' } as never)
    expect(await topupApi.quote('stripe', 10)).toEqual({
      pay_amount: '10.00',
      quote: null,
    })
    expect(request).toHaveBeenLastCalledWith(
      'POST',
      '/api/user/stripe/amount',
      expect.anything()
    )
  })
  it('reads every quote field exactly and rejects malformed quotes', () => {
    const endpoint = '/api/user/amount'
    const quote = { ...quoteResponse.quote, winner_window: 'week' }
    expect(parseTopupQuote(quote, endpoint)).toEqual({
      ...quote,
      expected_quota: '50000000',
    })
    for (const patch of [
      { expected_quota: '50000000' },
      { expected_quota: 0 },
      { expected_quota: 1.5 },
      { currency: 'cny' },
      { currency: '' },
      { pay_amount: 693.5 },
      { pay_amount: '0' },
      { pay_amount: '-1' },
      { list_pay_amount: '1e3' },
      { applied_rate: 0.05 },
      { factor: undefined },
      { cap_applied: 'false' },
      { winner_window: 'year' },
    ])
      expect(() => parseTopupQuote({ ...quote, ...patch }, endpoint)).toThrow(
        `Invalid API response: ${endpoint}`
      )
  })
  it('keeps the code and new quote of a changed price, and only then', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue(quoteChangedResponse as never)
    const error = await topupApi
      .pay('alipay', {
        amount: 10,
        expected_pay_amount: '730.00',
        expected_currency: 'CNY',
      })
      .catch((cause: unknown) => cause)
    expect(request).toHaveBeenLastCalledWith('POST', '/api/user/pay', {
      data: {
        amount: 10,
        expected_pay_amount: '730.00',
        expected_currency: 'CNY',
        payment_method: 'alipay',
      },
      signal: undefined,
    })
    expect(error).toMatchObject({ business: true, code: 'topup_quote_changed' })
    expect(quoteChangedFrom(error)).toEqual({
      ...quoteChangedResponse.quote,
      expected_quota: '50000000',
    })
    request.mockResolvedValue({
      ...quoteChangedResponse,
      quote: { ...quoteChangedResponse.quote, currency: 'cny' },
    } as never)
    await expect(topupApi.pay('alipay', { amount: 10 })).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
    request.mockResolvedValue({
      message: 'error',
      data: 'Top-up discount is temporarily unavailable, please try again',
      message_key:
        'Top-up discount is temporarily unavailable, please try again',
    } as never)
    const unavailable = await topupApi
      .pay('alipay', { amount: 10 })
      .catch((cause: unknown) => cause)
    expect(unavailable).toMatchObject({ business: true, code: undefined })
    expect(quoteChangedFrom(unavailable)).toBeNull()
  })
  it('preserves a durable failed order while retaining business failure', async () => {
    // controller/gmpay.go gmpayCreationError
    vi.spyOn(httpTransport, 'request').mockResolvedValue({
      message: 'error',
      data: 'Failed to create the payment order',
      message_key: 'Failed to create the payment order',
      trade_no: 'GPtest',
      creation_status: 'unknown',
    } as never)
    await expect(
      paymentRequest('/api/user/gmpay/pay', {})
    ).rejects.toMatchObject({
      business: true,
      message: 'Failed to create the payment order',
      data: { trade_no: 'GPtest', creation_status: 'unknown' },
    })
    expect(
      parseCreationFailure({
        trade_no: 'GPtest',
        creation_status: 'settled',
        secret: 'private',
      })
    ).toEqual({ trade_no: 'GPtest', creation_status: 'settled' })
  })
  it('passes the Pancake pair failure reason and orphan store through', async () => {
    // controller/topup_waffo_pancake.go CreateWaffoPancakePair
    const request = vi.spyOn(httpTransport, 'request').mockResolvedValue({
      message: 'error',
      data: {
        error: 'product create failed',
        store_id: 'store-1',
        store_name: 'Ren2Hub',
        orphan_store: true,
      },
    } as never)
    await expect(
      paymentRequest('/api/option/waffo-pancake/pair', {})
    ).rejects.toMatchObject({
      business: true,
      message: 'product create failed',
      data: {
        error: 'product create failed',
        orphan_store: true,
        store_id: 'store-1',
        store_name: 'Ren2Hub',
      },
    })
    request.mockResolvedValue({
      message: 'error',
      data: { error: 'store create failed' },
    } as never)
    const failure = await paymentRequest(
      '/api/option/waffo-pancake/pair',
      {}
    ).catch((error: unknown) => error)
    expect(failure).toMatchObject({ message: 'store create failed' })
    expect((failure as { data: unknown }).data).toEqual({
      error: 'store create failed',
    })
    for (const data of [
      { error: '' },
      { error: 'failed', orphan_store: false },
      { error: 'failed', orphan_store: true, store_name: 'Ren2Hub' },
    ])
      expect(parseWaffoPancakePairFailure(data)).toBeUndefined()
  })
  it.each([{}, { success: true, data: '73.00' }, { message: 'success' }, null])(
    'rejects a non-payment envelope %j',
    async (value) => {
      vi.spyOn(httpTransport, 'request').mockResolvedValue(value as never)
      await expect(
        paymentRequest('/api/user/amount', {})
      ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
    }
  )
  it('accepts an asset array and has_secret without reading a secret', () => {
    const value = {
      pay_address: 'https://gateway.example',
      merchant_id: 'test',
      has_secret: true,
      enabled: false,
      assets: [],
    }
    expect(parseGmpayConfig(value)).toEqual(value)
    expect(() => parseGmpayConfig({ ...value, assets: '[]' })).toThrow()
    expect(() => parseGmpayConfig({ ...value, has_secret: 'true' })).toThrow()
  })
  it('keeps token input quantities in whole native recharge units', () => {
    const inputs = topupInputAmounts(
      parseTopupInfo(nativeTopupInfo),
      'gmpay:usdt.tron',
      { quota_display_type: 'TOKENS', quota_per_unit: 500000 }
    )
    expect(inputs?.minimum).toBe(500000)
    expect(inputs?.presets).toEqual([5000000, 10000000])
    expect(validTopupAmount(750001, inputs)).toBe(false)
    expect(validTopupAmount(1000000, inputs)).toBe(true)
  })
  it('requires HTTPS for crypto cashier redirects and string-only EPay form fields', () => {
    expect(() =>
      checkoutTarget('gmpay:usdt.tron', {
        url: 'http://cashier.example/pay',
        data: {},
      })
    ).toThrow()
    expect(() =>
      checkoutTarget('alipay', {
        url: 'https://cashier.example/pay',
        data: { amount: 73 },
      })
    ).toThrow()
    expect(
      checkoutTarget('alipay', {
        url: 'https://cashier.example/pay',
        data: { amount: '73.00' },
      }).form
    ).toEqual({ amount: '73.00' })
  })
})
