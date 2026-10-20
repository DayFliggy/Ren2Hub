import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { useAdminChannels } from '@/composables/useAdminChannels'
import type { AdminChannel, AdminChannelCreateInput } from '@/types/console'

vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: { value: false } }),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))
const channel = { id: 101, name: 'Advanced Fixture' } as AdminChannel
const page = { items: [channel], total: 1, type_counts: {} }
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

afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
})

describe('channel management contracts', () => {
  it('creates one multi-key channel with an explicit polling strategy', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(page)
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 102 })
    const state = await setup()
    const input: AdminChannelCreateInput = {
      name: 'Multi',
      type: 1,
      key: 'key-a\nkey-b',
      models: 'fixture',
      status: 1,
      base_url: '',
      model_mapping: '',
      priority: 0,
      weight: 0,
      capacity_total: 20,
      channel_ratio: '1',
    }
    expect(
      await state.createChannel(input, {
        multiKey: true,
        multiKeyMode: 'polling',
      })
    ).toBe(true)
    expect(post).toHaveBeenCalledWith(
      '/api/next/admin/channels',
      {
        mode: 'multi_to_single',
        multi_key_mode: 'polling',
        channel: input,
      },
      { signal: expect.any(AbortSignal) }
    )
  })

  it('retains raw balance output for display without treating it as a number', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(page)
    const state = await setup()
    get.mockResolvedValueOnce({ raw_response: '{"credits":"12.50"}' })
    expect(await state.refreshBalance(channel)).toBe(true)
    expect(state.balanceResponse.value).toEqual({
      channelName: channel.name,
      rawResponse: '{"credits":"12.50"}',
    })
    state.balanceResponse.value = null
    get.mockResolvedValueOnce({ balance: 12.5 })
    expect(await state.refreshBalance(channel)).toBe(true)
    expect(state.balanceResponse.value).toBeNull()
    get.mockRejectedValueOnce(new Error('Upstream unavailable'))
    expect(await state.refreshBalance(channel)).toBe(false)
    expect(state.balanceResponse.value).toBeNull()
  })
})

describe('retired subscription channels', () => {
  const retired = {
    ...channel,
    id: 157,
    type: 57,
    status: 1,
    models: 'gpt-5-codex',
  } as AdminChannel
  const active = {
    ...channel,
    id: 101,
    type: 1,
    status: 2,
    models: 'gpt-5-codex',
  } as AdminChannel
  const input: AdminChannelCreateInput = {
    name: 'Retired',
    type: 57,
    key: 'fixture',
    models: 'gpt-5-codex',
    status: 1,
    base_url: '',
    model_mapping: '',
    priority: 0,
    weight: 0,
    capacity_total: 20,
    channel_ratio: '1',
  }

  it('does not send retired mutations or upstream requests, but permits explicit deletion', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ ...page, items: [retired] })
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const put = vi.spyOn(api, 'put').mockResolvedValue(null)
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({ id: retired.id })
    const state = await setup()
    expect(state.canRunBatch.value).toBe(false)
    expect(await state.createChannel(input)).toBe(false)
    expect(await state.updateChannelDetails(retired, input)).toBe(false)
    expect(await state.updateChannelDetails(active, input)).toBe(false)
    expect(await state.updateNumber(retired, 'priority', 2)).toBe(false)
    expect(await state.toggleStatus(retired)).toBe(false)
    expect(await state.refreshBalance(retired)).toBe(false)
    expect((await state.testChannelModel(retired, 'gpt-5-codex')).ok).toBe(
      false
    )
    expect(await state.removeChannelModels(retired, ['gpt-5-codex'])).toBe(
      false
    )
    expect(await state.updateChannelsStatus('enable', [retired])).toBe(false)
    await expect(
      state.fetchUpstreamModels({ channelId: retired.id })
    ).rejects.toThrow('channels.retiredHint')
    await expect(
      state.fetchUpstreamModels({ type: 57, key: 'fixture', baseUrl: '' })
    ).rejects.toThrow('channels.retiredHint')
    await state.runVisibleBalance()
    expect(get).toHaveBeenCalledTimes(1)
    expect(post).not.toHaveBeenCalled()
    expect(put).not.toHaveBeenCalled()
    expect(await state.deleteChannel(retired)).toBe(true)
    expect(remove).toHaveBeenCalledWith('/api/channel/157', undefined, {
      signal: expect.any(AbortSignal),
    })
  })

  it('filters retired rows from batches and still tests Codex models on standard OpenAI', async () => {
    const listing = { ...page, items: [retired, active] }
    const get = vi
      .spyOn(api, 'get')
      .mockImplementation(async (url) =>
        url.includes('/test/') ? { time: 0.125 } : listing
      )
    const post = vi.spyOn(api, 'post').mockResolvedValue(1)
    const state = await setup()
    expect(
      await state.testChannelModel(active, 'gpt-5-codex', {
        endpointType: 'openai-response',
        stream: true,
      })
    ).toEqual({ ok: true, timeMs: 125 })
    expect(get).toHaveBeenCalledWith(
      '/api/next/admin/channels/test/101',
      {
        model: 'gpt-5-codex',
        endpoint_type: 'openai-response',
        stream: 'true',
      },
      { signal: undefined }
    )
    await state.runVisibleBalance()
    expect(
      get.mock.calls
        .filter(([url]) => url.includes('/balance/'))
        .map(([url]) => url)
    ).toEqual(['/api/next/admin/channels/balance/101'])
    expect(post).toHaveBeenCalledWith(
      '/api/next/admin/channels/lab-audit',
      expect.objectContaining({ channel_ids: [101] })
    )
    await state.updateChannelsStatus('enable', [retired, active])
    expect(post).toHaveBeenCalledWith(
      '/api/next/admin/channels/status/batch',
      expect.objectContaining({ ids: [101] }),
      { signal: expect.any(AbortSignal) }
    )
  })
})
