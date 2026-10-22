import { defineComponent, reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { httpTransport } from '@/api/httpTransport'
import { useAdminChannels } from '@/composables/useAdminChannels'
import type { AdminChannel, AdminChannelCreateInput } from '@/types/console'
const permissions = reactive({
  read: true,
  write: true,
  operate: true,
  sensitive_write: true,
})
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    hasPermission: (_resource: string, action: keyof typeof permissions) =>
      permissions[action],
  }),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }),
}))
const channel: AdminChannel = {
  id: 101,
  name: 'Native fixture',
  type: 1,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
  status: 1,
  priority: 0,
  weight: 1,
  used_quota: 500000,
  balance: 2,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'gpt-5-codex',
  model_mapping: '',
}
const page = { items: [channel], total: 1, type_counts: { '1': 1 } }
const input: AdminChannelCreateInput = {
  name: 'Multi',
  type: 1,
  key: 'key-a\nkey-b',
  models: 'fixture',
  channel_ratio: '1',
  max_concurrency: 0,
  status: 1,
  base_url: '',
  model_mapping: '',
  priority: 0,
  weight: 1,
}
let wrapper: VueWrapper
async function setup() {
  let state!: ReturnType<typeof useAdminChannels>
  wrapper = mount(
    defineComponent({
      setup() {
        state = useAdminChannels()
        return () => null
      },
    }),
    {
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            missingWarn: false,
            fallbackWarn: false,
          }),
        ],
      },
    }
  )
  await flushPromises()
  return state
}
beforeEach(() =>
  Object.assign(permissions, {
    read: true,
    write: true,
    operate: true,
    sensitive_write: true,
  })
)
afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
})
describe('native channel management', () => {
  it('uses native listing, search and multi-key creation without business groups', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const state = await setup()
    expect(get.mock.calls[0]?.[0]).toBe('/api/channel/')
    state.keyword.value = 'Native'
    await state.load()
    expect(get).toHaveBeenCalledWith(
      '/api/channel/search',
      expect.objectContaining({ keyword: 'Native', p: 1, page_size: 20 }),
      expect.any(Object)
    )
    expect(
      await state.createChannel(input, {
        multiKey: true,
        multiKeyMode: 'polling',
      })
    ).toBe(true)
    expect(post).toHaveBeenCalledWith(
      '/api/channel/',
      { mode: 'multi_to_single', multi_key_mode: 'polling', channel: input },
      { signal: expect.any(AbortSignal) }
    )
  })
  it('enforces action-specific permissions and clears results when read permission is lost', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const put = vi.spyOn(api, 'put').mockResolvedValue(null)
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const request = vi.spyOn(httpTransport, 'request')
    const state = await setup()
    permissions.sensitive_write = false
    expect(await state.createChannel(input)).toBe(false)
    expect(await state.deleteChannel(channel)).toBe(false)
    permissions.operate = false
    expect(await state.toggleStatus(channel)).toBe(false)
    expect((await state.testChannelModel(channel, 'fixture')).ok).toBe(false)
    expect(request).not.toHaveBeenCalled()
    expect(post).not.toHaveBeenCalled()
    expect(await state.updateNumber(channel, 'weight', 8)).toBe(true)
    expect(put).toHaveBeenCalledWith('/api/channel/', { id: 101, weight: 8 })
    permissions.read = false
    await flushPromises()
    expect(state.rows.value).toEqual([])
    expect(state.total.value).toBe(0)
  })
  it('reads native top-level time and balance output without accepting fake zero latency', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValueOnce({ success: true, time: 0.125 } as never)
      .mockResolvedValueOnce({ success: true } as never)
      .mockResolvedValueOnce({
        success: true,
        raw_response: '{"credits":12.5}',
      } as never)
    const state = await setup()
    expect(
      await state.testChannelModel(channel, 'gpt-5-codex', {
        endpointType: 'openai-response',
        stream: true,
      })
    ).toEqual({ ok: true, timeMs: 125 })
    expect(request).toHaveBeenCalledWith(
      'GET',
      '/api/channel/test/101',
      expect.objectContaining({
        params: {
          model: 'gpt-5-codex',
          endpoint_type: 'openai-response',
          stream: 'true',
        },
      })
    )
    expect((await state.testChannelModel(channel, 'fixture')).ok).toBe(false)
    expect(await state.refreshBalance(channel)).toBe(true)
    expect(state.balanceResponse.value?.rawResponse).toBe('{"credits":12.5}')
  })
  it('queries balances only for channel types with a backend balance branch', async () => {
    const multiKey = {
      ...channel,
      id: 102,
      channel_info: {
        is_multi_key: true,
        multi_key_mode: 'random' as const,
        multi_key_size: 2,
      },
    }
    const unsupported = [
      { ...channel, id: 103, type: 3 },
      { ...channel, id: 104, type: 14 },
      { ...channel, id: 105, type: 61 },
      multiKey,
    ]
    const supported = [
      channel,
      { ...channel, id: 106, type: 43 },
      { ...channel, id: 107, type: 58 },
    ]
    vi.spyOn(api, 'get').mockResolvedValue({
      items: [...unsupported, ...supported],
      total: 7,
      type_counts: {},
    })
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValue({ success: true, balance: 1 } as never)
    const state = await setup()

    for (const item of unsupported)
      expect(await state.refreshBalance(item)).toBe(false)
    expect(request).not.toHaveBeenCalled()

    await state.runVisibleBalance()
    expect(
      request.mock.calls.map(([method, path]) => `${method} ${path}`)
    ).toEqual([
      'GET /api/channel/update_balance/101',
      'GET /api/channel/update_balance/106',
      'GET /api/channel/update_balance/107',
    ])
  })
  it('refuses retired type 57 without blocking Codex model names on ordinary API channels', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const post = vi.spyOn(api, 'post')
    const request = vi.spyOn(httpTransport, 'request')
    const state = await setup()
    const retired = { ...channel, type: 57 }
    expect(await state.createChannel({ ...input, type: 57 })).toBe(false)
    expect(await state.toggleStatus(retired)).toBe(false)
    expect((await state.testChannelModel(retired, 'gpt-5-codex')).ok).toBe(
      false
    )
    await expect(
      state.fetchUpstreamModels({ type: 57, key: 'fixture', baseUrl: '' })
    ).rejects.toThrow('channels.retiredHint')
    expect(post).not.toHaveBeenCalled()
    expect(request).not.toHaveBeenCalled()
  })
  it('does not apply a late list result after access is revoked', async () => {
    let finish!: (value: unknown) => void
    const get = vi.spyOn(api, 'get').mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }) as never
    )
    const state = await setup()
    permissions.read = false
    await flushPromises()
    expect(get.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    finish(page)
    await flushPromises()
    expect(state.rows.value).toEqual([])
  })
})

