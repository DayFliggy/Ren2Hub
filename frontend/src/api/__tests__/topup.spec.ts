import { afterEach, describe, expect, it, vi } from 'vitest'
import { httpTransport } from '../httpTransport'
import {
  checkoutTarget,
  parseCreationFailure,
  parseGmpayConfig,
  parseTopupInfo,
  paymentRequest,
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
      .mockResolvedValue({ message: 'success', data: '73.00' } as never)
    expect(await topupApi.quote('gmpay:usdt.tron', 10)).toBe('73.00')
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
  it('preserves a durable failed order while retaining business failure', async () => {
    vi.spyOn(httpTransport, 'request').mockResolvedValue({
      message: 'error',
      data: 'Creation failed',
      trade_no: 'GPtest',
      creation_status: 'unknown',
      merchant_id: 'private',
    } as never)
    await expect(
      paymentRequest('/api/user/gmpay/pay', {})
    ).rejects.toMatchObject({
      business: true,
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
