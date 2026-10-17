import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import KeyClientConfigModal from '../KeyClientConfigModal.vue'
import type { ModelClientConfig } from '@/api/modelManifest'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/modelManifest', () => ({ getTokenClientConfig: mocks.get }))
const response = (
  format: 'codex' | 'pi',
  content: string
): ModelClientConfig => ({
  format,
  content,
  filename: format === 'pi' ? 'ren2hub.models.json' : 'ren2hub.config.toml',
  media_type: 'text/plain',
  models: ['allowed'],
  diagnostics: [{ code: 'prices_omitted' }],
})
function setup() {
  return mount(KeyClientConfigModal, {
    props: { tokenId: 7 },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot/><slot name="footer"/></div>' },
      },
    },
  })
}
beforeEach(() => vi.resetAllMocks())

describe('client configuration modal', () => {
  it('previews and copies only the server export and shows price diagnostics', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    mocks.get.mockResolvedValue(
      response('codex', 'env_key = "REN2HUB_API_KEY"')
    )
    const wrapper = setup()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(
      7,
      'codex',
      undefined,
      expect.any(AbortSignal)
    )
    expect(wrapper.get('textarea').element.value).toContain('REN2HUB_API_KEY')
    expect(wrapper.text()).toContain('Client prices are omitted')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Copy configuration')!
      .trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('env_key = "REN2HUB_API_KEY"')
    wrapper.unmount()
  })
  it('aborts stale exports when switching clients and never resurrects old content', async () => {
    let resolveOld!: (value: ModelClientConfig) => void
    mocks.get
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveOld = resolve
          })
      )
      .mockResolvedValueOnce(response('pi', '{"providers":{}}'))
    const wrapper = setup()
    const firstSignal = mocks.get.mock.calls[0]![3] as AbortSignal
    await wrapper.get('select[aria-label="Client"]').setValue('pi')
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    resolveOld(response('codex', 'stale'))
    await flushPromises()
    expect(wrapper.get('textarea').element.value).toBe('{"providers":{}}')
    wrapper.unmount()
    expect((mocks.get.mock.calls[1]![3] as AbortSignal).aborted).toBe(true)
  })
  it('removes a previous export when authorization changes and refresh fails', async () => {
    mocks.get
      .mockResolvedValueOnce(response('codex', 'old'))
      .mockRejectedValueOnce(new Error('token revoked'))
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('select[aria-label="Client"]').setValue('pi')
    await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('token revoked')
    const download = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Download')!
    expect(download.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
