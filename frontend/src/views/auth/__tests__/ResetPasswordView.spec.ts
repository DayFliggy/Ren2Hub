import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const resetPassword = vi.hoisted(() => vi.fn())
const turnstileReset = vi.hoisted(() => vi.fn())

vi.mock('@/api/auth', () => ({ authApi: { resetPassword } }))

import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import ResetPasswordView from '../ResetPasswordView.vue'

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

function mountView(turnstile: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  app.phase = 'ready'
  app.status = turnstile
    ? { turnstile_check: true, turnstile_site_key: 'site-key' }
    : {}
  return mount(ResetPasswordView, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        AuthLayout: { template: '<div><slot /></div>' },
        RouterLink: true,
        TurnstileWidget: TurnstileStub,
      },
    },
  })
}

type ResetWrapper = ReturnType<typeof mountView>

async function submitWithToken(wrapper: ResetWrapper, token: string) {
  wrapper.findComponent(TurnstileStub).vm.$emit('verified', token)
  await flushPromises()
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('ResetPasswordView Turnstile', () => {
  beforeEach(async () => {
    vi.resetAllMocks()
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('sends the captured token and hides the widget after success', async () => {
    resetPassword.mockResolvedValue(undefined)
    const wrapper = mountView(true)
    await wrapper.find('input[type="email"]').setValue('demo@example.com')
    const submit = wrapper.find('button[type="submit"]')
    expect(submit.attributes('disabled')).toBeDefined()

    await submitWithToken(wrapper, 'reset-token')

    expect(resetPassword).toHaveBeenCalledWith(
      'demo@example.com',
      'reset-token'
    )
    expect(turnstileReset).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(TurnstileStub).exists()).toBe(false)
    expect(submit.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('resets the widget after business and network failures', async () => {
    resetPassword
      .mockRejectedValueOnce(
        new ApiError('Email not registered', { business: true })
      )
      .mockRejectedValueOnce(new Error('Network Error'))
    const wrapper = mountView(true)
    await wrapper.find('input[type="email"]').setValue('demo@example.com')

    await submitWithToken(wrapper, 'reset-token-1')
    await submitWithToken(wrapper, 'reset-token-2')

    expect(resetPassword.mock.calls).toEqual([
      ['demo@example.com', 'reset-token-1'],
      ['demo@example.com', 'reset-token-2'],
    ])
    expect(turnstileReset).toHaveBeenCalledTimes(2)
    expect(wrapper.findComponent(TurnstileStub).exists()).toBe(true)
    wrapper.unmount()
  })

  it('sends no token when Turnstile is disabled', async () => {
    resetPassword.mockResolvedValue(undefined)
    const wrapper = mountView(false)
    await wrapper.find('input[type="email"]').setValue('demo@example.com')
    expect(wrapper.findComponent(TurnstileStub).exists()).toBe(false)

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(resetPassword).toHaveBeenCalledWith('demo@example.com', undefined)
    expect(turnstileReset).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
