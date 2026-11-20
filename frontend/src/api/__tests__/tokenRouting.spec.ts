import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import {
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
})
const previewQuery = { model: 'fixture', path: '/v1/responses', stream: false }
const previewResponse = () => ({
  mode: 'ordered',
  version: 4,
  model: 'fixture',
  path: '/v1/responses',
  stream: true,
  ordering: 'ordered',
  transport: 'http',
  entries: [
    {
      channel_id: 9,
      eligible: false,
      reason: 'channel_saturated',
      channel_ratio: '1.25',
    },
  ],
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
  })
  it('keeps selected channels when automatic routing is limited to them', async () => {
    vi.mocked(api.put).mockResolvedValue({ ...route(), mode: 'auto' })
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
  it('reads a selected scope whose channels were all deleted', () => {
    expect(
      parseTokenRoute({ ...route(), mode: 'auto', entries: [] })
    ).toMatchObject({ mode: 'auto', selected_only: true, entries: [] })
  })
  it('rejects an old profile, invalid mode or non-numeric version', () => {
    expect(() => parseTokenRoute({ groups: [] })).toThrow()
    expect(() => parseTokenRoute({ ...route(), mode: 'priority' })).toThrow()
    expect(() => parseTokenRoute({ ...route(), mode: 'manual' })).toThrow()
    expect(() =>
      parseTokenRoute({ ...route(), selected_only: undefined })
    ).toThrow()
    expect(() => parseTokenRoute({ ...route(), selected_only: 1 })).toThrow()
    expect(() => parseTokenRoute({ ...route(), version: '4' })).toThrow()
  })
  it('previews the saved configuration without a write', async () => {
    vi.mocked(api.get).mockResolvedValue(previewResponse())
    const signal = new AbortController().signal
    const result = await tokenRoutingApi.preview(
      7,
      { model: 'fixture', path: '/v1/responses', stream: true },
      signal
    )
    expect(api.get).toHaveBeenCalledWith(
      '/api/token/7/routing/preview',
      {
        model: 'fixture',
        path: '/v1/responses',
        stream: 'true',
        transport: 'http',
      },
      { signal }
    )
    expect(result).toMatchObject({ stream: true, ordering: 'ordered' })
    expect(result.entries[0]!.eligible).toBe(false)
    expect(api.put).not.toHaveBeenCalled()
  })
  it.each(['ordered', 'weighted_random_per_request'] as const)(
    'accepts the %s preview ordering',
    async (ordering) => {
      vi.mocked(api.get).mockResolvedValue({ ...previewResponse(), ordering })
      const result = await tokenRoutingApi.preview(7, previewQuery)
      expect(result.ordering).toBe(ordering)
    }
  )
  it.each([
    { transport: 'invalid' },
    { transport: undefined },
    { ordering: 'weighted' },
    { ordering: 'manual' },
    { mode: 'manual' },
    { ordering: undefined },
    { stream: 'true' },
    { stream: undefined },
  ])('rejects a preview with an invalid field %o', async (field) => {
    vi.mocked(api.get).mockResolvedValue({ ...previewResponse(), ...field })
    await expect(tokenRoutingApi.preview(7, previewQuery)).rejects.toThrow(
      'Invalid API response'
    )
  })
})
