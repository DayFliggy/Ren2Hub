import { describe, expect, it } from 'vitest'
import {
  parseTopUp,
  parseTopUpDetail,
  parseTopUpRanking,
  parseTopUpStatistics,
} from '../adminOrders'
import { nativeTopUp, nativeGmpayEvidence } from './topupFixtures'

describe('native order contracts', () => {
  it('projects native success and credited quota without inventing missing payment facts', () => {
    const order = parseTopUp({
      ...nativeTopUp,
      status: 'success',
      credited_quota: 5000000,
      complete_time: 1792368100,
    })
    expect(order).toMatchObject({
      order_no: 'GPtest',
      method: 'gmpay',
      currency: 'CNY',
      amount: 73,
      quota: '5000000',
      status: 'completed',
    })
    const legacy = parseTopUp({ ...nativeTopUp, currency: '', pay_amount: '' })
    expect(legacy.currency).toBe('')
    expect(legacy.amount).toBeNaN()
  })
  it('validates the order identity, excludes merchant fields, and keeps exact crypto receipts', () => {
    const detail = parseTopUpDetail(
      {
        ...nativeTopUp,
        username: 'payer',
        email: 'payer@example.test',
        gmpay: {
          ...nativeGmpayEvidence,
          settled_crypto_amount: '0.123456789012345678',
          merchant_key: 'private',
        },
      },
      'GPtest'
    )
    expect(detail.state.email).toBe('payer@example.test')
    expect(detail.evidence?.settled_crypto_amount).toBe('0.123456789012345678')
    expect(detail.evidence).not.toHaveProperty('merchant_key')
    expect(() =>
      parseTopUpDetail(
        { ...nativeTopUp, gmpay: nativeGmpayEvidence },
        'GPother'
      )
    ).toThrow()
  })
  it.each([
    { credited_quota: '5000000' },
    { credited_quota: Number.MAX_SAFE_INTEGER + 1 },
    { status: 'completed' },
    { pay_amount: 'NaN' },
  ])('rejects invalid native rows %j', (change) => {
    expect(() => parseTopUp({ ...nativeTopUp, ...change })).toThrow()
  })
  it('keeps currency totals separate and exposes unknown legacy amounts', () => {
    const total = {
      currency: 'CNY',
      count: 2,
      paid_count: 1,
      paid_amount: '73.00',
      credited_quota: 5000000,
      unknown_amount_count: 0,
    }
    const stats = parseTopUpStatistics({
      total: 3,
      currencies: [
        total,
        { ...total, currency: '', paid_amount: '0', unknown_amount_count: 1 },
      ],
      methods: [
        { ...total, method: 'epay' },
        { ...total, method: 'alipay', paid_amount: '5.00' },
      ],
      daily: [],
      start_time: 1,
      end_time: 2,
    })
    expect(stats.currencies.map((value) => value.currency)).toEqual(['CNY', ''])
    expect(stats.currencies[1]?.unknown_amount_count).toBe(1)
    expect(
      stats.methods.map((value) => [value.method, value.paid_amount])
    ).toEqual([
      ['epay', '73.00'],
      ['alipay', '5.00'],
    ])
    expect(() =>
      parseTopUpStatistics({
        ...stats,
        currencies: [{ ...total, paid_amount: 73 }],
      })
    ).toThrow()
    const withoutMethods: Record<string, unknown> = { ...stats }
    delete withoutMethods.methods
    expect(() => parseTopUpStatistics(withoutMethods)).toThrow()
  })
  it('reads the quota ranking without payment credentials and rejects malformed rows', () => {
    const ranking = parseTopUpRanking({
      items: [
        {
          user_id: 2,
          username: 'alice',
          paid_count: 3,
          credited_quota: 5000000,
        },
      ],
      start_time: 1,
      end_time: 2,
    })
    expect(ranking).toEqual([
      {
        user_id: 2,
        username: 'alice',
        paid_count: 3,
        credited_quota: '5000000',
      },
    ])
    expect(() =>
      parseTopUpRanking({
        items: [
          { user_id: 2, username: 'a', paid_count: -1, credited_quota: 1 },
        ],
      })
    ).toThrow()
    expect(() =>
      parseTopUpRanking({
        items: [
          { user_id: 2, username: 'a', paid_count: 1, credited_quota: '1' },
        ],
      })
    ).toThrow()
  })
})
