import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
  parseCreatedTokenId,
  parseTokenRoute,
  tokenRoutingApi,
  type TokenRoute,
} from '@/api/tokenRouting'

vi.mock('@/api/client', () => ({ api: { get: vi.fn(), put: vi.fn() } }))
beforeEach(() => vi.resetAllMocks())

const route = (): TokenRoute => ({
  token_id: 7,
  mode: 'ordered',
  selected_only: true,
  version: 4,
  max_channel_ratio: '1.25',
  entries: [
    { channel_id: 9, enabled: true, position: 0 },
    { channel_id: 2, enabled: false, position: 1 },
  ],
  route_scope: 'selected',
  effective_mode: 'ordered',
  enabled_route_channel_count: 1,
})

describe('native token routing', () => {
  it('saves the ordered list, scope and version without retired groups or policies', async () => {
    vi.mocked(api.put).mockResolvedValue({ ...route(), version: 5 })
    const result = await tokenRoutingApi.save(7, route())
    expect(api.put).toHaveBeenCalledWith('/api/token/7/routing', {
      mode: 'ordered',
      selected_only: true,
      version: 4,
      max_channel_ratio: '1.25',
      entries: [
        { channel_id: 9, enabled: true },
        { channel_id: 2, enabled: false },
      ],
    })
    expect(result.version).toBe(5)
    expect(result.route_scope).toBe('selected')
    expect(result.effective_mode).toBe('ordered')
  })

  it('keeps selected channels (including paused ones) when automatic routing is limited to them', async () => {
    vi.mocked(api.put).mockResolvedValue({
      ...route(),
      mode: 'auto',
      effective_mode: 'auto',
    })
    await tokenRoutingApi.save(7, { ...route(), mode: 'auto' })
    expect(api.put).toHaveBeenCalledWith('/api/token/7/routing', {
      mode: 'auto',
      selected_only: true,
      version: 4,
      max_channel_ratio: '1.25',
      entries: [
        { channel_id: 9, enabled: true },
        { channel_id: 2, enabled: false },
      ],
    })
  })

  it('sends no entries when automatic routing uses every platform channel', async () => {
    vi.mocked(api.put).mockResolvedValue({
      ...route(),
      mode: 'auto',
      selected_only: false,
      entries: [],
      route_scope: 'platform',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })
    await tokenRoutingApi.save(7, {
      ...route(),
      mode: 'auto',
      selected_only: false,
    })
    expect(api.put).toHaveBeenCalledWith(
      '/api/token/7/routing',
      expect.objectContaining({
        mode: 'auto',
        selected_only: false,
        entries: [],
      })
    )
  })

  it('parses orphaned empty scope and all-paused platform fallback accurately', () => {
    const orphaned = parseTokenRoute({
      token_id: 7,
      mode: 'auto',
      selected_only: true,
      version: 2,
      max_channel_ratio: null,
      entries: [],
    })
    expect(orphaned).toMatchObject({
      mode: 'auto',
      selected_only: true,
      route_scope: 'empty',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })

    const allPaused = parseTokenRoute({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 3,
      max_channel_ratio: null,
      entries: [{ channel_id: 9, enabled: false, position: 0 }],
    })
    expect(allPaused).toMatchObject({
      mode: 'ordered',
      selected_only: true,
      route_scope: 'platform',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })
  })

  it('rejects an old profile, invalid mode, invalid scope or non-numeric version', () => {
    expect(() => parseTokenRoute({ groups: [] })).toThrow()
    expect(() => parseTokenRoute({ ...route(), mode: 'priority' })).toThrow()
    expect(() => parseTokenRoute({ ...route(), mode: 'manual' })).toThrow()
    expect(() =>
      parseTokenRoute({ ...route(), selected_only: undefined })
    ).toThrow()
    expect(() => parseTokenRoute({ ...route(), selected_only: 1 })).toThrow()
    expect(() => parseTokenRoute({ ...route(), version: '4' })).toThrow()
    expect(() =>
      parseTokenRoute({ ...route(), route_scope: 'unknown' })
    ).toThrow()
  })

  it('fetches token-less candidate channels with draft model limits and ratio cap', async () => {
    vi.mocked(api.get).mockResolvedValue([
      {
        id: 11,
        name: 'Channel A',
        type: 1,
        status: 1,
        models: ['gpt-4o'],
        channel_ratio: '1.2',
        max_concurrency: 10,
        eligible: true,
      },
    ])
    const signal = new AbortController().signal
    const result = await tokenRoutingApi.candidates(
      { model_limits: ['gpt-4o', 'claude-4'], max_channel_ratio: '1.5' },
      signal
    )
    expect(api.get).toHaveBeenCalledWith(
      '/api/token/routing/channels',
      { model_limits: 'gpt-4o,claude-4', max_channel_ratio: '1.5' },
      { signal }
    )
    expect(result).toHaveLength(1)
    expect(result[0]!.id).toBe(11)
  })

  it('parses created token id from POST /api/token/ response', () => {
    expect(parseCreatedTokenId({ id: 42 })).toBe(42)
    expect(parseCreatedTokenId(null)).toBeNull()
    expect(() => parseCreatedTokenId({ id: '42' })).toThrow()
  })
})
