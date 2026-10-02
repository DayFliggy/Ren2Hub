import { flushPromises, mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import ModelsView from '@/views/console/ModelsView.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

const manifest = {
  schema_version: 1,
  models: [
    {
      id: 'gpt-4o',
      visible: true,
      supported_endpoint_types: ['openai'],
      price_configured: true,
      price_version: 'v1',
    },
  ],
}
beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue(manifest)
})

afterEach(() => vi.restoreAllMocks())

describe('ModelsView live boundary', () => {
  it('renders backend model names without invented pricing metadata', async () => {
    const wrapper = mount(ModelsView, {
      attachTo: document.body,
      global: { plugins: [i18n] },
    })
    await flushPromises()

    expect(api.get).toHaveBeenCalledWith(
      '/api/user/model-manifest',
      undefined,
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.text()).toContain('gpt-4o')
    expect(wrapper.text()).toContain('共 1 个模型')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('shows cache read and cache write prices in model details', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce(manifest)
      .mockResolvedValueOnce([
        {
          model_name: 'gpt-4o',
          description: 'General model',
          quota_type: 0,
          model_ratio: 0.5,
          completion_ratio: 2,
          cache_ratio: 0.25,
          create_cache_ratio: 1.25,
          owner_by: 'OpenAI',
          supported_endpoint_types: ['chat'],
          enable_groups: ['default'],
        },
      ])
      .mockResolvedValueOnce({ models: [] })

    const wrapper = mount(ModelsView, {
      attachTo: document.body,
      global: { plugins: [i18n] },
    })
    await flushPromises()

    await wrapper.get('button[title="详情"]').trigger('click')
    await flushPromises()

    expect(
      document.body.querySelector('[data-detail-cache-read]')?.textContent
    ).toContain('$0.250')
    expect(
      document.body.querySelector('[data-detail-cache-write]')?.textContent
    ).toContain('$1.250')
  })
})
