import { defineComponent, nextTick, reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { httpTransport } from '@/api/httpTransport'
import { useAdminChannels } from '@/composables/useAdminChannels'
import type {
  AdminChannel,
  AdminChannelCreateInput,
  AdminChannelCreateOptions,
} from '@/types/console'
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
const toast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
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
  vi.clearAllMocks()
})
describe('native channel management', () => {
  it.each([true, 1])(
    'keeps a status update successful when publication_pending is %s',
    async (pending) => {
      vi.spyOn(api, 'get').mockResolvedValue(page)
      const postEnvelope = vi.spyOn(api, 'postEnvelope').mockResolvedValue({
        success: true,
        data: 1,
        publication_pending: pending,
      })
      const state = await setup()
      expect(await state.toggleStatus(channel)).toBe(true)
      expect(postEnvelope).toHaveBeenCalledWith('/api/channel/101/status', {
        status: 2,
      })
      expect(toast.warning).toHaveBeenCalledExactlyOnceWith(
        'common.publicationPending'
      )
      expect(toast.error).not.toHaveBeenCalled()
    }
  )

  it('warns instead of reporting success when the status did not change', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    vi.spyOn(api, 'postEnvelope').mockResolvedValue({
      success: true,
      data: false,
      publication_pending: false,
    })
    const state = await setup()
    get.mockClear()
    expect(await state.toggleStatus(channel)).toBe(false)
    expect(toast.warning).toHaveBeenCalledExactlyOnceWith(
      'channels.statusUnchanged'
    )
    expect(toast.success).not.toHaveBeenCalled()
    expect(toast.error).not.toHaveBeenCalled()
    expect(get).toHaveBeenCalledWith(
      '/api/channel/',
      expect.anything(),
      expect.anything()
    )
  })

  it('retains the batch changed count and reports pending publication separately', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    vi.spyOn(api, 'postEnvelope').mockResolvedValue({
      success: true,
      data: 1,
      publication_pending: 1,
    })
    const state = await setup()
    expect(await state.updateChannelsStatus('disable', [channel])).toBe(true)
    expect(toast.success).toHaveBeenCalledWith('channels.bulkDisabled')
    expect(toast.warning).toHaveBeenCalledWith('common.publicationPending')
    expect(toast.error).not.toHaveBeenCalled()
  })
  it('uses native listing, search and single creation without business groups', async () => {
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
    expect(await state.createChannel(input)).toBe(true)
    expect(post).toHaveBeenCalledWith(
      '/api/channel/',
      { mode: 'single', channel: input },
      { signal: expect.any(AbortSignal) }
    )
  })
  it.each([
    {
      name: 'single',
      options: undefined,
      result: {
        id: 101,
        ids: [101],
        results: [{ input_index: 0, channel_id: 101 }],
      },
    },
    {
      name: 'batch',
      options: { batchKeys: true },
      result: {
        ids: [101, 102],
        results: [
          { input_index: 0, channel_id: 101 },
          { input_index: 1, channel_id: 102 },
        ],
      },
    },
  ])(
    'keeps creation feedback and refresh for the native $name ID response',
    async ({ options, result }) => {
      const get = vi.spyOn(api, 'get').mockResolvedValue(page)
      vi.spyOn(api, 'post').mockResolvedValue(result)
      const state = await setup()
      get.mockClear()
      expect(
        await state.createChannel(
          input,
          options as AdminChannelCreateOptions | undefined
        )
      ).toBe(true)
      expect(toast.success).toHaveBeenCalledExactlyOnceWith('channels.created')
      expect(toast.error).not.toHaveBeenCalled()
      expect(get).toHaveBeenCalledWith(
        '/api/channel/',
        expect.any(Object),
        expect.any(Object)
      )
      expect(state.rows.value[0]?.id).toBe(101)
    }
  )
  it('does not report creation success after the server rolls back a failed batch', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    vi.spyOn(api, 'post').mockRejectedValue(new Error('batch rolled back'))
    const state = await setup()
    get.mockClear()
    expect(await state.createChannel(input, { batchKeys: true })).toBe(false)
    expect(toast.success).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledWith('Error: batch rolled back')
    expect(get).not.toHaveBeenCalled()
  })
  it('filters by model through native search, which the plain list ignores', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    const state = await setup()
    get.mockClear()
    vi.useFakeTimers()
    try {
      state.modelKeyword.value = ' gpt-5 '
      await nextTick()
      await vi.advanceTimersByTimeAsync(300)
      expect(get).toHaveBeenCalledWith(
        '/api/channel/search',
        expect.objectContaining({ model: 'gpt-5', keyword: undefined }),
        expect.any(Object)
      )
    } finally {
      vi.useRealTimers()
    }
  })
  it('resets every list filter back to the plain list and keeps the sort', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    const state = await setup()
    state.sortBy.value = 'name'
    state.keyword.value = 'Native'
    state.modelKeyword.value = 'gpt-5'
    state.type.value = '1'
    state.lab.value = 'openai'
    state.status.value = 'enabled'
    expect(state.hasFilters.value).toBe(true)
    state.resetFilters()
    expect(state.hasFilters.value).toBe(false)
    get.mockClear()
    await state.load()
    expect(get).toHaveBeenCalledWith(
      '/api/channel/',
      expect.objectContaining({
        keyword: undefined,
        model: undefined,
        type: undefined,
        lab: undefined,
        status: undefined,
        sort_by: 'name',
      }),
      expect.any(Object)
    )
  })
  it('enforces action-specific permissions and clears results when read permission is lost', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const put = vi.spyOn(api, 'put').mockResolvedValue(null)
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const getEnvelope = vi.spyOn(api, 'getEnvelope')
    const state = await setup()
    permissions.sensitive_write = false
    expect(await state.createChannel(input)).toBe(false)
    expect(await state.deleteChannel(channel)).toBe(false)
    permissions.operate = false
    expect(await state.toggleStatus(channel)).toBe(false)
    expect((await state.testChannelModel(channel, 'fixture')).ok).toBe(false)
    expect(getEnvelope).not.toHaveBeenCalled()
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
    const getEnvelope = vi
      .spyOn(api, 'getEnvelope')
      .mockResolvedValueOnce({
        success: true,
        message: '',
        time: 0.125,
      } as never)
      .mockResolvedValueOnce({ success: true, message: '' })
      .mockResolvedValueOnce({
        success: true,
        message: '',
        raw_response: '{"credits":12.5}',
      } as never)
    const state = await setup()
    expect(
      await state.testChannelModel(channel, 'gpt-5-codex', {
        endpointType: 'openai-response',
        stream: true,
      })
    ).toEqual({ ok: true, timeMs: 125 })
    expect(getEnvelope).toHaveBeenCalledWith(
      '/api/channel/test/101',
      {
        model: 'gpt-5-codex',
        endpoint_type: 'openai-response',
        stream: 'true',
      },
      { signal: undefined }
    )
    expect((await state.testChannelModel(channel, 'fixture')).ok).toBe(false)
    expect(await state.refreshBalance(channel)).toBe(true)
    expect(state.balanceResponse.value?.rawResponse).toBe('{"credits":12.5}')
  })
  it('returns a failed channel test as a per-model result with the native message', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    vi.spyOn(httpTransport, 'request').mockResolvedValueOnce({
      success: false,
      message: 'provider failed',
      time: 1.5,
      error_code: 'bad_response_status_code',
    } as never)
    const state = await setup()
    expect(await state.testChannelModel(channel, 'fixture')).toEqual({
      ok: false,
      message: 'provider failed',
    })
  })
  it('queries balances only for channel types with a backend balance branch', async () => {
    const unsupported = [
      { ...channel, id: 103, type: 3 },
      { ...channel, id: 104, type: 14 },
    ]
    const supported = [
      channel,
      { ...channel, id: 105, type: 61 },
      { ...channel, id: 106, type: 43 },
      { ...channel, id: 107, type: 58 },
    ]
    vi.spyOn(api, 'get').mockResolvedValue({
      items: [...unsupported, ...supported],
      total: 6,
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
      'GET /api/channel/update_balance/105',
      'GET /api/channel/update_balance/106',
      'GET /api/channel/update_balance/107',
    ])
  })
  it('refuses retired type 57 without blocking Codex model names on ordinary API channels', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const post = vi.spyOn(api, 'post')
    const getEnvelope = vi.spyOn(api, 'getEnvelope')
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
    expect(getEnvelope).not.toHaveBeenCalled()
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
