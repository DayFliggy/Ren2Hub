import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PriceFetchDialog from '../PriceFetchDialog.vue'
import { api } from '@/api/client'
import {
  parseFetchedPrices,
  pricingConfigApi,
  type FetchedPriceSource,
} from '@/api/pricingConfig'
import type { PricingDraft } from '@/features/pricing/draft'
import fixedSuccess from '../../../../tests/fixtures/native/ratio-sync-fixed-sources.success.post.json'
import fixedDownloadError from '../../../../tests/fixtures/native/ratio-sync-fixed-sources.download-error.post.json'
import fixedParseError from '../../../../tests/fixtures/native/ratio-sync-fixed-sources.parse-error.post.json'
afterEach(() => vi.restoreAllMocks())
const digest = 'a'.repeat(64)
const published = 'b'.repeat(64)
function localSource(
  overrides: Partial<FetchedPriceSource> = {}
): FetchedPriceSource {
  return {
    ...parseFetchedPrices(fixedSuccess.data).find(
      (source) => source.id === (overrides.id ?? 'sub2api')
    )!,
    ...overrides,
  }
}
function mockSources(sources: FetchedPriceSource[]) {
  vi.spyOn(pricingConfigApi, 'fetch').mockResolvedValue(sources)
  vi.spyOn(pricingConfigApi, 'upstreams').mockResolvedValue([])
}
function mountDialog(draft: PricingDraft = {}, exchangeRate = 1) {
  return mount(PriceFetchDialog, {
    props: { draft, exchangeRate, currency: 'CNY' },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /></div>' },
        TablePagination: true,
      },
    },
  })
}
function card(wrapper: VueWrapper, name: string) {
  const match = wrapper
    .findAll('.price-source')
    .find((item) => item.text().includes(name))
  if (!match) throw new Error(`source card "${name}" not found`)
  return match
}
function fillButton(wrapper: VueWrapper) {
  return wrapper
    .findAll('button')
    .find((button) => button.text() === 'Fill pricing draft')!
}
describe('price source confirmation', () => {
  it('fills the exchange-rate adjusted USD price and blocks unsupported expressions', async () => {
    mockSources([
      localSource({
        models: [
          {
            model_name: 'a',
            provider: 'p',
            pricing: { ModelPrice: 7, CompletionRatio: 2 },
          },
          {
            model_name: 'b',
            provider: 'p',
            pricing: {
              'billing_setting.billing_mode': 'tiered_expr',
              'billing_setting.billing_expr': 'max(p * 7, 10)',
            },
          },
        ],
      }),
    ])
    const wrapper = mountDialog({}, 7)
    await flushPromises()
    await wrapper.get('input[value=sub2api]').setValue(true)
    expect(wrapper.text()).toContain('1 USD = 7 CNY')
    const boxes = wrapper.findAll('input[type=checkbox]')
    await boxes[0]!.setValue(true)
    await boxes[1]!.setValue(true)
    const bulk = wrapper.get('select[aria-label="Apply to selected prices"]')
    await bulk.setValue('divide')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Apply to selected prices')!
      .trigger('click')
    expect(wrapper.text()).toContain('Adjusted by exchange rate: 2 models')
    expect(wrapper.text()).toContain('cannot be adjusted automatically')
    expect(fillButton(wrapper).attributes('disabled')).toBeDefined()
    await wrapper
      .get('select[aria-label="Exchange-rate adjustment b p"]')
      .setValue('keep')
    expect(fillButton(wrapper).attributes('disabled')).toBeUndefined()
    await fillButton(wrapper).trigger('click')
    expect(wrapper.emitted('fill')?.[0]?.[0]).toEqual([
      {
        model_name: 'a',
        provider: 'p',
        pricing: { ModelPrice: 1, CompletionRatio: 2 },
      },
      {
        model_name: 'b',
        provider: 'p',
        pricing: {
          'billing_setting.billing_mode': 'tiered_expr',
          'billing_setting.billing_expr': 'max(p * 7, 10)',
        },
      },
    ])
    wrapper.unmount()
  })
  it('renders precise field and expression differences as inert text', async () => {
    const oldExpression = 'p * 0.123456789012345678 + c * 2'
    const newExpression =
      'p * 0.123456789012345679 + c * 3 + "<img src=x onerror=alert(1)>"'
    mockSources([
      localSource({
        models: [
          {
            model_name: 'a',
            provider: 'p',
            pricing: {
              ModelRatio: 0,
              'billing_setting.billing_expr': newExpression,
            },
          },
        ],
      }),
    ])
    const wrapper = mountDialog({
      a: { ModelPrice: 0, 'billing_setting.billing_expr': oldExpression },
    })
    await flushPromises()
    await wrapper.get('input[value=sub2api]').setValue(true)
    expect(wrapper.text()).toContain('Not set')
    expect(wrapper.text()).toContain('0.123456789012345679')
    expect(wrapper.findAll('[data-diff="removed"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[data-diff="added"]').length).toBeGreaterThan(0)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.emitted('fill')).toBeUndefined()
    wrapper.unmount()
  })
  it('requires one selected source and fills only selected models without saving', async () => {
    mockSources([
      localSource({
        models: [
          { model_name: 'a', provider: 'p', pricing: { ModelPrice: 1 } },
          { model_name: 'b', provider: 'p', pricing: { ModelPrice: 2 } },
        ],
      }),
      parseFetchedPrices(fixedDownloadError.data).find(
        (source) => source.id === 'axonhub'
      )!,
    ])
    const save = vi.spyOn(pricingConfigApi, 'save')
    const wrapper = mountDialog({ a: { ModelPrice: 4 } })
    await flushPromises()
    expect(wrapper.find('input[type=checkbox]').exists()).toBe(false)
    expect(wrapper.text()).toContain('price source: remote returned HTTP 503')
    expect(card(wrapper, 'AxonHub').text()).toContain(
      'Download failed; no fingerprint'
    )
    await wrapper.get('input[value=sub2api]').setValue(true)
    await wrapper.get('input[type=checkbox]').setValue(true)
    expect(wrapper.text()).toContain('1 models already have prices')
    await fillButton(wrapper).trigger('click')
    expect(wrapper.emitted('fill')?.[0]?.[0]).toEqual([
      { model_name: 'a', provider: 'p', pricing: { ModelPrice: 1 } },
    ])
    expect(save).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps the fingerprint when downloading succeeds but parsing fails', async () => {
    const sources = parseFetchedPrices(fixedParseError.data)
    expect(sources).toHaveLength(2)
    for (const source of sources) {
      expect(source.status).toBe('error')
      expect(source.fingerprint).not.toBeNull()
      expect(source.models).toEqual([])
    }
    mockSources(sources)
    const wrapper = mountDialog()
    await flushPromises()
    for (const source of sources) {
      const sourceCard = card(wrapper, source.name)
      expect(sourceCard.get('code').attributes('title')).toBe(
        source.fingerprint!.sha256
      )
      expect(sourceCard.text()).not.toContain('Download failed; no fingerprint')
      for (const error of source.errors) expect(wrapper.text()).toContain(error)
    }
    expect(fillButton(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('fill')).toBeUndefined()
    wrapper.unmount()
  })
  it('takes source ids from the backend and checks the fingerprint contract', () => {
    const raw = (overrides: Record<string, unknown>) => ({
      sources: [
        {
          ...fixedSuccess.data.sources.find(
            (source) => source.id === 'axonhub'
          ),
          ...overrides,
        },
      ],
    })
    expect(parseFetchedPrices(raw({}))[0]).toMatchObject({
      fingerprint: {
        sha256: fixedSuccess.data.sources.find(
          (source) => source.id === 'axonhub'
        )!.fingerprint.sha256,
        published_sha256: null,
        matches: null,
      },
      warnings: ['source does not publish a sha256 fingerprint'],
    })
    // A source added in pkg/pricesource needs no frontend list change.
    expect(
      parseFetchedPrices(raw({ id: 'new-source', name: 'New Source' }))[0]
    ).toMatchObject({ id: 'new-source', name: 'New Source' })
    const invalid = [
      { id: '' },
      { id: 1 },
      { name: undefined },
      { fingerprint: { sha256: 'abc', published_sha256: null, matches: null } },
      {
        fingerprint: {
          sha256: digest,
          published_sha256: published,
          matches: true,
        },
      },
      {
        fingerprint: { sha256: digest, published_sha256: null, matches: true },
      },
      { warnings: undefined },
      { warnings: [1] },
    ]
    for (const overrides of invalid)
      expect(() => parseFetchedPrices(raw(overrides))).toThrow(
        'Invalid API response: /api/ratio_sync/fetch'
      )
  })
  it('shows fingerprint states and source warnings without blocking a mismatch', async () => {
    mockSources([
      localSource({
        fingerprint: {
          sha256: digest,
          published_sha256: published,
          matches: false,
        },
        warnings: ['published sha256 does not match the downloaded document'],
        models: [
          { model_name: 'a', provider: 'p', pricing: { ModelPrice: 1 } },
        ],
      }),
      localSource({
        id: 'axonhub',
        name: 'AxonHub',
        fingerprint: { sha256: digest, published_sha256: null, matches: null },
        warnings: ['source does not publish a sha256 fingerprint'],
        models: [
          { model_name: 'b', provider: 'p', pricing: { ModelPrice: 2 } },
        ],
      }),
    ])
    const wrapper = mountDialog()
    await flushPromises()
    const axonhub = card(wrapper, 'AxonHub')
    expect(axonhub.text()).toContain('not published or unreadable')
    expect(axonhub.text()).not.toContain('Matches the published')
    expect(axonhub.get('details').attributes('open')).toBeUndefined()
    expect(axonhub.get('details summary').text()).toBe('View fingerprints')
    expect(axonhub.get('code').attributes('title')).toBe(digest)
    const sub2api = card(wrapper, 'Sub2API')
    expect(
      sub2api.get('[data-fingerprint=fingerprintMismatched]').text()
    ).toContain('Does not match the published SHA-256')
    expect(sub2api.get('details').attributes('open')).toBeUndefined()
    expect(sub2api.text()).toContain(`Published SHA-256: ${published}`)
    expect(wrapper.text()).toContain(
      'Sub2API: published sha256 does not match the downloaded document'
    )
    expect(wrapper.text()).toContain(
      'AxonHub: source does not publish a sha256 fingerprint'
    )
    await wrapper.get('input[value=sub2api]').setValue(true)
    await wrapper.get('input[type=checkbox]').setValue(true)
    await fillButton(wrapper).trigger('click')
    expect(wrapper.emitted('fill')?.[0]?.[0]).toEqual([
      { model_name: 'a', provider: 'p', pricing: { ModelPrice: 1 } },
    ])
    wrapper.unmount()
  })
  it('fetches native presets and OpenRouter channels through native ratio sync', async () => {
    vi.spyOn(api, 'get').mockImplementation((path: string) => {
      if (path !== '/api/ratio_sync/channels')
        return Promise.reject(new Error(`Unexpected API request: ${path}`))
      return Promise.resolve([
        { id: 3, name: 'OpenAI', base_url: 'https://api.openai.com', type: 1 },
        {
          id: 7,
          name: 'My OpenRouter',
          base_url: 'https://openrouter.ai/api',
          type: 20,
        },
        {
          id: -100,
          name: '官方倍率预设',
          base_url: 'https://basellm.github.io',
          type: 0,
        },
        {
          id: -101,
          name: 'models.dev 价格预设',
          base_url: 'https://models.dev',
          type: 0,
        },
      ])
    })
    const native = new Map<number, unknown>([
      [
        -100,
        {
          differences: {},
          prices: {
            'gpt-a': {
              current: {},
              upstreams: {
                官方倍率预设: {
                  model_ratio: 1.25,
                  completion_ratio: 4,
                  billing_mode: 'ratio',
                },
              },
            },
          },
          test_results: [{ name: '官方倍率预设', status: 'success' }],
        },
      ],
      [
        7,
        {
          differences: {},
          prices: {},
          test_results: [
            {
              name: 'My OpenRouter(7)',
              status: 'error',
              error: 'no API key configured for this channel',
            },
          ],
        },
      ],
    ])
    const post = vi
      .spyOn(api, 'post')
      .mockImplementation((path: string, body?: unknown) => {
        if (path !== '/api/ratio_sync/fetch')
          return Promise.reject(new Error(`Unexpected API request: ${path}`))
        const upstreams = (body as { upstreams?: { id: number }[] }).upstreams
        if (!upstreams) return Promise.resolve({ sources: [] })
        return Promise.resolve(native.get(upstreams[0]!.id))
      })
    const wrapper = mountDialog()
    await flushPromises()
    // Fixed sources download one after another, so they get a longer timeout.
    expect(post).toHaveBeenCalledWith(
      '/api/ratio_sync/fetch',
      {},
      { signal: expect.any(AbortSignal), timeoutMs: 45_000 }
    )
    expect(wrapper.find('input[value="upstream:3"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('basellm official ratio preset')
    expect(wrapper.text()).toContain('models.dev price preset')
    expect(wrapper.text()).toContain('My OpenRouter')
    expect(wrapper.text()).not.toContain('No OpenRouter channel')
    await wrapper.get('input[value="upstream:-100"]').setValue(true)
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/ratio_sync/fetch',
      {
        upstreams: [
          {
            id: -100,
            name: '官方倍率预设',
            base_url: 'https://basellm.github.io',
            endpoint: '/llm-metadata/api/newapi/ratio_config-v1-base.json',
          },
        ],
        include_unchanged: true,
      },
      expect.anything()
    )
    await wrapper.get('input[type=checkbox]').setValue(true)
    await fillButton(wrapper).trigger('click')
    expect(wrapper.emitted('fill')?.[0]?.[0]).toEqual([
      {
        model_name: 'gpt-a',
        provider: '',
        pricing: {
          ModelRatio: 1.25,
          CompletionRatio: 4,
          'billing_setting.billing_mode': 'ratio',
        },
      },
    ])
    await wrapper.get('input[value="upstream:7"]').setValue(true)
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/ratio_sync/fetch',
      {
        upstreams: [
          {
            id: 7,
            name: 'My OpenRouter',
            base_url: 'https://openrouter.ai/api',
            endpoint: 'openrouter',
          },
        ],
        include_unchanged: true,
      },
      expect.anything()
    )
    expect(wrapper.text()).toContain(
      'My OpenRouter: no API key configured for this channel'
    )
    wrapper.unmount()
  })
  it('explains that OpenRouter needs a configured channel instead of offering one', async () => {
    vi.spyOn(pricingConfigApi, 'fetch').mockResolvedValue([])
    vi.spyOn(pricingConfigApi, 'upstreams').mockResolvedValue([
      {
        id: -100,
        name: '官方倍率预设',
        base_url: 'https://basellm.github.io',
        kind: 'basellm',
      },
      {
        id: -101,
        name: 'models.dev 价格预设',
        base_url: 'https://models.dev',
        kind: 'models_dev',
      },
    ])
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.text()).toContain(
      'OpenRouter prices require an OpenRouter channel with a configured key'
    )
    expect(wrapper.findAll('input[type=radio]')).toHaveLength(2)
    wrapper.unmount()
  })
})
