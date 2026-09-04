import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const generateAccessToken = vi.hoisted(() => vi.fn())
const apiGet = vi.hoisted(() => vi.fn())
const apiPost = vi.hoisted(() => vi.fn())
const publicStatus = vi.hoisted(() => vi.fn())

vi.mock('@/api/auth', () => ({
  authApi: { generateAccessToken },
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
  apiGet.mockImplementation((url: string) => {
    if (url === '/api/user/passkey') return Promise.resolve({ enabled: false })
    if (url === '/api/user/2fa/status')
      return Promise.resolve({ enabled: false })
    if (url === '/api/user/oauth/bindings') return Promise.resolve([])
    return Promise.resolve({})
  })
  generateAccessToken.mockReset()
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
        quota: 0,
        used_quota: 0,
        request_count: 0,
        created_at: 1,
      },
    },
    global: { plugins: [i18n] },
  })
}

describe('AccountSecurityPanel access token', () => {
  it('only shows a generated token in the open modal and clears it on close', async () => {
    generateAccessToken.mockResolvedValue('one-time-secret')
    const wrapper = render()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Manage token')
      ?.trigger('click')
    expect(document.body.textContent).toContain(
      'For security, existing access tokens'
    )

    const regenerate = Array.from(
      document.body.querySelectorAll('button')
    ).find((button) => button.textContent?.trim() === 'Regenerate token')
    regenerate?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('Regenerate access token?')

    const confirm = Array.from(document.body.querySelectorAll('button'))
      .filter((button) => button.textContent?.trim() === 'Regenerate token')
      .at(-1)
    confirm?.click()
    await flushPromises()

    const input = document.body.querySelector<HTMLInputElement>(
      'input[aria-label="access-token"]'
    )
    expect(input?.value).toBe('one-time-secret')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      'one-time-secret'
    )

    const close = Array.from(document.body.querySelectorAll('button')).find(
      (button) =>
        button.textContent?.trim() === 'Close' &&
        button
          .closest('[role="dialog"]')
          ?.querySelector('input[aria-label="access-token"]')
    )
    close?.click()
    await new Promise((resolve) => setTimeout(resolve, 250))
    await flushPromises()
    expect(
      document.body.querySelector('input[aria-label="access-token"]')
    ).toBeNull()
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
