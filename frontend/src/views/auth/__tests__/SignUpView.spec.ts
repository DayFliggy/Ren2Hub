import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const route = vi.hoisted(() => ({ query: { aff: 'INVALID' } }))
const push = vi.hoisted(() => vi.fn())
const validateAffiliate = vi.hoisted(() => vi.fn())

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
    register: vi.fn(),
    validateAffiliate,
  },
}))

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { storeAffiliateAttribution } from '@/utils/affiliate'
import SignUpView from '../SignUpView.vue'

describe('SignUpView affiliate attribution', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    localStorage.clear()
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.phase = 'ready'
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('passes the URL attribution to native registration without a validation API', async () => {
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

    const inputs = wrapper.findAll('input')
    expect(inputs.at(-1)?.element.value).toBe('INVALID')
    expect(validateAffiliate).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('邀请码无效')
    wrapper.unmount()
  })
})
