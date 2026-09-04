import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const routerPush = vi.hoisted(() => vi.fn())
const getLoginSessions = vi.hoisted(() => vi.fn())
const revokeLoginSession = vi.hoisted(() => vi.fn())
const revokeOtherLoginSessions = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('@/api/auth', () => ({
  authApi: {
    getLoginSessions,
    revokeLoginSession,
    revokeOtherLoginSessions,
  },
}))

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { LoginSession } from '@/types/auth'
import LoginSessionsPanel from '@/components/console/LoginSessionsPanel.vue'

const current: LoginSession = {
  sid: 'current',
  current: true,
  login_method: 'password',
  ip: '127.0.0.1',
  user_agent: 'Mozilla/5.0 (Windows NT 10.0) Chrome/120.0',
  created_at: 1,
  last_active_at: Math.floor(Date.now() / 1000),
  expires_at: 2_000_000_000,
}
const other: LoginSession = {
  ...current,
  sid: 'other',
  current: false,
  ip: '203.0.113.3',
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  getLoginSessions.mockResolvedValue([current, other])
  revokeLoginSession.mockResolvedValue({ revoked_sid: 'other', current: false })
  revokeOtherLoginSessions.mockResolvedValue({ revoked_count: 1 })
  routerPush.mockReset()
})

function render() {
  return mount(LoginSessionsPanel, {
    global: { plugins: [i18n] },
  })
}

describe('LoginSessionsPanel', () => {
  it('renders current and other sessions and confirms bulk revoke', async () => {
    const wrapper = render()
    await flushPromises()

    expect(wrapper.text()).toContain('Chrome · Windows')
    expect(wrapper.text()).toContain('Current')
    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    expect(document.body.textContent).toContain('Sign out other sessions?')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === 'Sign out other sessions'
    )
    confirm?.click()
    await flushPromises()
    expect(revokeOtherLoginSessions).toHaveBeenCalledOnce()
  })

  it('revokes an inactive session and reloads the list', async () => {
    const wrapper = render()
    await flushPromises()
    const revoke = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Revoke')
    await revoke?.trigger('click')
    expect(document.body.textContent).toContain('Revoke session?')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === 'Revoke'
    )
    confirm?.click()
    await flushPromises()
    expect(revokeLoginSession).toHaveBeenCalledWith('other')
    expect(getLoginSessions.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('clears the local auth state and redirects when current session is revoked', async () => {
    const auth = useAuthStore()
    const invalidate = vi.spyOn(auth, 'invalidateLocalSession')
    revokeLoginSession.mockResolvedValueOnce({
      revoked_sid: 'current',
      current: true,
    })
    const wrapper = render()
    await flushPromises()
    const signOut = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Sign out this device')
    await signOut?.trigger('click')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === 'Sign out this device'
    )
    confirm?.click()
    await flushPromises()
    expect(invalidate).toHaveBeenCalledOnce()
    expect(routerPush).toHaveBeenCalledWith({ name: 'sign-in' })
  })

  it('can retry a failed list request', async () => {
    getLoginSessions
      .mockReset()
      .mockRejectedValueOnce(new Error('load failed'))
      .mockResolvedValueOnce([current, other])
    const wrapper = render()
    await flushPromises()

    expect(wrapper.text()).toContain('load failed')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry')
      ?.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Current')
  })

  it('shows an empty session state', async () => {
    getLoginSessions.mockReset().mockResolvedValueOnce([])
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('No active login sessions')
  })
})
