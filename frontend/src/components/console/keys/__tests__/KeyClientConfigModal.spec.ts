import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import KeyClientConfigModal from '@/components/console/keys/KeyClientConfigModal.vue'
import {
  getTokenClientConfig,
  loadClientConfigCatalog,
} from '@/api/clientConfig'
import { ApiError } from '@/api/types'
import { buildClientConfig } from '@/utils/clientConfig'
import type { ClientConfiguration } from '@/types/clientConfig'

vi.mock('@/api/clientConfig', () => ({
  getTokenClientConfig: vi.fn(),
  loadClientConfigCatalog: vi.fn(),
}))

let wrapper: VueWrapper | undefined
const config = buildClientConfig(
  'codex',
  'https://gateway.example',
  'future-model'
)
const copy = vi.fn().mockResolvedValue(undefined)

function button(text: string) {
  const found = wrapper!.findAll('button').find((item) => item.text() === text)
  if (!found) throw new Error(`Button not found: ${text}`)
  return found
}

function mountModal(locale = 'en') {
  wrapper = mount(KeyClientConfigModal, {
    props: { tokenId: 7 },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale,
          fallbackLocale: 'en',
          messages: {},
        }),
      ],
      stubs: { Teleport: true },
    },
  })
  return wrapper
}

beforeEach(() => {
  vi.mocked(loadClientConfigCatalog).mockResolvedValue({
    baseURL: 'https://gateway.example/v1',
    models: ['future-model', 'other-model'],
    protocolModels: {
      codex: ['future-model', 'other-model'],
      pi: ['future-model'],
    },
  })
  vi.mocked(getTokenClientConfig).mockResolvedValue(config)
  copy.mockClear()
  vi.stubGlobal('navigator', { clipboard: { writeText: copy } })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

describe('KeyClientConfigModal', () => {
  it('keeps missing protocol facts explicit even when the address is edited', async () => {
    vi.mocked(loadClientConfigCatalog).mockResolvedValueOnce({
      baseURL: 'https://gateway.example',
      models: ['other-model'],
      protocolModels: { codex: [], pi: ['other-model'] },
    })
    mountModal()
    await flushPromises()
    await wrapper!
      .get('input[aria-label="Client server address"]')
      .setValue('https://public.example')
    expect(wrapper!.get('[role="alert"]').text()).toContain(
      'No model candidate explicitly supports'
    )
    expect(
      button('Generate configuration').attributes('disabled')
    ).toBeDefined()
    expect(getTokenClientConfig).not.toHaveBeenCalled()
    await wrapper!.get('select[aria-label="Client"]').setValue('pi')
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
    expect(
      button('Generate configuration').attributes('disabled')
    ).toBeUndefined()
  })
  it.each(['en', 'zh-CN'])(
    'previews and copies a secret-free config in %s',
    async (locale) => {
      mountModal(locale)
      await flushPromises()
      expect(wrapper!.get('textarea').element.value).toBe(config.content)
      await button(locale === 'en' ? 'Copy configuration' : '复制配置').trigger(
        'click'
      )
      expect(copy).toHaveBeenCalledWith(config.content)
      expect(wrapper!.text()).toContain('REN2HUB_API_KEY')
      expect(wrapper!.text()).not.toContain('error.failed')
    }
  )

  it('downloads the actual preview with its media type and stable filename', async () => {
    mountModal()
    await flushPromises()
    const create = vi.fn().mockReturnValue('blob:client-config')
    const revoke = vi.fn()
    vi.stubGlobal(
      'URL',
      Object.assign(class extends URL {}, {
        createObjectURL: create,
        revokeObjectURL: revoke,
      })
    )
    let downloadedFilename = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      downloadedFilename = this.download
    })
    await button('Download').trigger('click')
    expect(create.mock.calls[0][0]).toBeInstanceOf(Blob)
    expect((create.mock.calls[0][0] as Blob).type).toBe('application/toml')
    expect(downloadedFilename).toBe('ren2hub.config.toml')
    expect(revoke).not.toHaveBeenCalled()
    await new Promise((resolve) => window.setTimeout(resolve, 0))
    expect(revoke).toHaveBeenCalledWith('blob:client-config')
  })

  it('lets users replace a localhost site default without changing site settings', async () => {
    vi.mocked(loadClientConfigCatalog).mockResolvedValueOnce({
      baseURL: 'http://localhost:3000',
      models: ['future-model'],
      protocolModels: { codex: ['future-model'], pi: ['future-model'] },
    })
    mountModal()
    await flushPromises()
    const address = wrapper!.get('input[aria-label="Client server address"]')
    expect((address.element as HTMLInputElement).value).toBe(
      'http://localhost:3000'
    )
    await address.setValue('https://public.example/proxy/')
    expect(wrapper!.find('textarea').exists()).toBe(false)
    await button('Generate configuration').trigger('click')
    await flushPromises()
    expect(getTokenClientConfig).toHaveBeenLastCalledWith(
      7,
      'codex',
      'future-model',
      'https://public.example/proxy/',
      undefined,
      expect.any(AbortSignal)
    )
    await button('Use current site address').trigger('click')
    expect((address.element as HTMLInputElement).value).toBe(
      new URL('/', window.location.origin).href
    )
    expect(wrapper!.find('textarea').exists()).toBe(false)
  })

  it('requires explicit Pi capabilities and invalidates the preview when they change', async () => {
    mountModal()
    await flushPromises()
    await wrapper!.get('select[aria-label="Client"]').setValue('pi')
    expect(wrapper!.find('textarea').exists()).toBe(false)
    await button('Generate configuration').trigger('click')
    expect(wrapper!.get('[role="alert"]').text()).toContain('positive integer')
    expect(getTokenClientConfig).toHaveBeenCalledTimes(1)
    await wrapper!
      .get('input[aria-label="Context window (tokens)"]')
      .setValue('64000')
    await wrapper!
      .get('input[aria-label="Maximum output (tokens)"]')
      .setValue('8000')
    await wrapper!.get('select[aria-label="Reasoning support"]').setValue('no')
    await wrapper!.get('select[aria-label="Input types"]').setValue('text')
    vi.mocked(getTokenClientConfig).mockImplementation(
      async (_id, format, model, url, pi) =>
        buildClientConfig(format, url, model, pi)
    )
    await button('Generate configuration').trigger('click')
    await flushPromises()
    expect(getTokenClientConfig).toHaveBeenLastCalledWith(
      7,
      'pi',
      'future-model',
      'https://gateway.example/v1',
      {
        contextWindow: 64000,
        maxTokens: 8000,
        reasoning: false,
        imageInput: false,
      },
      expect.any(AbortSignal)
    )
    expect(wrapper!.get('textarea').element.value).toContain(
      '"apiKey": "${REN2HUB_API_KEY}"'
    )
    await wrapper!
      .get('input[aria-label="Maximum output (tokens)"]')
      .setValue('9000')
    expect(wrapper!.find('textarea').exists()).toBe(false)
    expect(button('Download').attributes('disabled')).toBeDefined()
    await wrapper!.get('select[aria-label="Client"]').setValue('codex')
    await flushPromises()
    expect(wrapper!.get('textarea').element.value).toContain(
      'wire_api = "responses"'
    )
  })

  it('clears stale output immediately and ignores late responses after client changes', async () => {
    let resolve!: (value: ClientConfiguration) => void
    vi.mocked(getTokenClientConfig).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    mountModal()
    await flushPromises()
    const signal = vi.mocked(getTokenClientConfig).mock.calls[0][5]!
    await wrapper!.get('select[aria-label="Client"]').setValue('pi')
    expect(signal.aborted).toBe(true)
    resolve(config)
    await flushPromises()
    expect(wrapper!.find('textarea').exists()).toBe(false)
    expect(button('Copy configuration').attributes('disabled')).toBeDefined()
  })

  it('clears the previous preview when current model access is revoked', async () => {
    mountModal()
    await flushPromises()
    vi.mocked(getTokenClientConfig).mockRejectedValueOnce(
      new ApiError('Permission revoked', { status: 403 })
    )
    await wrapper!.get('select[aria-label="Model"]').setValue('other-model')
    expect(wrapper!.find('textarea').exists()).toBe(false)
    await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('Permission revoked')
    expect(button('Download').attributes('disabled')).toBeDefined()
  })

  it('aborts the metadata load when closed before it completes', async () => {
    vi.mocked(loadClientConfigCatalog).mockReturnValueOnce(
      new Promise(() => {})
    )
    mountModal()
    const signal = vi.mocked(loadClientConfigCatalog).mock.calls[0][1]!
    wrapper!.unmount()
    wrapper = undefined
    expect(signal.aborted).toBe(true)
  })
})
