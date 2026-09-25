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
      channel_ratio: 1,
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
