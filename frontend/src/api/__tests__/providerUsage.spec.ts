import { beforeEach, describe, expect, it, vi } from 'vitest'
import { parseProviderUsage, providerUsageApi } from '@/api/providerUsage'

const transport = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/api/client', () => ({ api: transport }))

const snapshot = {
  channel_id: 71,
  provider: 'codex',
  state: 'known',
  plan_type: 'plus',
  windows: [
    {
      name: 'rate_limit_primary',
      used_percent: 0,
      limit_seconds: 18000,
      reset_at: null,
    },
  ],
  fetched_at: 100,
  expires_at: 400,
  last_attempt_at: 100,
  next_refresh_at: 160,
  upstream_status: 200,
  error_code: '',
  auto_refresh: false,
}

describe('provider usage projection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })
  it('distinguishes unknown from zero and rejects malformed or incomplete snapshots', () => {
    expect(parseProviderUsage(snapshot).windows[0]?.used_percent).toBe(0)
    expect(
      parseProviderUsage({ ...snapshot, state: 'unknown', windows: [] }).windows
    ).toEqual([])
    expect(
      parseProviderUsage({
        ...snapshot,
        windows: [{ ...snapshot.windows[0], used_percent: null }],
      }).windows[0]?.used_percent
    ).toBeNull()
    for (const change of [
      { state: 'available' },
      { auto_refresh: undefined },
      { next_refresh_at: -1 },
      { provider: 'grok' },
      { windows: [{ ...snapshot.windows[0], used_percent: 101 }] },
      { windows: [{ ...snapshot.windows[0], used_percent: undefined }] },
      { windows: [{ ...snapshot.windows[0], reset_at: '500' }] },
      { windows: [snapshot.windows[0], snapshot.windows[0]] },
    ])
      expect(() => parseProviderUsage({ ...snapshot, ...change })).toThrow()
  })
  it('keeps fetching separate from reading and carries cancellation to all calls', async () => {
    const signal = new AbortController().signal
    transport.get.mockResolvedValue(snapshot)
    transport.post.mockResolvedValue(snapshot)
    transport.put.mockResolvedValue({ ...snapshot, auto_refresh: true })
    await providerUsageApi.get(71, signal)
    expect(transport.get).toHaveBeenCalledWith(
      '/api/channel/71/provider-usage',
      undefined,
      { signal }
    )
    expect(transport.post).not.toHaveBeenCalled()
    await providerUsageApi.refresh(71, signal)
    expect(transport.post).toHaveBeenCalledWith(
      '/api/channel/71/provider-usage/refresh',
      undefined,
      { signal }
    )
    expect(
      (await providerUsageApi.settings(71, true, signal)).auto_refresh
    ).toBe(true)
    expect(transport.put).toHaveBeenCalledWith(
      '/api/channel/71/provider-usage/settings',
      { auto_refresh: true },
      { signal }
    )
  })
  it('does not project provider credentials or arbitrary upstream fields', () => {
    expect(
      parseProviderUsage({
        ...snapshot,
        access_token: 'private',
        account_id: 'private',
        raw: {},
      })
    ).not.toHaveProperty('access_token')
  })
})
