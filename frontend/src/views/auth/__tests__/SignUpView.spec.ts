import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const route = vi.hoisted(() => ({ query: { aff: 'INVALID' } }))
const push = vi.hoisted(() => vi.fn())
const validateAffiliate = vi.hoisted(() => vi.fn())
const register = vi.hoisted(() => vi.fn())
const sendVerification = vi.hoisted(() => vi.fn())
const turnstileReset = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual =
    await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => route,
    useRouter: () => ({ push }),
  }
})
vi.mock('@/api/auth', () => ({
  authApi: {
    register,
    sendVerification,
    validateAffiliate,
  },
}))

import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import {
  getAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'
import SignUpView from '../SignUpView.vue'

describe('SignUpView affiliate attribution', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    localStorage.clear()
    route.query.aff = 'INVALID'
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('shows URL attribution without silently confirming or persisting it', async () => {
    storeAffiliateAttribution('STORED-CODE')
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'

    const wrapper = mount(SignUpView, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          PasswordStrengthMeter: true,
          RouterLink: true,
        },
      },
    })
    await flushPromises()

    expect(
      wrapper.get<HTMLInputElement>('[data-affiliate-attribution] input')
        .element.value
    ).toBe('INVALID')
    expect(
      wrapper.get<HTMLInputElement>(
        '[data-affiliate-attribution] input[type="checkbox"]'
      ).element.checked
    ).toBe(false)
    expect(getAffiliateAttribution()).toBe('')
    expect(validateAffiliate).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('邀请码无效')
    wrapper.unmount()
  })

  it('omits an unconfirmed URL invite from password registration', async () => {
    register.mockResolvedValue(undefined)
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    const wrapper = mount(SignUpView, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          PasswordStrengthMeter: true,
          RouterLink: true,
        },
      },
    })
    await flushPromises()
    await wrapper.find('input[autocomplete="username"]').setValue('demo')
    for (const input of wrapper.findAll('input[autocomplete="new-password"]'))
      await input.setValue('password-123')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(register).toHaveBeenCalledTimes(1)
    expect(register.mock.calls[0]?.[0].aff_code).toBeUndefined()
    expect(getAffiliateAttribution()).toBe('')
    wrapper.unmount()
  })

  it('sends a confirmed code and exposes stable affiliate registration errors', async () => {
    register.mockRejectedValueOnce(
      new ApiError('The invite code is invalid.', {
        code: 'AFFILIATE_CODE_INVALID',
        business: true,
      })
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    const wrapper = mount(SignUpView, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          PasswordStrengthMeter: true,
          RouterLink: true,
        },
      },
    })
    await flushPromises()
    await wrapper.find('input[autocomplete="username"]').setValue('demo')
    for (const input of wrapper.findAll('input[autocomplete="new-password"]'))
      await input.setValue('password-123')
    await wrapper
      .get<HTMLInputElement>(
        '[data-affiliate-attribution] input[type="checkbox"]'
      )
      .setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(register).toHaveBeenCalledWith(
      expect.objectContaining({ aff_code: 'INVALID' }),
      undefined
    )
    expect(
      wrapper.get('[data-affiliate-attribution] [role="alert"]').text()
    ).toContain('The invite code is invalid.')
    wrapper.unmount()
  })

  it('hides password registration and offers configured OAuth providers', async () => {
    route.query.aff = ''
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    app.status = {
      password_register_enabled: false,
      github_oauth: true,
    }

    const wrapper = mount(SignUpView, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          PasswordStrengthMeter: true,
          RouterLink: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('已关闭密码注册')
    expect(wrapper.text()).toContain('使用 GitHub 继续')
    wrapper.unmount()
  })
})

// Mirrors TurnstileWidget: reset() clears the token by emitting verified('').
const TurnstileStub = defineComponent({
  name: 'TurnstileWidget',
  emits: ['verified', 'unavailable'],
  setup(_, { emit, expose }) {
    expose({
      reset: () => {
        turnstileReset()
        emit('verified', '')
      },
    })
    return () => null
  },
})

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function mountTurnstileSignUp() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  app.phase = 'ready'
  app.status = {
    email_verification: true,
    turnstile_check: true,
    turnstile_site_key: 'site-key',
  }
  return mount(SignUpView, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        AuthLayout: { template: '<div><slot /></div>' },
        PasswordStrengthMeter: true,
        RouterLink: true,
        TurnstileWidget: TurnstileStub,
      },
    },
  })
}

type SignUpWrapper = ReturnType<typeof mountTurnstileSignUp>

async function fillForm(wrapper: SignUpWrapper) {
  await wrapper.find('input[autocomplete="username"]').setValue('demo')
  await wrapper.find('input[autocomplete="email"]').setValue('demo@example.com')
  for (const input of wrapper.findAll('input[autocomplete="new-password"]'))
    await input.setValue('password-123')
  await wrapper.find('input[autocomplete="one-time-code"]').setValue('123456')
}

async function verify(wrapper: SignUpWrapper, token: string) {
  wrapper.findComponent(TurnstileStub).vm.$emit('verified', token)
  await flushPromises()
}

