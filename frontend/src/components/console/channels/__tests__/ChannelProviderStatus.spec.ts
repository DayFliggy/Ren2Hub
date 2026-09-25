import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import ChannelProviderStatus from '../ChannelProviderStatus.vue'
import { api } from '@/api/client'
import type { AdminChannel } from '@/types/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())
describe('read-only provider status', () => {
  it.each([
    [62, 'vllm'],
    [63, 'sglang'],
  ])(
    'loads channel type %i with partial endpoint failures',
    async (type, provider) => {
      const get = vi.spyOn(api, 'get').mockResolvedValue({
        sampled_at: 1,
        version: '0.9',
        endpoints: {
          '/health': { status: 200 },
          '/metrics': { status: 503, error: 'http_error' },
        },
        models: [{ id: 'fixture', root: '', max_model_len: 32768 }],
        metrics: [],
        raw_metrics: '',
      })
      const wrapper = mount(ChannelProviderStatus, {
        props: { channel: { id: 101, type } as AdminChannel },
        global: { plugins: [i18n] },
      })
      await flushPromises()
      expect(get).toHaveBeenCalledWith(
        `/api/channel/101/${provider}/status`,
        undefined,
        { signal: expect.any(AbortSignal) }
      )
      expect(wrapper.text()).toContain('503 · HTTP error')
      expect(wrapper.text()).toContain('32768')
      wrapper.unmount()
    }
  )
})
