import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

const push = vi.hoisted(() => vi.fn())
const assertPasskey = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual =
    await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => ({ query: {} }),
    useRouter: () => ({ push }),
  }
})
vi.mock('@/utils/passkey', () => ({ assertPasskey }))

import { api } from '@/api/client'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { AuthBundle } from '@/types/auth'
import SignInView from '../SignInView.vue'

const bundle: AuthBundle = {
  access_token: 'access-token',
  token_type: 'Bearer',
  access_expires_at: 2_000_000_000,
  session: {
    sid: 'session-1',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'vitest',
    created_at: 1,
    last_active_at: 2,
    expires_at: 2_000_000_000,
  },
  user: {
    id: 1,
    username: 'demo',
    display_name: 'Demo',
    email: 'demo@example.com',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
  },
}
const options = {
  publicKey: { challenge: 'Y2hhbGxlbmdl', rpId: 'example.com' },
}
const credential = { id: 'credential-1', type: 'public-key' }

beforeAll(async () => {
  await loadMessageDomain('auth')
  await setLocale('zh-CN')
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

describe('SignInView passkey second factor', () => {
  it('follows the native login-verification passkey contract', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = { frontend_capabilities: { passkey: 'live' } }
    vi.spyOn(app, 'initialize').mockResolvedValue()
    const auth = useAuthStore()
    // The password step answers with the native second-factor challenge.
    vi.spyOn(auth, 'login').mockResolvedValue({
      require_verification: true,
      methods: [{ method: 'passkey', available: true }],
      flow_token: 'login-flow',
      expires_at: 2_000_000_000,
    })
    const acceptLogin = vi
      .spyOn(auth, 'acceptLogin')
      .mockImplementation(() => {})
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({
        flow_token: 'passkey-flow',
        expires_at: 2_000_000_000,
        options,
        rp_ids: ['example.com'],
      })
      .mockResolvedValueOnce(bundle)
    assertPasskey.mockResolvedValue(credential)

    const wrapper = mount(SignInView, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          RouterLink: true,
          TurnstileWidget: true,
        },
      },
    })
    const [username, password] = wrapper.findAll('input')
    await username!.setValue('demo')
    await password!.setValue('password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const passkeyButton = wrapper
      .findAll('button')
      .find((button) => button.text() === '使用 Passkey 登录')
    expect(passkeyButton).toBeDefined()
    await passkeyButton!.trigger('click')
    await flushPromises()

    // router/api-router.go registers only /api/user/login/passkey/{begin,finish}
    // for this step; controller/login_verification.go reads these fields.
    expect(post.mock.calls).toEqual([
      [
        '/api/user/login/passkey/begin',
        { rp_id: '', flow_token: 'login-flow' },
      ],
      [
        '/api/user/login/passkey/finish',
        {
          flow_token: 'login-flow',
          passkey_flow_token: 'passkey-flow',
          credential,
        },
      ],
    ])
    expect(assertPasskey).toHaveBeenCalledWith(options)
    expect(acceptLogin).toHaveBeenCalledWith(bundle)
    expect(push).toHaveBeenCalledWith({ name: 'dashboard' })
    wrapper.unmount()
  })
})
