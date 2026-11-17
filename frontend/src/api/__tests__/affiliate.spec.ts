import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { affiliateApi } from '../affiliate'

const policy = {
  user_id: 8,
  code: 'INVITE',
  code_custom: false,
  enabled: false,
  rate_bps: null,
  effective_rate_bps: 2000,
  inviter_id: 0,
  invited_at: 0,
}
const info = {
  code: 'INVITE',
  code_enabled: true,
  invited: 4,
  effective_rate_bps: 2000,
  effective_rate_percent: 20,
  transferable: 3,
  frozen_reward: 0,
  reward_total: 7,
  monthly_series: [
    { month: '2026-10', new_count: 1, cumulative: 4, reward: 3 },
  ],
  policy: { enabled: true, freeze_hours: 24 },
}
afterEach(() => vi.restoreAllMocks())

describe('native affiliate API contracts', () => {
  it('keeps legacy metadata compatible and reads the transfer gate and outstanding offset', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce(info)
      .mockResolvedValueOnce({
        ...info,
        outstanding_offset: 12,
        transfer_enabled: false,
      })
    expect(await affiliateApi.info()).toMatchObject({
      outstanding_offset: '0',
      transfer_enabled: true,
    })
    expect(await affiliateApi.info()).toMatchObject({
      outstanding_offset: '12',
      transfer_enabled: false,
    })
  })
  it('reads reversal components and rejects private allocation rows', async () => {
    const row = {
      id: 1,
      user_id: 2,
      invitee_id: 3,
      kind: 'reversal',
      skip_reason: '',
      source_type: 'admin_quota_reversal',
      source_id: 'operation',
      base_quota: 100,
      rate_bps: 2000,
      reward_quota: 20,
      credited_quota: 5,
      debt_offset_quota: 3,
      debt_added_quota: 15,
      frozen_until: 0,
      thawed_at: 0,
      created_at: 1,
      available_after: 0,
      frozen_after: 0,
      wallet_after: 12,
    }
    const page = { items: [row], total: 1, page: 1, page_size: 20 }
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(page)
      .mockResolvedValueOnce({
        ...page,
        items: [
          {
            ...row,
            kind: 'accrue',
            credited_quota: undefined,
            debt_offset_quota: undefined,
            debt_added_quota: undefined,
          },
        ],
      })
      .mockResolvedValueOnce({
        ...page,
        items: [{ ...row, kind: 'transfer_allocation' }],
      })
    expect((await affiliateApi.records(1, 20)).items[0]).toMatchObject({
      kind: 'reversal',
      reward_quota: '20',
      credited_quota: '5',
      debt_offset_quota: '3',
      debt_added_quota: '15',
    })
    expect(get).toHaveBeenCalledWith(
      '/api/user/affiliate/records',
      { p: 1, page_size: 20 },
      { signal: undefined }
    )
    expect((await affiliateApi.records(1, 20)).items[0]).toMatchObject({
      credited_quota: '0',
      debt_offset_quota: '0',
      debt_added_quota: '0',
    })
    await expect(affiliateApi.records(1, 20)).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
  })
  it('asks for monthly series in the viewer timezone', async () => {
    vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(-480)
    const get = vi.spyOn(api, 'get').mockResolvedValue(info)
    const signal = new AbortController().signal
    await affiliateApi.info(signal)
    expect(get).toHaveBeenCalledWith(
      '/api/user/affiliate',
      { tz_offset: '-480' },
      { signal }
    )
  })
  it('validates native numeric quota and chart data at the API boundary', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce(info)
      .mockResolvedValueOnce({ ...info, transferable: '3' })
      .mockResolvedValueOnce({
        ...info,
        monthly_series: [{ month: '2026-10', reward: 3 }],
      })
      .mockResolvedValueOnce({
        ...info,
        transferable: Number.MAX_SAFE_INTEGER + 1,
      })
    expect(await affiliateApi.info()).toMatchObject({
      transferable: '3',
      monthly_series: [
        { month: '2026-10', new_count: 1, cumulative: 4, reward: '3' },
      ],
    })
    for (let i = 0; i < 3; i++)
      await expect(affiliateApi.info()).rejects.toMatchObject({
        code: 'INVALID_RESPONSE',
      })
  })
  it('uses server pagination and only exposes the masked invitee projection', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      items: [
        {
          masked_name: 'a***e',
          invited_at: 123,
          reward_quota: 3,
          id: 7,
          username: 'private',
          email: 'private@example.test',
        },
      ],
      page: 2,
      page_size: 20,
      total: 21,
    })
    const signal = new AbortController().signal
    expect(await affiliateApi.invitees(2, 20, 'asc', signal)).toEqual({
      items: [{ masked_name: 'a***e', invited_at: 123, reward_quota: '3' }],
      page: 2,
      pageSize: 20,
      total: 21,
    })
    expect(get).toHaveBeenCalledWith(
      '/api/user/affiliate/invitees',
      { p: 2, page_size: 20, order: 'asc' },
      { signal }
    )
  })
  it('sends only the operation ID for an idempotent full transfer', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      replayed: true,
      transferred_quota: 3,
      wallet_quota: 100,
    })
    expect(await affiliateApi.transfer('retry-operation')).toEqual({
      replayed: true,
      transferred_quota: '3',
      wallet_quota: '100',
    })
    expect(post).toHaveBeenCalledWith('/api/user/affiliate/transfer', {
      operation_id: 'retry-operation',
    })
  })
  it('uses PUT for policy updates and preserves disabled status when resetting a code', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(policy)
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ ...policy, code: 'NEWCODE' })
    expect((await affiliateApi.userPolicy(8)).enabled).toBe(false)
    const result = await affiliateApi.updateUserPolicy(8, {
      rate_bps: null,
      reset_code: true,
    })
    expect(result.enabled).toBe(false)
    expect(put).toHaveBeenCalledWith('/api/admin/affiliate/users/8', {
      rate_bps: null,
      reset_code: true,
    })
  })
})
