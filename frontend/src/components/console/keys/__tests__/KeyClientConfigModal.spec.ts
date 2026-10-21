import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import KeyClientConfigModal from '../KeyClientConfigModal.vue'
import {
  ClientConfigError,
  type ClientConfiguration,
} from '@/types/clientConfig'
const mocks = vi.hoisted(() => ({ load: vi.fn(), generate: vi.fn() }))
vi.mock('@/api/clientConfig', () => ({
  loadClientConfigCatalog: mocks.load,
  getTokenClientConfig: mocks.generate,
}))
const configuration = (content: string): ClientConfiguration => ({
  filename: 'ren2hub.config.toml',
  mediaType: 'application/toml',
  content,
})
function setup() {
  return mount(KeyClientConfigModal, {
    props: { tokenId: 7 },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en' })],
      stubs: {
        ConsoleModal: { template: '<div><slot/><slot name="footer"/></div>' },
      },
    },
  })
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.load.mockResolvedValue({
    baseURL: 'https://api.example.test',
    models: ['allowed'],
    protocolModels: { codex: ['allowed'], pi: ['allowed'] },
  })
  mocks.generate.mockResolvedValue(configuration('env_key = "REN2HUB_API_KEY"'))
})
describe('native client configuration modal', () => {
  it('previews a revalidated configuration and copies only the environment-key reference', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    const wrapper = setup()
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith(
      7,
      'codex',
      'allowed',
      'https://api.example.test',
      undefined,
      expect.any(AbortSignal)
    )
    expect(wrapper.get('textarea').element.value).toContain('REN2HUB_API_KEY')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Copy configuration')!
      .trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('env_key = "REN2HUB_API_KEY"')
    wrapper.unmount()
  })
  it('cancels a pending export when changing clients and requires explicit Pi capabilities', async () => {
    let resolve!: (value: ClientConfiguration) => void
    mocks.generate.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      })
    )
    const wrapper = setup()
    await flushPromises()
    const signal = mocks.generate.mock.calls[0]![5] as AbortSignal
    await wrapper.get('select[aria-label="Client"]').setValue('pi')
    expect(signal.aborted).toBe(true)
    resolve(configuration('stale'))
    await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Generate configuration')!
      .trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'Enter positive integer'
    )
    expect(mocks.generate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('removes old output when access is revoked before regeneration', async () => {
    const wrapper = setup()
    await flushPromises()
    mocks.generate.mockRejectedValueOnce(
      new ClientConfigError('token_unavailable')
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Generate configuration')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'disabled, expired, or exhausted'
    )
    expect(
      wrapper
        .findAll('button')
        .find((button) => button.text() === 'Download')!
        .attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })
})
