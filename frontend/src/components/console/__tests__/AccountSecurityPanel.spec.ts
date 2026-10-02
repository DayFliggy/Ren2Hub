import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const apiGet = vi.hoisted(() => vi.fn())
const apiPost = vi.hoisted(() => vi.fn())
const publicStatus = vi.hoisted(() => vi.fn())
const requestProof = vi.hoisted(() => vi.fn())
vi.mock('@/components/auth/SecurityProofDialog.vue', () => ({
  default: {
    setup(_: unknown, { expose }: { expose: (value: unknown) => void }) {
      expose({ request: requestProof })
      return () => null
    },
  },
}))
vi.mock('@/api/client', () => ({
  api: { get: apiGet, post: apiPost, delete: vi.fn() },
  setApiUnauthorizedHandler: vi.fn(),
}))

vi.mock('@/api/console', () => ({
  api: {
    get: apiGet,
    post: apiPost,
    delete: vi.fn(),
  },
}))

vi.mock('@/api/public', () => ({
  publicApi: {
    status: publicStatus,
  },
}))

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import AccountSecurityPanel from '@/components/console/AccountSecurityPanel.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  requestProof.mockReset().mockResolvedValue('one-use-proof')
  apiGet.mockImplementation((url: string) => {
    if (url === '/api/user/self/personal-access-tokens')
      return Promise.resolve([])
    if (url === '/api/user/self/personal-access-tokens/scopes')
      return Promise.resolve({
        scopes: ['profile:read'],
        limit: 20,
        default_lifetime_days: 30,
      })
    if (url === '/api/user/token/status')
      return Promise.resolve({ exists: false, token_ref: '' })
    if (url === '/api/user/passkey') return Promise.resolve({ enabled: false })
    if (url === '/api/user/2fa/status')
      return Promise.resolve({ enabled: false })
    if (url === '/api/user/oauth/bindings') return Promise.resolve([])
    return Promise.resolve({})
  })
  apiPost.mockReset().mockResolvedValue({ flow_token: 'oauth-flow' })
  publicStatus.mockReset().mockResolvedValue({
    github_oauth: true,
    github_client_id: 'github-client',
  })
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
})

function render() {
  return mount(AccountSecurityPanel, {
    props: {
      user: {
        id: 1,
        username: 'member',
        display_name: 'Member',
        email: 'member@example.com',
        role: 1,
        status: 1,
        available_quota: '0',
        reserved_quota: '0',
        quota_version: 0,
        used_quota: '0',
        request_count: 0,
        created_at: 1,
      },
    },
    global: { plugins: [i18n] },
  })
}

describe('AccountSecurityPanel retired token management', () => {
  it('does not offer or request the retired single-token API', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).not.toContain('Manage token')
    expect(
      apiGet.mock.calls.some(([url]) =>
        String(url).startsWith('/api/user/token')
      )
    ).toBe(false)
    wrapper.unmount()
  })
})

describe('AccountSecurityPanel OAuth popup lifecycle', () => {
  it('closes its popup and removes window event handlers when unmounted', async () => {
    const removeEventListener = vi.spyOn(window, 'removeEventListener')
    const closePopup = vi.fn()
    const openPopup = vi.spyOn(window, 'open').mockReturnValue({
      close: closePopup,
      location: { replace: vi.fn() },
    } as unknown as Window)
    const wrapper = render()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Bind')
      ?.trigger('click')
    const confirm = Array.from(document.body.querySelectorAll('button'))
      .filter((button) => button.textContent?.trim() === 'Bind')
      .at(-1)
    confirm?.click()
    await flushPromises()
    expect(openPopup).toHaveBeenCalledOnce()

    wrapper.unmount()
    await flushPromises()

    expect(closePopup).toHaveBeenCalledOnce()
    expect(removeEventListener).toHaveBeenCalledWith(
      'message',
      expect.any(Function)
    )
    expect(removeEventListener).toHaveBeenCalledWith(
      'beforeunload',
      expect.any(Function)
    )
    openPopup.mockRestore()
    removeEventListener.mockRestore()
  })
})