describe('SignUpView Turnstile requests', () => {
  beforeEach(async () => {
    vi.resetAllMocks()
    localStorage.clear()
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('spends one captured token per request and resets the widget after each', async () => {
    sendVerification.mockResolvedValue(undefined)
    register.mockResolvedValue(undefined)
    const wrapper = mountTurnstileSignUp()
    await flushPromises()
    await fillForm(wrapper)
    const send = wrapper.find('button[type="button"]')
    const submit = wrapper.find('button[type="submit"]')
    expect(send.attributes('disabled')).toBeDefined()
    expect(submit.attributes('disabled')).toBeDefined()

    await verify(wrapper, 'mail-token')
    await send.trigger('click')
    await flushPromises()
    expect(sendVerification).toHaveBeenCalledWith(
      'demo@example.com',
      'mail-token'
    )
    expect(turnstileReset).toHaveBeenCalledTimes(1)
    expect(send.attributes('disabled')).toBeDefined()

    await verify(wrapper, 'register-token')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(register).toHaveBeenCalledWith(
      expect.objectContaining({
        username: 'demo',
        email: 'demo@example.com',
        verification_code: '123456',
      }),
      'register-token'
    )
    expect(turnstileReset).toHaveBeenCalledTimes(2)
    expect(push).toHaveBeenCalledWith({ name: 'sign-in' })
    wrapper.unmount()
  })

  it('keeps sending a code and registering mutually exclusive', async () => {
    const mail = deferred()
    const registration = deferred()
    sendVerification.mockReturnValue(mail.promise)
    register.mockReturnValue(registration.promise)
    const wrapper = mountTurnstileSignUp()
    await flushPromises()
    await fillForm(wrapper)

    await verify(wrapper, 'mail-token')
    await wrapper.find('button[type="button"]').trigger('click')
    await verify(wrapper, 'late-token')
    expect(
      wrapper.find('button[type="submit"]').attributes('disabled')
    ).toBeDefined()
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(register).not.toHaveBeenCalled()
    mail.resolve()
    await flushPromises()

    await verify(wrapper, 'register-token')
    await wrapper.find('form').trigger('submit')
    await verify(wrapper, 'second-mail-token')
    expect(
      wrapper.find('button[type="button"]').attributes('disabled')
    ).toBeDefined()
    expect(sendVerification).toHaveBeenCalledTimes(1)
    expect(register).toHaveBeenCalledWith(expect.anything(), 'register-token')
    registration.resolve()
    await flushPromises()
    expect(turnstileReset).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('resets the widget after business and network failures', async () => {
    sendVerification
      .mockRejectedValueOnce(
        new ApiError('Email already in use', { business: true })
      )
      .mockRejectedValueOnce(new Error('Network Error'))
    register.mockRejectedValueOnce(
      new ApiError('Username taken', { business: true })
    )
    const wrapper = mountTurnstileSignUp()
    await flushPromises()
    await fillForm(wrapper)
    const send = wrapper.find('button[type="button"]')

    await verify(wrapper, 'mail-token-1')
    await send.trigger('click')
    await flushPromises()
    await verify(wrapper, 'mail-token-2')
    await send.trigger('click')
    await flushPromises()
    await verify(wrapper, 'register-token')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(sendVerification.mock.calls.map((call) => call[1])).toEqual([
      'mail-token-1',
      'mail-token-2',
    ])
    expect(register).toHaveBeenCalledWith(expect.anything(), 'register-token')
    expect(turnstileReset).toHaveBeenCalledTimes(3)
    expect(push).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

function mountSignUp(emailVerification: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  app.phase = 'ready'
  app.status = { email_verification: emailVerification }
  return mount(SignUpView, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        AuthLayout: { template: '<div><slot /></div>' },
        PasswordStrengthMeter: true,
        RouterLink: true,
      },
    },
  })
}

describe('SignUpView email verification', () => {
  beforeEach(async () => {
    vi.resetAllMocks()
    localStorage.clear()
    route.query.aff = ''
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('registers without email fields when verification is off', async () => {
    register.mockResolvedValue(undefined)
    const wrapper = mountSignUp(false)
    await flushPromises()
    expect(wrapper.find('input[autocomplete="email"]').exists()).toBe(false)
    expect(wrapper.find('input[autocomplete="one-time-code"]').exists()).toBe(
      false
    )

    await wrapper.find('input[autocomplete="username"]').setValue('demo')
    for (const input of wrapper.findAll('input[autocomplete="new-password"]'))
      await input.setValue('password-123')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(register).toHaveBeenCalledTimes(1)
    const payload = register.mock.calls[0][0]
    expect(Object.keys(payload)).not.toContain('email')
    expect(Object.keys(payload)).not.toContain('verification_code')
    expect(payload).toMatchObject({
      username: 'demo',
      password: 'password-123',
    })
    expect(push).toHaveBeenCalledWith({ name: 'sign-in' })
    wrapper.unmount()
  })

  it('requires a valid email and sends the code when verification is on', async () => {
    register.mockResolvedValue(undefined)
    const wrapper = mountSignUp(true)
    await flushPromises()
    const inputs = wrapper.findAll('input').map((input) => input.attributes())
    const emailIndex = inputs.findIndex((a) => a.autocomplete === 'email')
    expect(inputs[emailIndex + 1]?.autocomplete).toBe('one-time-code')
    expect(wrapper.text()).toContain('邮箱验证码')
    expect(wrapper.text()).toContain('发送验证码')

    await wrapper.find('input[autocomplete="username"]').setValue('demo')
    for (const input of wrapper.findAll('input[autocomplete="new-password"]'))
      await input.setValue('password-123')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(register).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('邮箱格式不正确')

    await wrapper
      .find('input[autocomplete="email"]')
      .setValue('demo@example.com')
    await wrapper.find('input[autocomplete="one-time-code"]').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(register.mock.calls[0][0]).toEqual({
      username: 'demo',
      password: 'password-123',
      email: 'demo@example.com',
      verification_code: '123456',
    })
    wrapper.unmount()
  })
})
