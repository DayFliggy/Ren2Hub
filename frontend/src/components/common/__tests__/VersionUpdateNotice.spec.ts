import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

const publicApi = vi.hoisted(() => ({
  status: vi.fn(),
  notice: vi.fn(),
  pricingCount: vi.fn(),
  uptime: vi.fn(),
}))
vi.mock('@/api/public', () => ({ publicApi }))

import VersionUpdateNotice from '@/components/common/VersionUpdateNotice.vue'
import i18n, { setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'

const nativeLocation = window.location
const notice = () => document.body.querySelector('[data-version-update]')

beforeAll(() => setLocale('en'))
beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
})

describe('version update notice', () => {
  it('appears only after the server reports a different version and reloads the page', async () => {
    const reload = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { reload },
      configurable: true,
      writable: true,
    })
    const app = useAppStore()
    const wrapper = mount(VersionUpdateNotice, {
      global: { plugins: [i18n] },
    })
    publicApi.status.mockResolvedValue({ version: 'v1.0.0' })
    await app.refreshStatus()
    await app.refreshStatus()
    await flushPromises()
    expect(notice()).toBeNull()

    publicApi.status.mockResolvedValue({ version: 'v1.0.1' })
    await app.refreshStatus()
    await flushPromises()
    expect(notice()?.textContent).toContain(
      'The site has been updated. Reload the page to use the new version.'
    )
    ;(notice()?.querySelector('button') as HTMLButtonElement).click()
    expect(reload).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
})
