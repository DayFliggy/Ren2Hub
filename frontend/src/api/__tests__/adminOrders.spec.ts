import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import {
  parseTopUp,
  parseTopUpDetail,
  parseTopUpRanking,
  parseTopUpStatistics,
  reverseTopUpAffiliateReward,
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
  it.each([
    ['stripe', 'stripe', 'stripe'],
    ['', 'stripe', 'stripe'],
    ['epay', 'alipay', 'epay'],
    ['', 'alipay', 'other'],
    ['paypal', 'stripe', 'other'],
    ['', '', 'other'],
  ])(
    'classifies provider %j with payment_method %j as %s like the backend filter',
    (provider, method, expected) => {
      const order = parseTopUp({
        ...nativeTopUp,
        payment_provider: provider,
        payment_method: method,
      })
      expect(order.method).toBe(expected)
      expect(order.payment_method).toBe(method)
    }
  )
  it('validates the order identity, excludes merchant fields, and keeps exact crypto receipts', () => {
    const detail = parseTopUpDetail(
      {
        ...nativeTopUp,
        username: 'payer',
        email: 'payer@example.test',
        user_deleted: false,
        gmpay: {
          ...nativeGmpayEvidence,
          settled_crypto_amount: '0.123456789012345678',
          merchant_key: 'private',
        },
      },
      'GPtest'
    )
    expect(detail.state.email).toBe('payer@example.test')
    expect(detail.userDeleted).toBe(false)
    expect(detail.evidence?.settled_crypto_amount).toBe('0.123456789012345678')
    expect(detail.evidence).not.toHaveProperty('merchant_key')
    expect(() =>
      parseTopUpDetail(
        { ...nativeTopUp, gmpay: nativeGmpayEvidence },
        'GPother'
      )
    ).toThrow()
  })
  it('reads the referral reward only where the administrator detail sends one', () => {
    const base = { ...nativeTopUp, user_deleted: false }
    // The owner's own detail has no field; an order without a reward has null.
    expect(parseTopUpDetail(base, 'GPtest').affiliate).toBeNull()
    expect(
      parseTopUpDetail({ ...base, affiliate: null }, 'GPtest').affiliate
    ).toBeNull()
    expect(
      parseTopUpDetail(
        {
          ...base,
          affiliate: { inviter_id: 12, reward_quota: 200000, reversed: false },
        },
        'GPtest'
      ).affiliate
    ).toEqual({ inviter_id: 12, reward_quota: '200000', reversed: false })
    for (const affiliate of [
      { inviter_id: 0, reward_quota: 200000, reversed: false },
      { inviter_id: 12, reward_quota: 0, reversed: false },
      { inviter_id: 12, reward_quota: 200000 },
      'paid',
    ])
      expect(() => parseTopUpDetail({ ...base, affiliate }, 'GPtest')).toThrow()
  })
  it('keeps the order of a hard-deleted owner and requires the deleted flag', () => {
    // model.GetTopUpDetail keeps user_id and leaves the names empty.
    const orphan = {
      ...nativeTopUp,
      payment_provider: 'stripe',
      payment_method: 'stripe',
      username: '',
      email: '',
      gmpay: null,
    }
    const detail = parseTopUpDetail({ ...orphan, user_deleted: true }, 'GPtest')
    expect(detail.userDeleted).toBe(true)
    expect(detail.state.user_id).toBe(7)
    expect(() => parseTopUpDetail(orphan, 'GPtest')).toThrow()
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
        { ...total, method: 'other', paid_amount: '5.00' },
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
      ['other', '5.00'],
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

describe('referral reward reversal', () => {
  afterEach(() => vi.restoreAllMocks())
  it('posts the order number and reads how the reward was taken back', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({
        record: { id: 9, kind: 'reversal' },
        replayed: false,
        reversed_quota: 200000,
        revoked_quota: 150000,
        debt_added_quota: 50000,
      })
      .mockResolvedValueOnce({ replayed: true, reversed_quota: 200000 })
    expect(await reverseTopUpAffiliateReward('GPtest')).toEqual({
      replayed: false,
      reversed_quota: '200000',
      revoked_quota: '150000',
      debt_added_quota: '50000',
    })
    expect(post).toHaveBeenCalledWith(
      '/api/user/topup/admin/affiliate-reversal',
      { trade_no: 'GPtest' }
    )
    // A success must say where the reward went.
    await expect(reverseTopUpAffiliateReward('GPtest')).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
  })
})
