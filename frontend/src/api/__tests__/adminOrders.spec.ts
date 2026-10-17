import { describe, expect, it } from 'vitest'
import { parseGmpayOrderDetail, parseGmpayOrderEvidence } from '../adminOrders'

const evidence = {
  status: 'unknown',
  last_failure: 'TRANSPORT',
  token: 'usdt',
  network: 'tron',
  gateway_crypto_amount: '',
  receive_address: '',
  settled_crypto_amount: '',
  settled_receive_address: '',
  settled_transaction_id: '',
}

describe('GMPay administrator evidence', () => {
  it('preserves large exact quota and crypto values and excludes unrecognized fields', () => {
    const detail = parseGmpayOrderDetail(
      {
        order_no: 'GPtest',
        method: 'gmpay',
        status: 'completed',
        quota: '9007199254740993',
        paid_at: 1785114100,
        gmpay: {
          ...evidence,
          status: 'settled',
          settled_crypto_amount: '0.123456789012345678',
          merchant_key: 'must-not-be-exposed',
        },
      },
      'GPtest'
    )
    expect(detail.state).toEqual({
      status: 'completed',
      quota: '9007199254740993',
      paid_at: 1785114100,
    })
    expect(detail.evidence?.settled_crypto_amount).toBe('0.123456789012345678')
    expect(detail.evidence).not.toHaveProperty('merchant_key')
  })

  it.each([
    { quota: 500000 },
    { quota: '-1' },
    { paid_at: -1 },
    { status: 'unverified' },
  ])('rejects invalid authoritative order state %j', (overrides) => {
    expect(() =>
      parseGmpayOrderDetail(
        {
          order_no: 'GPtest',
          method: 'gmpay',
          status: 'pending',
          quota: '0',
          paid_at: 0,
          gmpay: evidence,
          ...overrides,
        },
        'GPtest'
      )
    ).toThrow()
  })

  it('accepts an unknown creation result without inventing a gateway trade number', () => {
    expect(
      parseGmpayOrderEvidence(
        { order_no: 'GPtest', method: 'gmpay', gmpay: evidence },
        'GPtest'
      )
    ).toEqual({ ...evidence, gateway_trade_no: '' })
  })

  it('rejects evidence for a different order or provider', () => {
    expect(() =>
      parseGmpayOrderEvidence(
        { order_no: 'GPother', method: 'gmpay', gmpay: evidence },
        'GPtest'
      )
    ).toThrow()
    expect(() =>
      parseGmpayOrderEvidence(
        { order_no: 'GPtest', method: 'epay', gmpay: evidence },
        'GPtest'
      )
    ).toThrow()
  })

  it('rejects malformed receipt fields without hiding their absence behind defaults', () => {
    expect(() =>
      parseGmpayOrderEvidence(
        {
          order_no: 'GPtest',
          method: 'gmpay',
          gmpay: { ...evidence, settled_crypto_amount: 10 },
        },
        'GPtest'
      )
    ).toThrow()
  })
})
