import { describe, expect, it } from 'vitest'
import { parseTopUp, parseTopUpDetail } from '../adminOrders'
import { formatAdminOrderAmount } from '@/constants/adminOrders'
const order = {
  id: 1,
  user_id: 2,
  trade_no: 'fixture-order',
  payment_provider: 'gmpay',
  payment_method: 'gmpay:usdt.tron',
  create_time: 1,
  complete_time: 2,
  status: 'success',
  currency: 'CNY',
  pay_amount: '7.23',
  expected_quota: 500000,
  credited_quota: 500000,
  affiliate_base_quota: 500000,
}
describe('native recharge ledger', () => {
  it('projects native settled status and authoritative credited quota', () => {
    expect(parseTopUp(order)).toMatchObject({
      order_no: 'fixture-order',
      status: 'completed',
      currency: 'CNY',
      amount: 7.23,
      quota: '500000',
    })
    expect(
      parseTopUp({ ...order, status: 'expired', credited_quota: 0 })
    ).toMatchObject({ status: 'expired', quota: '0' })
  })
  it('does not label an unknown historical currency as USD', () => {
    const legacy = parseTopUp({ ...order, currency: '', pay_amount: '' })
    expect(formatAdminOrderAmount(legacy.amount, legacy.currency)).toBe('—')
    expect(formatAdminOrderAmount(1, 'EUR')).toBe('EUR 1.00')
  })
  it('retains the requested GMPay network separately from settlement evidence', () => {
    const result = parseTopUpDetail(
      {
        ...order,
        gmpay: {
          status: 'settled',
          last_failure: '',
          gateway_trade_no: 'gateway-fixture',
          token: 'usdt',
          network: 'tron',
          gateway_crypto_amount: '1',
          receive_address: 'fixture-address',
          settled_crypto_amount: '1',
          settled_receive_address: 'fixture-address',
          settled_transaction_id: 'fixture-transaction',
        },
      },
      'fixture-order'
    )
    expect(result.state.status).toBe('completed')
    expect(result.evidence?.network).toBe('tron')
    expect(result.evidence).not.toHaveProperty('verified_network')
  })
})

it.each(['not-a-number', '-1', 'Infinity'])(
  'rejects invalid native paid amount %s',
  (pay_amount) => {
    expect(() => parseTopUp({ ...order, pay_amount })).toThrow(
      'Invalid API response'
    )
  }
)
