import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

const replace = vi.hoisted(() => vi.fn())
const route = vi.hoisted(() => ({ query: {} as Record<string, string> }))
const assertPasskey = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual =
    await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => route,
    useRouter: () => ({ replace }),
  }
})
vi.mock('@/utils/passkey', () => ({ assertPasskey }))

import { api } from '@/api/client'
import { ClientError } from '@/api/clientError'
import { useToast } from '@/composables/useToast'
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
  route.query = {}
})

function mountSignIn(pinia: ReturnType<typeof createPinia>) {
  return mount(SignInView, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        AuthLayout: { template: '<div><slot /></div>' },
        RouterLink: {
          props: ['to'],
          template: '<a :data-to="JSON.stringify(to)"><slot /></a>',
        },
        TurnstileWidget: true,
      },
    },
  })
}

describe('SignInView password form', () => {
  it('names the empty fields instead of submitting them', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = {}
    const login = vi.spyOn(useAuthStore(), 'login')

    const wrapper = mountSignIn(pinia)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(login).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('请输入邮箱或用户名')
    expect(wrapper.text()).toContain('请输入密码')
    wrapper.unmount()
  })

  it('replaces the sign-in entry with the sanitized redirect', async () => {
    route.query = { redirect: '/keys?p=2' }
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = {}
    vi.spyOn(app, 'initialize').mockResolvedValue()
    vi.spyOn(useAuthStore(), 'login').mockResolvedValue(null)

    const wrapper = mountSignIn(pinia)
    const [username, password] = wrapper.findAll('input')
    await username!.setValue('demo')
    await password!.setValue('password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(replace).toHaveBeenCalledWith('/keys?p=2')
    const signUp = wrapper
      .findAll('a')
      .find((link) => link.text() === '立即注册')
    expect(JSON.parse(signUp!.attributes('data-to')!)).toEqual({
      name: 'sign-up',
      query: { redirect: '/keys?p=2' },
    })
    wrapper.unmount()
  })
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
    expect(replace).toHaveBeenCalledWith({ name: 'dashboard' })
    wrapper.unmount()
  })
})

describe('SignInView passkey cancellation', () => {
  it('shows a dismissed passkey prompt in the interface language', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = { frontend_capabilities: { passkey: 'live' } }
    vi.spyOn(app, 'initialize').mockResolvedValue()
    vi.spyOn(useAuthStore(), 'login').mockResolvedValue({
      require_verification: true,
      methods: [{ method: 'passkey', available: true }],
      flow_token: 'login-flow',
      expires_at: 2_000_000_000,
    })
    vi.spyOn(api, 'post').mockResolvedValueOnce({
      flow_token: 'passkey-flow',
      expires_at: 2_000_000_000,
      options,
      rp_ids: ['example.com'],
    })
    assertPasskey.mockRejectedValue(
      new ClientError('passkeyCancelled', 'NotAllowedError')
    )
    useToast().toasts.splice(0)

    const wrapper = mountSignIn(pinia)
    const [username, password] = wrapper.findAll('input')
    await username!.setValue('demo')
    await password!.setValue('password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '使用 Passkey 登录')!
      .trigger('click')
    await flushPromises()

    expect(useToast().toasts.map((toast) => toast.message)).toEqual([
      '已取消 Passkey 验证。',
    ])
    useToast().toasts.splice(0)
    wrapper.unmount()
  })
})

describe('SignInView legal consent', () => {
  function mountWithStatus(status: Record<string, unknown>) {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = status
    vi.spyOn(app, 'initialize').mockResolvedValue()
    const login = vi.spyOn(useAuthStore(), 'login').mockResolvedValue(null)
    return { wrapper: mountSignIn(pinia), login }
  }
  async function fillCredentials(wrapper: ReturnType<typeof mountSignIn>) {
    const [username, password] = wrapper.findAll('input')
    await username!.setValue('demo')
    await password!.setValue('password')
  }

  it('does not ask for consent when no legal document is published', async () => {
    const { wrapper, login } = mountWithStatus({})
    expect(wrapper.find('[data-legal-consent]').exists()).toBe(false)
    await fillCredentials(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(login).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('blocks password sign-in until the published terms are accepted', async () => {
    const { wrapper, login } = mountWithStatus({
      user_agreement_enabled: true,
      privacy_policy_enabled: true,
    })
    const consent = wrapper.get('[data-legal-consent]')
    expect(consent.text()).toBe('我已阅读并同意用户协议和隐私政策')
    const links = consent
      .findAll('a')
      .map((link) => JSON.parse(link.attributes('data-to')!))
    expect(links).toEqual([
      { name: 'user-agreement' },
      { name: 'privacy-policy' },
    ])

    const submit = wrapper.get('button[type="submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    expect(submit.attributes('aria-describedby')).toBe(consent.attributes('id'))
    await fillCredentials(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(login).not.toHaveBeenCalled()

    await consent.get('input[type="checkbox"]').setValue(true)
    expect(submit.attributes('disabled')).toBeUndefined()
    expect(submit.attributes('aria-describedby')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(login).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('disables passkey and OAuth sign-in until consent, with only the published link', async () => {
    const post = vi.spyOn(api, 'post')
    const { wrapper } = mountWithStatus({
      privacy_policy_enabled: true,
      github_oauth: true,
      frontend_capabilities: { passkey: 'live' },
    })
    const consent = wrapper.get('[data-legal-consent]')
    expect(consent.findAll('a')).toHaveLength(1)
    expect(consent.text()).toBe('我已阅读并同意隐私政策')
    const methodButtons = () =>
      wrapper
        .findAll('button')
        .filter((button) =>
          ['使用 Passkey 登录', '使用 GitHub 登录'].includes(button.text())
        )
    expect(methodButtons()).toHaveLength(2)
    for (const button of methodButtons())
      expect(button.attributes('disabled')).toBeDefined()
    await methodButtons()[0]!.trigger('click')
    await flushPromises()
    expect(post).not.toHaveBeenCalled()

    await consent.get('input[type="checkbox"]').setValue(true)
    for (const button of methodButtons())
      expect(button.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
