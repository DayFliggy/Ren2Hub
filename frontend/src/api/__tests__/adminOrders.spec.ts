import { describe, expect, it } from 'vitest'
import { parseGmpayOrderEvidence } from '../adminOrders'

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
