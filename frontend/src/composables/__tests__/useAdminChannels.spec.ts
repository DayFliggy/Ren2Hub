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
  group: 'default',
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
  group: 'default,vip',
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
  it('uses native listing, search and multi-key creation with routing groups', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const state = await setup()
    expect(get.mock.calls[0]?.[0]).toBe('/api/channel/')
    state.keyword.value = 'Native'
    await state.load()
    expect(get).toHaveBeenLastCalledWith(
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
    expect(await state.updateNumber(channel, 'priority', 8)).toBe(true)
    expect(put).toHaveBeenCalledWith('/api/channel/', { id: 101, priority: 8 })
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