describe('channel priority transaction', () => {
  it('previews and submits the same revision with a nullable previous priority', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (path) =>
      path === '/api/channel/priority/order'
        ? { revision: 'r1', items: [{ id: 101, priority: null }], changes: [] }
        : page
    )
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ revision: 'r1', items: [], changes: [] })
    const put = vi.spyOn(api, 'put')
    const state = await setup()
    expect(await state.updateNumber(channel, 'priority', -5)).toBe(true)
    expect(post.mock.calls).toEqual([
      [
        '/api/channel/priority/reorder',
        {
          revision: 'r1',
          preview: true,
          restore: [{ id: 101, previous: null, priority: -5 }],
        },
      ],
      [
        '/api/channel/priority/reorder',
        {
          revision: 'r1',
          preview: false,
          restore: [{ id: 101, previous: null, priority: -5 }],
        },
      ],
    ])
    expect(put).not.toHaveBeenCalled()
  })
  it('stops on a stale UI priority before previewing', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (path) =>
      path === '/api/channel/priority/order'
        ? { revision: 'r2', items: [{ id: 101, priority: 3 }], changes: [] }
        : page
    )
    const post = vi.spyOn(api, 'post')
    const state = await setup()
    expect(await state.updateNumber(channel, 'priority', 4)).toBe(false)
    expect(post).not.toHaveBeenCalled()
  })
  it.each(['preview', 'commit'])(
    'does not retry or bypass a %s conflict',
    async (phase) => {
      vi.spyOn(api, 'get').mockImplementation(async (path) =>
        path === '/api/channel/priority/order'
          ? { revision: 'r1', items: [{ id: 101, priority: 0 }], changes: [] }
          : page
      )
      const post = vi.spyOn(api, 'post')
      if (phase === 'commit')
        post.mockResolvedValueOnce({ revision: 'r1', items: [], changes: [] })
      post.mockRejectedValueOnce(new Error('Priority revision conflict'))
      const put = vi.spyOn(api, 'put')
      const state = await setup()
      expect(await state.updateNumber(channel, 'priority', 4)).toBe(false)
      expect(post).toHaveBeenCalledTimes(phase === 'preview' ? 1 : 2)
      expect(put).not.toHaveBeenCalled()
    }
  )
})
