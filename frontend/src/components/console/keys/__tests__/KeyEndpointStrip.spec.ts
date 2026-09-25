import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import KeyEndpointStrip from '@/components/console/keys/KeyEndpointStrip.vue'
import { runEndpointProbe } from '@/components/console/keys/endpointProbe'
import i18n, { loadMessageDomain } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { parsePublicStatus } from '@/api/public'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function mountStrip(status: Record<string, unknown> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useAppStore()
  store.status = parsePublicStatus(status)
  return mount(KeyEndpointStrip, {
    attachTo: document.body,
    global: { plugins: [i18n, pinia] },
  })
}

describe('KeyEndpointStrip', () => {
  it('renders configured addresses and copies the exact selected path, including configuration updates', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    const wrapper = mountStrip({
      server_address: 'https://console.example.test',
      api_info_enabled: true,
      api_info: [
        {
          route: 'Global',
          description: 'Worldwide access',
          url: 'https://api.example.test/v1',
        },
        {
          route: 'Asia',
          description: 'Regional access',
          url: 'https://asia.example.test/gateway/v1/',
        },
      ],
    })

    expect(wrapper.findAll('code').map((node) => node.text())).toEqual([
      'https://api.example.test/v1',
      'https://asia.example.test/gateway/v1/',
    ])
    expect(wrapper.text()).toContain('Regional access')

    await wrapper
      .get(
        `[aria-label="${i18n.global.t('keys.endpoints.copy', { name: 'Asia' })}"]`
      )
      .trigger('click')
    await flushPromises()

    expect(writeText).toHaveBeenCalledWith(
      'https://asia.example.test/gateway/v1/'
    )
    useAppStore().status = parsePublicStatus({
      api_info: [
        { route: 'Replacement', url: 'https://new.example.test/proxy/' },
      ],
    })
    await flushPromises()
    expect(wrapper.findAll('code').map((node) => node.text())).toEqual([
      'https://new.example.test/proxy/',
    ])
    await wrapper
      .get(
        `[aria-label="${i18n.global.t('keys.endpoints.copy', { name: 'Replacement' })}"]`
      )
      .trigger('click')
    expect(writeText).toHaveBeenLastCalledWith(
      'https://new.example.test/proxy/'
    )
    wrapper.unmount()
  })

  it.each([
    { api_info: [], server_address: 'https://gateway.example.test/proxy/' },
    {
      api_info_enabled: false,
      api_info: [{ route: 'Hidden', url: 'https://hidden.example.test' }],
      server_address: 'https://gateway.example.test/proxy/',
    },
  ])(
    'uses the server address when the configured list is empty or disabled',
    (status) => {
      const wrapper = mountStrip(status)
      expect(wrapper.findAll('code').map((node) => node.text())).toEqual([
        'https://gateway.example.test/proxy/',
      ])
      wrapper.unmount()
    }
  )

  it('falls back to the current origin when no valid address exists', () => {
    const wrapper = mountStrip({
      server_address: 'javascript:alert(1)',
      api_info: [
        { route: 'Unsafe', url: 'javascript:alert(1)' },
        { route: 'Credentials', url: 'https://name:secret@example.test' },
      ],
    })
    expect(wrapper.findAll('code').map((node) => node.text())).toEqual([
      window.location.origin,
    ])
    expect(wrapper.text()).not.toContain('secret')
    wrapper.unmount()
  })

  it('uses a cancellable HEAD request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response())
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()

    await expect(
      runEndpointProbe('https://renai.uno', controller.signal)
    ).resolves.toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenCalledWith(
      'https://renai.uno',
      expect.objectContaining({ method: 'HEAD', mode: 'no-cors' })
    )
  })

  it('cancels an in-flight HTTP probe', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string, init?: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('Aborted', 'AbortError'))
          })
        })
      })
    )
    const controller = new AbortController()
    const pending = runEndpointProbe('https://renai.uno', controller.signal)

    controller.abort()

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
})
