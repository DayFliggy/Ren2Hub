import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import {
  getAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { AuthBundle } from '@/types/auth'
import SignInView from '../SignInView.vue'
import SignUpView from '../SignUpView.vue'

const bundle: AuthBundle = {
  access_token: 'test-access',
  token_type: 'Bearer',
  access_expires_at: 2_000_000_000,
  session: {
    sid: 'test-session',
    current: true,
    login_method: 'oauth',
    ip: '127.0.0.1',
    user_agent: 'vitest',
    created_at: 1,
    last_active_at: 1,
    expires_at: 2_000_000_000,
  },
  user: {
    id: 1,
    username: 'created',
    display_name: 'Created',
    email: '',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
  },
}

beforeAll(async () => {
  await loadMessageDomain('auth')
  await setLocale('en')
})
beforeEach(() => localStorage.clear())
afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('OAuth-only attribution recovery', () => {
  it.each([
    { name: 'login', view: SignInView },
    { name: 'registration', view: SignUpView },
  ])(
    '$name omits a prefilled code until the user confirms it',
    async ({ view }) => {
      storeAffiliateAttribution('UNCONFIRMED-CODE')
      const pinia = createPinia()
      setActivePinia(pinia)
      const app = useAppStore()
      app.phase = 'ready'
      app.status = {
        github_oauth: true,
        github_client_id: 'isolated-test-client',
        password_register_enabled: true,
      }
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: '/auth-test', component: view }],
      })
      await router.push('/auth-test')
      const post = vi
        .spyOn(api, 'post')
        .mockResolvedValue({ flow_token: 'state' })
      const popup = {
        closed: false,
        close: vi.fn(),
        location: { replace: vi.fn() },
      } as unknown as Window
      vi.spyOn(window, 'open').mockReturnValue(popup)
      const wrapper = mount(view, {
        global: {
          plugins: [pinia, router, i18n],
          stubs: {
            AuthLayout: { template: '<div><slot /></div>' },
            RouterLink: true,
            PasswordStrengthMeter: true,
          },
        },
      })
      await flushPromises()
      expect(
        wrapper.get<HTMLInputElement>(
          '[data-affiliate-attribution] input[type="checkbox"]'
        ).element.checked
      ).toBe(false)
      await wrapper
        .findAll('button')
        .find((button) => button.text().includes('GitHub'))!
        .trigger('click')
      await flushPromises()

      expect(post.mock.calls[0]?.[1]).not.toHaveProperty('aff')
      expect(getAffiliateAttribution()).toBe('')
      wrapper.unmount()
      await flushPromises()
    }
  )

  it.each(
    [
      { name: 'login', view: SignInView },
      { name: 'registration', view: SignUpView },
    ].flatMap((entry) =>
      [false, true].flatMap((passwordEnabled) =>
        ['AFFILIATE_CODE_INVALID', 'AFFILIATE_DISABLED'].map((code) => ({
          ...entry,
          code,
          passwordEnabled,
        }))
      )
    )
  )(
    '$name passwordEnabled=$passwordEnabled recovers $code with explicit clearing and a fresh state',
    async ({ view, code, passwordEnabled }) => {
      storeAffiliateAttribution('REJECTED-CODE')
      const pinia = createPinia()
      setActivePinia(pinia)
      const app = useAppStore()
      app.phase = 'ready'
      app.status = {
        password_login_enabled: passwordEnabled,
        password_register_enabled: passwordEnabled,
        github_oauth: true,
        github_client_id: 'isolated-test-client',
      }
      const auth = useAuthStore()
      const accepted = vi
        .spyOn(auth, 'acceptLogin')
        .mockImplementation(() => {})
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [
          { path: '/auth-test', component: view },
          {
            path: '/dashboard',
            name: 'dashboard',
            component: { template: '<div />' },
          },
        ],
      })
      await router.push('/auth-test')
      const post = vi
        .spyOn(api, 'post')
        .mockResolvedValueOnce({ flow_token: 'first-state' })
        .mockResolvedValueOnce({ flow_token: 'new-state' })
      const get = vi
        .spyOn(api, 'get')
        .mockRejectedValueOnce(
          new ApiError('Localized referral rejection', { code, business: true })
        )
        .mockResolvedValueOnce(bundle)
      const popup = {
        closed: false,
        close: vi.fn(),
        location: { replace: vi.fn() },
      } as unknown as Window
      vi.spyOn(window, 'open').mockReturnValue(popup)
      const wrapper = mount(view, {
        global: {
          plugins: [pinia, router, i18n],
          stubs: {
            AuthLayout: { template: '<div><slot /></div>' },
            RouterLink: true,
            PasswordStrengthMeter: true,
          },
        },
      })
      await flushPromises()
      expect(wrapper.find('form').exists()).toBe(passwordEnabled)
      const provider = () =>
        wrapper
          .findAll('button')
          .find((button) => button.text().includes('GitHub'))!
      const callback = (state: string) =>
        window.dispatchEvent(
          new MessageEvent('message', {
            origin: window.location.origin,
            source: popup,
            data: {
              type: 'ren2hub:oauth-bind-callback',
              provider: 'github',
              state,
              code: 'mock-code',
            },
          })
        )
      await wrapper
        .get<HTMLInputElement>(
          '[data-affiliate-attribution] input[type="checkbox"]'
        )
        .setValue(true)
      await provider().trigger('click')
      await flushPromises()
      callback('first-state')
      await flushPromises()
      expect(
        wrapper.get('[data-affiliate-attribution] [role=alert]').text()
      ).toContain('Localized referral rejection')
      expect(getAffiliateAttribution()).toBe('REJECTED-CODE')
      expect(accepted).not.toHaveBeenCalled()
      expect(post.mock.calls[0]?.[1]).toMatchObject({ aff: 'REJECTED-CODE' })
      await wrapper.get('[data-affiliate-attribution] button').trigger('click')
      await flushPromises()
      expect(getAffiliateAttribution()).toBe('')
      expect(
        wrapper.get<HTMLInputElement>('[data-affiliate-attribution] input')
          .element.value
      ).toBe('')
      await provider().trigger('click')
      await flushPromises()
      expect(post).toHaveBeenCalledTimes(2)
      expect(post.mock.calls[1]?.[1]).not.toHaveProperty('aff')
      callback('first-state')
      await flushPromises()
      expect(get).toHaveBeenCalledTimes(1)
      callback('new-state')
      await flushPromises()
      expect(get.mock.calls[1]?.[1]).toMatchObject({ state: 'new-state' })
      expect(accepted).toHaveBeenCalledWith(bundle)
      expect(router.currentRoute.value.name).toBe('dashboard')
      wrapper.unmount()
    }
  )
})
