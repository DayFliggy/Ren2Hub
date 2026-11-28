import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import {
  parseUpstreamAccount,
  parseUpstreamBinding,
  parseUpstreamCapabilities,
  parseUpstreamRemoteKey,
  UpstreamRequestError,
  upstreamsApi,
} from '@/api/upstreams'
import fixture from '../../../tests/fixtures/native/upstreams.get.json'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
}))
beforeEach(() => vi.resetAllMocks())

const clone = <T>(value: T): T => structuredClone(value)

describe('upstream DTOs from the backend contract fixture', () => {
  it('parses the account list, detail, keys and bindings', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(clone(fixture.list.data))
    const [account] = await upstreamsApi.list()
    expect(account).toMatchObject({
      id: 4,
      platform: 'newapi',
      has_proxy: true,
      upstream_groups: [{ id: null, name: 'standard', ratio: '1.25' }],
      site_meta: { balance: '300', username: 'demo', quota_per_unit: null },
    })
    expect(parseUpstreamAccount(clone(fixture.detail.data))).toEqual(account)

    const newapiKey = parseUpstreamRemoteKey(
      clone(fixture.newapi_keys.data[0]),
      'keys'
    )
    expect(newapiKey).toMatchObject({
      remain_quota: 9000,
      used_quota: 1000,
      unlimited_quota: false,
      expired_time: -1,
      quota: null,
      expires_at: null,
    })
    const sub2apiKey = parseUpstreamRemoteKey(
      clone(fixture.sub2api_keys.data[0]),
      'keys'
    )
    expect(sub2apiKey).toMatchObject({
      quota: 30.5,
      quota_used: 4.75,
      expires_at: '2099-01-01T00:00:00Z',
      upstream_group_id: 7,
      remain_quota: null,
    })
    expect(
      parseUpstreamBinding(clone(fixture.bindings.data[0]), 'bindings')
    ).toMatchObject({ channel_id: 9, ratio_sync_factor: '1.25', stale: false })
  })

  it('parses probe capabilities with the NewAPI display settings', () => {
    expect(
      parseUpstreamCapabilities(clone(fixture.newapi_probe.data), 'probe')
    ).toMatchObject({
      platform: 'newapi',
      password_login_available: false,
      quota_display_type: 'CNY',
      quota_per_unit: 1000,
      usd_exchange_rate: 7,
      totp_enabled: null,
    })
    expect(
      parseUpstreamCapabilities(clone(fixture.sub2api_probe.data), 'probe')
    ).toMatchObject({ platform: 'sub2api', totp_enabled: true })
  })

  it.each([
    ['unknown platform', { platform: 'axonhub' }],
    ['stringly id', { id: '4' }],
    ['negative sync time', { last_sync_at: -1 }],
    ['non-numeric balance', { site_meta: { balance: 'lots' } }],
    ['zero quota unit', { site_meta: { balance: '1', quota_per_unit: 0 } }],
    ['bad group ratio', { upstream_groups: [{ name: 'g', ratio: 'x' }] }],
    ['missing subscriptions', { subscriptions: undefined }],
  ])('rejects an account with %s', (_, patch) => {
    const value = { ...clone(fixture.detail.data), ...patch }
    expect(() => parseUpstreamAccount(value)).toThrow(ApiError)
  })

  it('rejects a binding with a non-decimal factor', () => {
    const value = {
      ...clone(fixture.bindings.data[0]),
      ratio_sync_factor: '1e3',
    }
    expect(() => parseUpstreamBinding(value, 'bindings')).toThrow(
      'Invalid API response'
    )
  })

  it('never returns the raw secret fields of a key', () => {
    const value = { ...clone(fixture.newapi_keys.data[0]), key: 'sk-raw' }
    expect(parseUpstreamRemoteKey(value, 'keys')).not.toHaveProperty('key')
  })
})

describe('upstream requests', () => {
  it('sends batched channel ids and rejects oversized batches', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(clone(fixture.bindings.data))
    const result = await upstreamsApi.bindingsForChannels([9, 9, 12])
    expect(api.get).toHaveBeenCalledWith(
      '/api/admin/upstreams/bindings',
      { channel_ids: '9,12' },
      { signal: undefined }
    )
    expect(result.map((binding) => binding.channel_id)).toEqual([9])
    await expect(
      upstreamsApi.bindingsForChannels(
        Array.from({ length: 501 }, (_, i) => i + 1)
      )
    ).rejects.toThrow(RangeError)
    expect(await upstreamsApi.bindingsForChannels([])).toEqual([])
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('distinguishes an account from a 2FA challenge and nothing else', async () => {
    vi.mocked(api.post).mockResolvedValueOnce({ challenge: 'c1' })
    await expect(upstreamsApi.auth({ platform: 'sub2api' })).resolves.toEqual({
      kind: 'challenge',
      challenge: 'c1',
    })
    vi.mocked(api.post).mockResolvedValueOnce({ account_id: 4 })
    await expect(
      upstreamsApi.verify({ challenge: 'c1', code: '123456' })
    ).resolves.toEqual({ kind: 'account', account_id: 4 })
    vi.mocked(api.post).mockResolvedValueOnce({ account_id: 4, challenge: 'c' })
    await expect(upstreamsApi.auth({})).rejects.toThrow('Invalid API response')
  })

  it('requires null data from mutations', async () => {
    vi.mocked(api.patch).mockResolvedValueOnce({ ok: true })
    await expect(upstreamsApi.update(4, { name: 'x' })).rejects.toThrow(
      'Invalid API response'
    )
    vi.mocked(api.patch).mockResolvedValueOnce(null)
    await upstreamsApi.updateBinding(4, 9, { ratio_sync_enabled: true })
    expect(api.patch).toHaveBeenLastCalledWith(
      '/api/admin/upstreams/4/bindings/9',
      { ratio_sync_enabled: true },
      { signal: undefined }
    )
    vi.mocked(api.post).mockResolvedValueOnce(null)
    await upstreamsApi.reverifyBinding(4, 9)
    expect(api.post).toHaveBeenLastCalledWith(
      '/api/admin/upstreams/4/bindings/9/reverify',
      undefined,
      { signal: undefined, timeoutMs: 120_000 }
    )
  })

  it('keeps the business code and Retry-After of a 429 answer', async () => {
    const cause = { response: { headers: { 'retry-after': '30' } } }
    vi.mocked(api.post).mockRejectedValueOnce(
      new ApiError('slow down', {
        status: 429,
        code: 'upstream_rate_limited',
        cause,
      })
    )
    const error = await upstreamsApi.sync(4).catch((reason) => reason)
    expect(error).toBeInstanceOf(UpstreamRequestError)
    expect(error).toMatchObject({
      status: 429,
      code: 'upstream_rate_limited',
      retryAfter: 30,
    })
  })

  it('ignores Retry-After outside 429 and invalid ids before sending', async () => {
    vi.mocked(api.delete).mockRejectedValueOnce(
      new ApiError('busy', {
        status: 409,
        code: 'upstream_account_has_bindings',
        cause: { response: { headers: { 'retry-after': '5' } } },
      })
    )
    await expect(upstreamsApi.remove(4)).rejects.toMatchObject({
      code: 'upstream_account_has_bindings',
      retryAfter: null,
    })
    expect(() => upstreamsApi.remove(0)).toThrow(RangeError)
  })
})
